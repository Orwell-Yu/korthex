package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Orwell-Yu/korthex/internal/agent"
	"github.com/Orwell-Yu/korthex/internal/config"
	"github.com/Orwell-Yu/korthex/internal/history"
	"github.com/Orwell-Yu/korthex/internal/k8s"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AppModel is the root Bubble Tea model.
type AppModel struct {
	resource      ResourceModel
	logviewer     LogViewerModel
	chat          ChatModel
	statusbar     StatusBarModel
	help          HelpModel
	podDetail     PodDetailModel
	historySearch HistorySearchModel
	kubeSwitch    KubeSwitchModel
	dataViewer    DataViewModel

	logBuffer *RingBuffer

	focus  PanelID
	layout LayoutMode
	width  int
	height int

	agent     agent.Agent
	k8sClient k8s.Client
	config    *config.Config
	program   *tea.Program

	configManager config.Manager
}

// NewAppModel creates the root model with injected dependencies.
func NewAppModel(a agent.Agent, k k8s.Client, cfg *config.Config, historyStore history.Store, cfgManager config.Manager) AppModel {
	theme := GetTheme(cfg.UI.Theme)

	bufSize := cfg.UI.LogLinesLimit
	if bufSize <= 0 {
		bufSize = 10000
	}
	logBuffer := NewRingBuffer(bufSize)

	pageSize := cfg.UI.LogPageSize
	if pageSize <= 0 {
		pageSize = 1000
	}

	maxTurns := cfg.Agent.MaxHistoryTurns
	if maxTurns <= 0 {
		maxTurns = 10
	}

	return AppModel{
		resource: NewResourceModel(k, theme),
		logviewer: NewLogViewerModel(logBuffer, k, logViewerConfig{
			PageSize: pageSize,
		}, theme),
		chat:          NewChatModel(a, maxTurns, theme, cfg.Agent.TokenMetrics),
		statusbar:     NewStatusBarModel(theme),
		help:          NewHelpModel(theme),
		podDetail:     NewPodDetailModel(k, theme),
		historySearch: NewHistorySearchModel(historyStore, theme),
		kubeSwitch:    NewKubeSwitchModel(theme),
		dataViewer:    NewDataViewModel(cfg.Database.Query.MaxRelationPaths, theme),
		logBuffer:     logBuffer,
		focus:         PanelResource,
		layout:        LayoutFull,
		agent:         a,
		k8sClient:     k,
		config:        cfg,
		configManager: cfgManager,
	}
}

// SetProgram stores the *tea.Program reference for p.Send() from goroutines.
func (m *AppModel) SetProgram(p *tea.Program) {
	m.program = p
	m.logviewer.SetProgram(p)
	m.chat.SetProgram(p)
}

// LogBuffer returns the Log Viewer's ring buffer for external read access.
func (m *AppModel) LogBuffer() *RingBuffer {
	return m.logBuffer
}

// panelAtPosition returns which panel occupies the given (x, y) terminal coordinate.
func (m AppModel) panelAtPosition(x, y int) (PanelID, bool) {
	dim := CalculateLayout(m.width, m.height, m.layout)

	switch m.layout {
	case LayoutFull:
		// Left column: resource browser
		if x < dim.ResourceW {
			return PanelResource, true
		}
		// Right column: log viewer (top) / chat (bottom)
		if y < dim.LogViewerH {
			return PanelLogViewer, true
		}
		if y < dim.LogViewerH+dim.ChatH {
			return PanelChat, true
		}

	case LayoutChatFocus:
		// Chat (top) / LogViewer (bottom)
		if y < dim.ChatH {
			return PanelChat, true
		}
		if y < dim.ChatH+dim.LogViewerH {
			return PanelLogViewer, true
		}

	case LayoutLogFocus:
		usable := max(m.height-statusBarH, 1)
		if y < usable {
			return PanelLogViewer, true
		}
	}

	return 0, false // status bar or out of bounds
}

// Init loads the initial namespace list and starts the chat cursor blink.
func (m AppModel) Init() tea.Cmd {
	return tea.Batch(loadNamespaces(m.k8sClient), m.chat.Init())
}

// Update implements the Bubble Tea event loop with keyboard routing priority.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	// 1. WindowSizeMsg — always: recalculate layout, propagate
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		dim := CalculateLayout(m.width, m.height, m.layout)

		// Use inner dimensions (subtract 2 for border) so that child panels
		// see the same height during Update (scroll calculations) and View (rendering).
		// renderPanel also sets inner dimensions before calling View, so this is idempotent.
		m.resource.SetDimensions(max(dim.ResourceW-2, 1), max(dim.ResourceH-2, 1))
		m.logviewer.SetDimensions(max(dim.LogViewerW-2, 1), max(dim.LogViewerH-2, 1))
		m.chat.width = max(dim.ChatW-2, 1)
		m.chat.height = max(dim.ChatH-2, 1)

		var cmd tea.Cmd
		m.statusbar, cmd = m.statusbar.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Keep the data viewer overlay's stored dims fresh for scroll clamping.
		// View() re-applies full terminal dims each frame, so this is advisory.
		m.dataViewer, _ = m.dataViewer.Update(msg)

		return m, tea.Batch(cmds...)

	// 2. Mouse events → click-to-focus + scroll routing
	case tea.MouseMsg:
		// Data viewer overlay (if visible) consumes mouse events first — tab clicks
		// and wheel scrolling — before the click-to-focus / panel scroll routing.
		if m.dataViewer.Visible() {
			var cmd tea.Cmd
			m.dataViewer, cmd = m.dataViewer.Update(msg)
			return m, cmd
		}

		// Left click: focus the panel under the cursor
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			if panel, ok := m.panelAtPosition(msg.X, msg.Y); ok && panel != m.focus {
				m.focus = panel
			}
			return m, nil
		}

		// Scroll: route to focused panel
		switch msg.Button { //nolint:exhaustive // only wheel events are relevant
		case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
			switch m.focus {
			case PanelResource:
				var cmd tea.Cmd
				m.resource, cmd = m.resource.Update(msg)
				return m, cmd
			case PanelLogViewer:
				var cmd tea.Cmd
				m.logviewer, cmd = m.logviewer.Update(msg)
				return m, cmd
			case PanelChat:
				var cmd tea.Cmd
				m.chat, cmd = m.chat.Update(msg)
				return m, cmd
			}
		}
		return m, nil

	// 3. Global hotkeys (regardless of focus)
	case tea.KeyMsg:
		// If help overlay is visible, route to help first
		if m.help.Visible() {
			var consumed bool
			m.help, consumed = m.help.Update(msg)
			if consumed {
				return m, nil
			}
		}

		// If history search overlay is visible, route all keys there
		if m.historySearch.Visible() {
			var cmd tea.Cmd
			m.historySearch, cmd = m.historySearch.Update(msg)
			// Check if user selected a session to restore
			if sid := m.historySearch.SelectedSessionID(); sid != "" {
				m.historySearch.ClearSelectedSessionID()
				return m, m.loadHistorySession(sid)
			}
			return m, cmd
		}

		// If pod detail overlay is visible, route all keys there
		if m.podDetail.Visible() {
			var cmd tea.Cmd
			m.podDetail, cmd = m.podDetail.Update(msg)
			return m, cmd
		}

		// If kubeSwitch overlay is visible, route all keys there
		if m.kubeSwitch.Visible() {
			var cmd tea.Cmd
			m.kubeSwitch, cmd = m.kubeSwitch.Update(msg)
			return m, cmd
		}

		// If data viewer overlay is visible, route all keys there (Esc closes it)
		if m.dataViewer.Visible() {
			var cmd tea.Cmd
			m.dataViewer, cmd = m.dataViewer.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "tab":
			m.focus = m.nextFocus(1)
			return m, nil
		case "shift+tab":
			m.focus = m.nextFocus(-1)
			return m, nil
		case "f1":
			m.layout = LayoutFull
			m.statusbar.SetLayout(m.layout)
			m.focus = m.correctFocus()
			return m, nil
		case "f2":
			m.layout = LayoutChatFocus
			m.statusbar.SetLayout(m.layout)
			m.focus = m.correctFocus()
			return m, nil
		case "f3":
			m.layout = LayoutLogFocus
			m.statusbar.SetLayout(m.layout)
			m.focus = m.correctFocus()
			return m, nil
		case ":":
			m.focus = PanelChat
			m.chat.input.Focus()
			return m, textinput.Blink
		case "?":
			m.help.Toggle()
			return m, nil
		case "ctrl+k":
			m.openKubeSwitch()
			return m, nil
		case "ctrl+t":
			// Reopen the data viewer overlay with the last query results (tabs
			// survive Esc). No-op if no query has been run yet.
			m.dataViewer.Reopen()
			return m, nil
		case "q":
			if m.focus != PanelChat {
				return m, tea.Quit
			}
			// q in Chat falls through to panel
		case "ctrl+c":
			// If agent is running in chat, cancel it instead of quitting
			if m.chat.isRunning && m.chat.cancelFunc != nil {
				m.chat.cancelFunc()
				m.chat.isRunning = false
				m.chat.cancelFunc = nil
				m.chat.addMessage(ChatMessage{Role: "status", Content: "Operation cancelled."})
				m.chat.scrollToBottom()
				return m, nil
			}
			return m, tea.Quit
		}

		// 3. Delegate remaining keys to focused panel
		switch m.focus {
		case PanelResource:
			var cmd tea.Cmd
			m.resource, cmd = m.resource.Update(msg)
			return m, cmd
		case PanelLogViewer:
			var cmd tea.Cmd
			m.logviewer, cmd = m.logviewer.Update(msg)
			return m, cmd
		case PanelChat:
			var cmd tea.Cmd
			m.chat, cmd = m.chat.Update(msg)
			return m, cmd
		}

	// 4. Custom messages — route by type
	case AgentEventMsg:
		// Always route to chat for display
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		// If EventLogsReady, also route to logviewer
		if msg.Type == agent.EventLogsReady {
			var logCmd tea.Cmd
			m.logviewer, logCmd = m.logviewer.Update(LogsLoadedMsg{LogLines: msg.LogLines})
			if logCmd != nil {
				cmds = append(cmds, logCmd)
			}
		}
		// If query_database tool result, parse and emit DataResultMsg for the Data Viewer overlay.
		if msg.Type == agent.EventToolResult && msg.ToolName == "query_database" {
			if dataMsg, ok := parseQueryResult(msg.ToolResult); ok {
				cmds = append(cmds, func() tea.Msg { return dataMsg })
			}
		}
		// If navigate_resource_browser tool call, sync Resource Browser
		if msg.Type == agent.EventToolCall && msg.ToolName == "navigate_resource_browser" {
			if navMsg, ok := parseNavigateToolArgs(msg.ToolArgs); ok {
				var resCmd tea.Cmd
				m.resource, resCmd = m.resource.Update(navMsg)
				if resCmd != nil {
					cmds = append(cmds, resCmd)
				}
				// Ensure resource panel is visible
				if m.layout != LayoutFull {
					m.layout = LayoutFull
					m.statusbar.SetLayout(m.layout)
				}
			}
		}
		// If agent triggers kubeconfig switch
		if msg.Type == agent.EventKubeSwitch && msg.SwitchContext != "" {
			kp := msg.SwitchKubeconfig
			if kp == "" {
				kp2, _ := m.k8sClient.ContextInfo()
				kp = kp2
			}
			switchKube := kp
			switchCtx := msg.SwitchContext
			cmds = append(cmds, func() tea.Msg {
				return kubeSwitchExecuteMsg{
					Kubeconfig: switchKube,
					Context:    switchCtx,
					FromAgent:  true,
				}
			})
		}
		// If metrics update, send to chat
		if msg.Type == agent.EventMetricsUpdate {
			var metricsCmd tea.Cmd
			m.chat, metricsCmd = m.chat.Update(MetricsUpdateMsg{Metrics: msg.Metrics})
			if metricsCmd != nil {
				cmds = append(cmds, metricsCmd)
			}
		}
		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd

	case cursor.BlinkMsg:
		// Route cursor blink to chat input
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd

	case agentStartedMsg:
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd

	case NavigateToLogsMsg:
		// Start log stream + switch focus to logviewer
		m.focus = PanelLogViewer
		var cmd tea.Cmd
		m.logviewer, cmd = m.logviewer.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.statusbar.SetNamespace(msg.Namespace)
		return m, tea.Batch(cmds...)

	case DataResultMsg:
		// query_database result → add a tab and show the overlay. The first result
		// (empty viewer) is the pinned primary tab; later results are FIFO-evicted.
		if m.dataViewer.TabCount() == 0 {
			msg.IsPrimary = true
		}
		m.dataViewer.AddTab(msg)
		return m, nil

	case csvExportedMsg:
		var cmd tea.Cmd
		m.dataViewer, cmd = m.dataViewer.Update(msg)
		return m, cmd

	case LogLineMsg:
		var cmd tea.Cmd
		m.logviewer, cmd = m.logviewer.Update(msg)
		return m, cmd

	case InformerUpdateMsg:
		var cmd tea.Cmd
		m.resource, cmd = m.resource.Update(msg)
		return m, cmd

	case ClusterConnectedMsg:
		// Route to resource panel + statusbar
		var cmd tea.Cmd
		m.resource, cmd = m.resource.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		var sbCmd tea.Cmd
		m.statusbar, sbCmd = m.statusbar.Update(msg)
		if sbCmd != nil {
			cmds = append(cmds, sbCmd)
		}
		if m.k8sClient != nil {
			m.statusbar.SetContext(m.k8sClient.CurrentContext())
		}
		m.statusbar.SetProvider(m.config.LLM.Provider, m.config.LLM.Model)
		return m, tea.Batch(cmds...)

	case ErrorMsg:
		var cmd tea.Cmd
		m.statusbar, cmd = m.statusbar.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.chat, _ = m.chat.Update(msg)
		return m, tea.Batch(cmds...)

	case logExportedMsg:
		m.statusbar.SetStatus("Logs exported: " + msg.path)
		return m, nil

	case showHistorySearchMsg:
		dim := CalculateLayout(m.width, m.height, m.layout)
		m.historySearch.SetDimensions(max(dim.ChatW-2, 1), max(dim.ChatH-2, 1))
		cmd := m.historySearch.Show()
		return m, cmd

	case showHelpMsg:
		m.help.Toggle()
		return m, nil

	case showKubeSwitchMsg:
		m.openKubeSwitch()
		return m, nil

	case kubeSwitchExecuteMsg:
		// Cancel running agent only if switch was initiated by user (not by the agent itself).
		// Agent-triggered switches let the agent finish its loop naturally.
		if !msg.FromAgent && m.chat.isRunning && m.chat.cancelFunc != nil {
			m.chat.cancelFunc()
			m.chat.isRunning = false
			m.chat.cancelFunc = nil
		}
		m.kubeSwitch.SetConnecting()
		kubeconfig := msg.Kubeconfig
		ctxName := msg.Context
		return m, func() tea.Msg {
			if err := m.k8sClient.Reconnect(kubeconfig, ctxName); err != nil {
				return kubeSwitchCompleteMsg{Err: err}
			}
			return kubeSwitchCompleteMsg{Kubeconfig: kubeconfig, ContextName: ctxName}
		}

	case kubeSwitchCompleteMsg:
		if msg.Err != nil {
			m.kubeSwitch.ShowError(msg.Err)
			return m, nil
		}
		m.kubeSwitch.Close()

		// Persist to config
		if m.configManager != nil {
			m.config.Kubernetes.Kubeconfig = msg.Kubeconfig
			m.config.Kubernetes.DefaultContext = msg.ContextName
			_ = m.configManager.Save(m.config)
		}

		// Reset resource browser and log viewer (Chat preserved)
		reloadCmd := m.resource.Reset()
		m.logviewer.Reset()

		// Update status bar
		m.statusbar.SetContext(msg.ContextName)
		m.statusbar.SetNamespace("")

		// Update agent cluster context
		m.agent.SetClusterContext(agent.ClusterContext{ContextName: msg.ContextName})

		// Ensure full layout
		m.layout = LayoutFull
		m.statusbar.SetLayout(m.layout)

		m.chat.addMessage(ChatMessage{
			Role:    "status",
			Content: fmt.Sprintf("Switched to context: %s", msg.ContextName),
		})

		return m, reloadCmd

	case showPodDetailMsg:
		dim := CalculateLayout(m.width, m.height, m.layout)
		m.podDetail.SetDimensions(max(dim.ResourceW-2, 1), max(dim.ResourceH-2, 1))
		var cmd tea.Cmd
		m.podDetail, cmd = m.podDetail.ShowPodDetail(msg.Namespace, msg.PodName)
		return m, cmd

	case podDetailReadyMsg:
		var cmd tea.Cmd
		m.podDetail, cmd = m.podDetail.Update(msg)
		return m, cmd

	case historySessionLoadedMsg:
		// Restore conversation: replace chat messages with loaded session
		m.chat.messages = msg.Messages
		m.chat.nextMsgID = len(msg.Messages)
		m.chat.collapsedTurns = 0
		m.chat.scrollOff = 0
		m.chat.autoScroll = true
		m.chat.mdRenderer.ClearCache()
		m.chat.forceScrollToBottom()
		// Trigger markdown rendering for assistant messages
		var renderCmds []tea.Cmd
		for _, chatMsg := range m.chat.messages {
			if chatMsg.Role == "assistant" {
				renderCmds = append(renderCmds, m.chat.mdRenderer.RenderAsync(chatMsg.ID, chatMsg.Content))
			}
		}
		return m, tea.Batch(renderCmds...)

	default:
		// Route data loading messages to resource
		switch msg.(type) {
		case deploymentsLoadedMsg, podsLoadedMsg, describeResultMsg, resourceItemsLoadedMsg:
			var cmd tea.Cmd
			m.resource, cmd = m.resource.Update(msg)
			return m, cmd
		case markdownRenderedMsg:
			var cmd tea.Cmd
			m.chat, cmd = m.chat.Update(msg)
			return m, cmd
		case historySearchResultMsg:
			var cmd tea.Cmd
			m.historySearch, cmd = m.historySearch.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// View composes all panels according to the current layout.
func (m AppModel) View() string {
	// Terminal too small warning
	if IsTerminalTooSmall(m.width, m.height) {
		return TerminalTooSmallMsg(m.width, m.height)
	}

	// KubeSwitch overlay takes precedence over everything except terminal-too-small
	if m.kubeSwitch.Visible() {
		return m.kubeSwitch.View(m.width, m.height)
	}

	// Help overlay takes over
	if m.help.Visible() {
		return m.help.View(m.width, m.height)
	}

	// Data viewer overlay takes over (full-screen; never composed into the
	// main layout, so it cannot corrupt the three-panel rendering).
	if m.dataViewer.Visible() {
		return m.dataViewer.View(m.width, m.height)
	}

	dim := CalculateLayout(m.width, m.height, m.layout)

	statusView := m.statusbar.View()

	var composed string
	switch m.layout {
	case LayoutFull:
		// Left: resource (or pod detail overlay) | Right: logviewer (top) / chat (bottom)
		var resView string
		if m.podDetail.Visible() {
			resView = m.renderPodDetailPanel(dim.ResourceW, dim.ResourceH)
		} else {
			resView = m.renderPanel(PanelResource, dim.ResourceW, dim.ResourceH)
		}

		logView := m.renderPanel(PanelLogViewer, dim.LogViewerW, dim.LogViewerH)
		var chatView string
		if m.historySearch.Visible() {
			chatView = m.renderHistorySearchPanel(dim.ChatW, dim.ChatH)
		} else {
			chatView = m.renderPanel(PanelChat, dim.ChatW, dim.ChatH)
		}
		rightCol := lipgloss.JoinVertical(lipgloss.Left, logView, chatView)

		mainArea := lipgloss.JoinHorizontal(lipgloss.Top, resView, rightCol)
		composed = lipgloss.JoinVertical(lipgloss.Left, mainArea, statusView)

	case LayoutChatFocus:
		var chatView string
		if m.historySearch.Visible() {
			chatView = m.renderHistorySearchPanel(dim.ChatW, dim.ChatH)
		} else {
			chatView = m.renderPanel(PanelChat, dim.ChatW, dim.ChatH)
		}
		logView := m.renderPanel(PanelLogViewer, dim.LogViewerW, dim.LogViewerH)
		mainArea := lipgloss.JoinVertical(lipgloss.Left, chatView, logView)
		composed = lipgloss.JoinVertical(lipgloss.Left, mainArea, statusView)

	case LayoutLogFocus:
		logView := m.renderPanel(PanelLogViewer, dim.LogViewerW, dim.LogViewerH)
		composed = lipgloss.JoinVertical(lipgloss.Left, logView, statusView)
	}

	// Hard-cap output to terminal dimensions to prevent overflow clipping
	return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, composed)
}

// loadHistorySession returns a tea.Cmd that loads a full session from the history store
// and converts it into ChatMessages for display.
func (m AppModel) loadHistorySession(sessionID string) tea.Cmd {
	store := m.historySearch.store
	if store == nil {
		return nil
	}
	return func() tea.Msg {
		msgs, err := store.LoadSession(context.Background(), sessionID)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		var chatMsgs []ChatMessage
		id := 0
		for _, msg := range msgs {
			role := msg.Role
			// Map history roles to chat display roles
			switch role {
			case "tool_call":
				role = "tool"
			case "tool_result":
				role = "tool"
			}
			chatMsgs = append(chatMsgs, ChatMessage{
				Role:    role,
				Content: msg.Content,
				ID:      id,
			})
			id++
		}
		return historySessionLoadedMsg{Messages: chatMsgs}
	}
}

func (m AppModel) renderPodDetailPanel(w, h int) string {
	theme := GetTheme(m.config.UI.Theme)
	border := theme.ActiveBorder

	innerW := max(w-2, 1)
	innerH := max(h-2, 1)

	m.podDetail.SetDimensions(innerW, innerH)
	content := m.podDetail.View()
	content = truncateContent(content, innerW, innerH)

	return border.Width(innerW).Height(innerH).Render(content)
}

func (m AppModel) renderHistorySearchPanel(w, h int) string {
	theme := GetTheme(m.config.UI.Theme)
	border := theme.ActiveBorder // always active when user is interacting

	innerW := max(w-2, 1)
	innerH := max(h-2, 1)

	m.historySearch.SetDimensions(innerW, innerH)
	content := m.historySearch.View()
	content = truncateContent(content, innerW, innerH)

	return border.Width(innerW).Height(innerH).Render(content)
}

func (m AppModel) renderPanel(id PanelID, w, h int) (out string) {
	active := m.focus == id
	theme := GetTheme(m.config.UI.Theme)

	var border lipgloss.Style
	if active {
		border = theme.ActiveBorder
	} else {
		border = theme.InactiveBorder
	}

	// Inner content dimensions (border takes 2 chars width + 2 lines height)
	innerW := max(w-2, 1)
	innerH := max(h-2, 1)

	// Guard against a panic in any panel's View(): degrade that single panel to
	// a placeholder rather than crashing the whole TUI (which renders as a blank
	// screen). The other panels keep rendering normally.
	defer func() {
		if r := recover(); r != nil {
			out = border.Width(innerW).Height(innerH).Render(
				truncateContent(fmt.Sprintf("panel render error: %v", r), innerW, innerH))
		}
	}()

	var content string
	switch id {
	case PanelResource:
		m.resource.SetDimensions(innerW, innerH)
		content = m.resource.View()
	case PanelLogViewer:
		m.logviewer.SetDimensions(innerW, innerH)
		content = m.logviewer.View()
	case PanelChat:
		m.chat.width = innerW
		m.chat.height = innerH
		content = m.chat.View()
	}

	// Hard-truncate content to prevent overflow (lipgloss Height is minimum, not maximum)
	content = truncateContent(content, innerW, innerH)

	return border.Width(innerW).Height(innerH).Render(content)
}

func (m AppModel) nextFocus(delta int) PanelID {
	panels := m.visiblePanels()
	if len(panels) == 0 {
		return m.focus
	}

	current := 0
	for i, p := range panels {
		if p == m.focus {
			current = i
			break
		}
	}

	next := (current + delta + len(panels)) % len(panels)
	return panels[next]
}

func (m AppModel) visiblePanels() []PanelID {
	switch m.layout {
	case LayoutFull:
		return []PanelID{PanelResource, PanelLogViewer, PanelChat}
	case LayoutChatFocus:
		return []PanelID{PanelChat, PanelLogViewer}
	case LayoutLogFocus:
		return []PanelID{PanelLogViewer}
	}
	return []PanelID{PanelResource, PanelLogViewer, PanelChat}
}

// correctFocus returns the current focus if it is visible, otherwise the first visible panel.
func (m AppModel) correctFocus() PanelID {
	if slices.Contains(m.visiblePanels(), m.focus) {
		return m.focus
	}
	return m.visiblePanels()[0]
}

// truncateContent hard-caps content to maxW visible characters per line and maxH lines.
// This prevents terminal line-wrapping from inflating the rendered height beyond what
// the layout calculated.
func truncateContent(s string, maxW, maxH int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > maxH {
		lines = lines[:maxH]
	}
	for i, line := range lines {
		if ansiWidth(line) > maxW {
			lines[i] = truncateAnsiLine(line, maxW)
		}
	}
	return strings.Join(lines, "\n")
}

// ansiWidth returns the visible character width of a string, ignoring ANSI escape sequences.
func ansiWidth(s string) int {
	return lipgloss.Width(s)
}

// truncateAnsiLine truncates a string to maxW visible characters, preserving ANSI sequences.
func truncateAnsiLine(s string, maxW int) string {
	w := 0
	inEsc := false
	lastValid := 0
	for i, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		w++
		if w > maxW {
			return s[:lastValid] + "\x1b[0m"
		}
		lastValid = i + len(string(r))
	}
	return s
}

// parseNavigateToolArgs converts navigate_resource_browser tool args to NavigateToResourceMsg.
// Returns (msg, true) on success, (zero, false) if the level is invalid.
func parseNavigateToolArgs(args map[string]string) (NavigateToResourceMsg, bool) {
	var level int
	switch args["level"] {
	case "namespace":
		level = levelNamespace
	case "deployment":
		level = levelDeployment
	case "pod":
		level = levelPod
	default:
		return NavigateToResourceMsg{}, false
	}
	name := args["name"]
	// For namespace level, AI often puts the target in "namespace" instead of "name".
	// Fall back so pendingHighlight still works.
	if level == levelNamespace && name == "" {
		name = args["namespace"]
	}
	return NavigateToResourceMsg{
		Level:     level,
		Namespace: args["namespace"],
		Name:      name,
	}, true
}

// parseQueryResult extracts the [query_result]...[/query_result] JSON block
// from a query_database tool result and returns a DataResultMsg.
func parseQueryResult(toolResult string) (DataResultMsg, bool) {
	const startTag = "[query_result]\n"
	const endTag = "\n[/query_result]"
	si := strings.Index(toolResult, startTag)
	if si < 0 {
		return DataResultMsg{}, false
	}
	si += len(startTag)
	ei := strings.Index(toolResult[si:], endTag)
	if ei < 0 {
		return DataResultMsg{}, false
	}
	raw := toolResult[si : si+ei]
	var parsed struct {
		Columns   []string   `json:"columns"`
		Rows      [][]string `json:"rows"`
		RowCount  int        `json:"row_count"`
		Truncated bool       `json:"truncated"`
		Database  string     `json:"database"`
		Namespace string     `json:"namespace"`
		PodName   string     `json:"pod_name"`
		TableName string     `json:"table_name"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return DataResultMsg{}, false
	}
	return DataResultMsg{
		Columns:   parsed.Columns,
		Rows:      parsed.Rows,
		RowCount:  parsed.RowCount,
		Truncated: parsed.Truncated,
		Database:  parsed.Database,
		Namespace: parsed.Namespace,
		PodName:   parsed.PodName,
		TableName: parsed.TableName,
	}, true
}

// openKubeSwitch opens the kubeconfig switch overlay if the agent is not running.
func (m *AppModel) openKubeSwitch() {
	if m.chat.isRunning {
		return
	}
	kp, ctx := m.k8sClient.ContextInfo()
	envKube := os.Getenv("KUBECONFIG")
	m.kubeSwitch.Show(kp, ctx, kubeHomeDir(), envKube)
}

// kubeHomeDir returns the default ~/.kube directory path.
func kubeHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(home, ".kube")
}
