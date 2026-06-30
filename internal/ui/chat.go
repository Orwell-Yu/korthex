package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/Orwell-Yu/korthex/internal/agent"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
	"github.com/muesli/reflow/wrap"
)

// ChatMessage represents a single message in the chat history.
type ChatMessage struct {
	Role    string // "user", "assistant", "tool", "error", "status"
	Content string
	ID      int // stable identifier for markdown cache (monotonically increasing)
}

// slashCommand defines a chat slash command.
type slashCommand struct {
	Name        string
	Description string
}

// slashCommands is the static registry of available slash commands.
var slashCommands = []slashCommand{
	{Name: "history", Description: "Search conversation history"},
	{Name: "kubeconfig", Description: "Switch kubeconfig / context"},
	{Name: "clear", Description: "Clear chat messages"},
	{Name: "help", Description: "Show keyboard shortcuts"},
}

// ChatModel is the AI chat panel.
type ChatModel struct {
	messages []ChatMessage
	input    textinput.Model
	theme    Theme
	width    int
	height   int

	// Viewport scroll
	scrollOff  int
	autoScroll bool // auto-scroll to bottom on new messages

	// Agent state
	isRunning  bool
	cancelFunc context.CancelFunc
	agent      agent.Agent
	program    *tea.Program
	spinner    spinner.Model

	// Markdown rendering (Phase 2)
	mdRenderer *MarkdownRenderer

	// Slash command autocomplete
	acVisible  bool
	acFiltered []slashCommand
	acCursor   int

	// Text input state (focus and cursor managed by bubbles/textinput)

	// Query history for up/down arrow recall
	queryHistory []string // past user queries (oldest first)
	historyIdx   int      // -1 = new input; 0..len-1 = browsing history
	savedInput   string   // saves in-progress input when browsing

	// FIFO eviction: max conversation turns retained (PRD §5.3)
	maxTurns       int
	collapsedTurns int
	nextMsgID      int // monotonic counter for stable message IDs

	// Token metrics (Phase 3)
	metrics      agent.AgentMetrics
	tokenMetrics bool // config: whether to show metrics in header
}

// NewChatModel creates a chat panel.
func NewChatModel(a agent.Agent, maxTurns int, theme Theme, tokenMetrics bool) ChatModel {
	if maxTurns <= 0 {
		maxTurns = 20
	}
	s := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(theme.AccentFG)),
	)
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "Ask about your cluster..."
	ti.CharLimit = 1024
	ti.Focus()
	return ChatModel{
		agent:        a,
		theme:        theme,
		input:        ti,
		maxTurns:     maxTurns,
		spinner:      s,
		historyIdx:   -1,
		autoScroll:   true,
		mdRenderer:   NewMarkdownRenderer(theme.Name, 80),
		tokenMetrics: tokenMetrics,
	}
}

// SetProgram stores the program reference for p.Send().
func (m *ChatModel) SetProgram(p *tea.Program) {
	m.program = p
}

// Init implements tea.Model.
func (m ChatModel) Init() tea.Cmd { return textinput.Blink }

// Update handles messages for the chat panel.
func (m ChatModel) Update(msg tea.Msg) (ChatModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.mdRenderer.SetWidth(msg.Width)

	case AgentEventMsg:
		return m.handleAgentEvent(agent.AgentEvent(msg))

	case MetricsUpdateMsg:
		m.metrics = msg.Metrics
		return m, nil

	case agentStartedMsg:
		m.isRunning = true
		m.cancelFunc = msg.cancel

	case spinner.TickMsg:
		if !m.isRunning {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case markdownRenderedMsg:
		if msg.Content != "" {
			m.mdRenderer.Put(msg.Index, msg.Content)
			// Recalculate scroll after rendered content changes line count
			if m.autoScroll {
				m.forceScrollToBottom()
			}
		}
		return m, nil

	case ErrorMsg:
		m.addMessage(ChatMessage{Role: "error", Content: msg.Err.Error()})
		m.scrollToBottom()

	case tea.MouseMsg:
		switch msg.Button { //nolint:exhaustive // only wheel events are relevant
		case tea.MouseButtonWheelUp:
			m.scrollOff -= 3
			if m.scrollOff < 0 {
				m.scrollOff = 0
			}
			m.autoScroll = false
		case tea.MouseButtonWheelDown:
			contentH := max(m.height-2, 1)
			totalLines := m.countRenderedLines()
			m.scrollOff += 3
			maxOff := max(totalLines-contentH, 0)
			if m.scrollOff >= maxOff {
				m.scrollOff = maxOff
				m.autoScroll = true
			}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	default:
		// Route unhandled messages (cursor blink, etc.) to textinput
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// agentStartedMsg carries the cancel function from the agent goroutine.
type agentStartedMsg struct {
	cancel context.CancelFunc
}

func (m ChatModel) handleKey(msg tea.KeyMsg) (ChatModel, tea.Cmd) {
	// Autocomplete is visible: intercept navigation keys
	if m.acVisible {
		switch msg.String() {
		case "esc":
			m.acVisible = false
			m.input.Reset()
			return m, nil
		case "up":
			if m.acCursor > 0 {
				m.acCursor--
			}
			return m, nil
		case "down":
			if m.acCursor < len(m.acFiltered)-1 {
				m.acCursor++
			}
			return m, nil
		case "tab":
			// Accept command into input, don't execute
			if len(m.acFiltered) > 0 {
				c := m.acFiltered[min(m.acCursor, len(m.acFiltered)-1)]
				m.input.SetValue("/" + c.Name)
				m.input.CursorEnd()
				m.acVisible = false
			}
			return m, nil
		case "enter":
			// Accept + execute
			if len(m.acFiltered) > 0 {
				c := m.acFiltered[min(m.acCursor, len(m.acFiltered)-1)]
				return m.executeSlashCommand(c.Name)
			}
			return m, nil
		default:
			// Pass to textinput, then re-evaluate autocomplete
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.updateAutocomplete()
			return m, cmd
		}
	}

	switch msg.String() {
	case "enter":
		val := strings.TrimSpace(m.input.Value())
		if val == "" || m.isRunning {
			return m, nil
		}
		// Check if it's a slash command
		if cmdName, ok := strings.CutPrefix(val, "/"); ok {
			return m.executeSlashCommand(cmdName)
		}
		query := val
		m.input.Reset()
		// Save to query history for up/down arrow recall
		m.queryHistory = append(m.queryHistory, query)
		m.historyIdx = -1
		m.savedInput = ""
		m.addMessage(ChatMessage{Role: "user", Content: query})
		m.addMessage(ChatMessage{Role: "status", Content: "Thinking..."})
		m.autoScroll = true
		m.scrollToBottom()
		m.isRunning = true
		// Disable input while running
		m.input.Blur()
		// Reset spinner for fresh tick ID (prevents stale ticks from prior runs)
		m.spinner = spinner.New(
			spinner.WithSpinner(spinner.MiniDot),
			spinner.WithStyle(lipgloss.NewStyle().Foreground(m.theme.AccentFG)),
		)
		return m, tea.Batch(m.executeAgent(query), m.spinner.Tick)

	case "up":
		if m.isRunning || len(m.queryHistory) == 0 {
			return m, nil
		}
		if m.historyIdx == -1 {
			// First press: save current input, go to most recent
			m.savedInput = m.input.Value()
			m.historyIdx = len(m.queryHistory) - 1
		} else if m.historyIdx > 0 {
			m.historyIdx--
		}
		m.input.SetValue(m.queryHistory[m.historyIdx])
		m.input.CursorEnd()
		return m, nil

	case "down":
		if m.isRunning || m.historyIdx == -1 {
			return m, nil
		}
		if m.historyIdx < len(m.queryHistory)-1 {
			m.historyIdx++
			m.input.SetValue(m.queryHistory[m.historyIdx])
		} else {
			// Past the end: restore saved input
			m.historyIdx = -1
			m.input.SetValue(m.savedInput)
			m.savedInput = ""
		}
		m.input.CursorEnd()
		return m, nil

	case "esc":
		m.input.Blur()
		return m, nil

	case "ctrl+u", "pgup":
		contentH := max(m.height-2, 1)
		m.scrollOff -= contentH / 2
		if m.scrollOff < 0 {
			m.scrollOff = 0
		}
		m.autoScroll = false
		return m, nil

	case "ctrl+d", "pgdown":
		contentH := max(m.height-2, 1)
		totalLines := m.countRenderedLines()
		m.scrollOff += contentH / 2
		maxOff := max(totalLines-contentH, 0)
		if m.scrollOff >= maxOff {
			m.scrollOff = maxOff
			m.autoScroll = true
		}
		return m, nil
	}

	// Delegate all other keys (printable, backspace, left/right cursor, etc.) to textinput
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.updateAutocomplete()
	return m, cmd
}

// updateAutocomplete checks current input and shows/hides the autocomplete dropdown.
func (m *ChatModel) updateAutocomplete() {
	val := m.input.Value()
	if !strings.HasPrefix(val, "/") || strings.Contains(val, " ") {
		m.acVisible = false
		return
	}
	prefix := strings.TrimPrefix(val, "/")
	var filtered []slashCommand
	for _, sc := range slashCommands {
		if strings.HasPrefix(sc.Name, prefix) {
			filtered = append(filtered, sc)
		}
	}
	if len(filtered) == 0 {
		m.acVisible = false
		return
	}
	m.acFiltered = filtered
	m.acVisible = true
	if m.acCursor >= len(filtered) {
		m.acCursor = len(filtered) - 1
	}
}

// executeSlashCommand dispatches a slash command by name.
func (m ChatModel) executeSlashCommand(name string) (ChatModel, tea.Cmd) {
	m.input.Reset()
	m.acVisible = false
	switch name {
	case "history":
		return m, func() tea.Msg { return showHistorySearchMsg{} }
	case "kubeconfig":
		return m, func() tea.Msg { return showKubeSwitchMsg{} }
	case "clear":
		m.messages = nil
		m.collapsedTurns = 0
		m.scrollOff = 0
		m.mdRenderer.ClearCache()
		return m, nil
	case "help":
		return m, func() tea.Msg { return showHelpMsg{} }
	default:
		m.addMessage(ChatMessage{Role: "error", Content: "Unknown command: /" + name})
		return m, nil
	}
}

func (m ChatModel) handleAgentEvent(ev agent.AgentEvent) (ChatModel, tea.Cmd) {
	switch ev.Type {
	case agent.EventStreamDelta:
		// Append delta to the last assistant message, or create a new one
		m.appendAssistantDelta(ev.StreamDelta)

	case agent.EventToolCall:
		display := ev.CommandDisplay
		if display == "" {
			display = ev.ToolName
		}
		m.addMessage(ChatMessage{Role: "tool", Content: "$ " + display})

	case agent.EventToolResult:
		result := ev.ToolResult
		if len(result) > 200 {
			result = result[:200] + "..."
		}
		m.addMessage(ChatMessage{Role: "tool", Content: result})

	case agent.EventLogsReady:
		count := len(ev.LogLines)
		m.addMessage(ChatMessage{
			Role:    "status",
			Content: itoa(count) + " log lines loaded in Log Viewer",
		})

	case agent.EventSummary:
		m.addMessage(ChatMessage{Role: "assistant", Content: ev.Text})

	case agent.EventError:
		m.addMessage(ChatMessage{Role: "error", Content: ev.Text})

	case agent.EventComplete:
		m.isRunning = false
		m.cancelFunc = nil
		// Remove "Thinking..." status if it's the last message
		m.removeThinkingStatus()
		// Re-enable auto-scroll and jump to bottom to show final result
		m.autoScroll = true
		// Re-enable input
		m.input.Focus()
		// Trigger async markdown rendering for all assistant messages
		var renderCmds []tea.Cmd
		for _, msg := range m.messages {
			if msg.Role == "assistant" {
				if _, ok := m.mdRenderer.Get(msg.ID); !ok {
					renderCmds = append(renderCmds, m.mdRenderer.RenderAsync(msg.ID, msg.Content))
				}
			}
		}
		renderCmds = append(renderCmds, textinput.Blink)
		// Defer scrollToBottom until after markdown render updates line counts
		m.forceScrollToBottom()
		return m, tea.Batch(renderCmds...)

	case agent.EventKubeSwitch:
		// Handled at AppModel level
	}
	m.scrollToBottom()
	return m, nil
}

func (m *ChatModel) appendAssistantDelta(delta string) {
	// Remove "Thinking..." status on first delta
	m.removeThinkingStatus()

	if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
		m.messages[len(m.messages)-1].Content += delta
	} else {
		msg := ChatMessage{Role: "assistant", Content: delta, ID: m.nextMsgID}
		m.nextMsgID++
		m.messages = append(m.messages, msg)
	}
}

func (m *ChatModel) removeThinkingStatus() {
	if len(m.messages) > 0 {
		last := m.messages[len(m.messages)-1]
		if last.Role == "status" && last.Content == "Thinking..." {
			m.messages = m.messages[:len(m.messages)-1]
		}
	}
}

func (m *ChatModel) addMessage(msg ChatMessage) {
	msg.ID = m.nextMsgID
	m.nextMsgID++
	m.messages = append(m.messages, msg)
	m.evictOldTurns()
}

// evictOldTurns drops the oldest complete turns when the conversation exceeds maxTurns.
// A "turn" starts with a "user" message and includes all subsequent messages until
// the next "user" message.
func (m *ChatModel) evictOldTurns() {
	// Count turns (each "user" message starts a new turn)
	turnCount := 0
	for _, msg := range m.messages {
		if msg.Role == "user" {
			turnCount++
		}
	}

	for turnCount > m.maxTurns && len(m.messages) > 0 {
		// Find the start of the second turn (index of second "user" message)
		secondUser := -1
		found := false
		for i, msg := range m.messages {
			if msg.Role == "user" {
				if found {
					secondUser = i
					break
				}
				found = true
			}
		}
		if secondUser == -1 {
			break // Only one turn left
		}
		m.messages = m.messages[secondUser:]
		m.collapsedTurns++
		turnCount--
	}
}

func (m *ChatModel) scrollToBottom() {
	if !m.autoScroll {
		return
	}
	m.forceScrollToBottom()
}

func (m *ChatModel) forceScrollToBottom() {
	totalLines := m.countRenderedLines()
	contentH := max(m.height-2, 1) // 2 = input line + separator
	m.scrollOff = max(totalLines-contentH, 0)
}

func (m ChatModel) countRenderedLines() int {
	// Must match renderMessages() output exactly
	return len(m.renderMessages())
}

func (m ChatModel) executeAgent(query string) tea.Cmd {
	a := m.agent
	p := m.program
	if a == nil || p == nil {
		return func() tea.Msg {
			return ErrorMsg{Err: fmt.Errorf("agent not available (agent=%v, program=%v)", a != nil, p != nil)}
		}
	}

	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel is returned via agentStartedMsg and stored for Ctrl+C
		ch := make(chan agent.AgentEvent, 16)
		go func() {
			defer close(ch)
			_ = a.Execute(ctx, query, ch)
		}()
		go func() {
			for ev := range ch {
				p.Send(AgentEventMsg(ev))
			}
		}()
		return agentStartedMsg{cancel: cancel}
	}
}

// View renders the chat panel.
func (m ChatModel) View() string {
	var b strings.Builder

	// Header
	b.WriteString(m.theme.Title.Render("AI Assistant"))
	if m.isRunning {
		b.WriteString(" " + m.spinner.View() + " " + m.theme.Subtitle.Render("running"))
	}

	// Token metrics in header (when enabled and non-zero)
	metricsStr := ""
	if m.tokenMetrics && m.metrics.Iterations > 0 {
		// Calculate available width for metrics
		headerUsed := lipgloss.Width(m.theme.Title.Render("AI Assistant"))
		if m.isRunning {
			headerUsed += lipgloss.Width(" "+m.spinner.View()+" "+m.theme.Subtitle.Render("running")) + 1
		}
		if !m.autoScroll {
			headerUsed += lipgloss.Width(m.theme.StatusBarKey.Render("SCROLLED")) + 1
		}
		available := max(m.width-headerUsed-3, 0) // 3 for " ─ " separator + margin
		metricsStr = m.metrics.FormatHeaderProgressive(available)
		if metricsStr != "" {
			b.WriteString(" " + m.theme.Subtitle.Render("─ "+metricsStr))
		}
	}

	if !m.autoScroll {
		scrollHint := m.theme.StatusBarKey.Render("SCROLLED")
		headerUsed := lipgloss.Width(m.theme.Title.Render("AI Assistant"))
		if m.isRunning {
			headerUsed += lipgloss.Width(" "+m.spinner.View()+" "+m.theme.Subtitle.Render("running")) + 1
		}
		if metricsStr != "" {
			headerUsed += lipgloss.Width(" "+m.theme.Subtitle.Render("─ "+metricsStr)) + 1
		}
		pad := max(m.width-headerUsed-lipgloss.Width(scrollHint), 1)
		b.WriteString(strings.Repeat(" ", pad) + scrollHint)
	}
	b.WriteString("\n")

	// Calculate autocomplete dropdown height
	acLines := m.autocompleteHeight()

	// Message area (reduced by autocomplete dropdown if visible)
	contentH := max(m.height-2-acLines, 1) // header + input line + dropdown
	renderedLines := m.renderMessages()

	// Apply scrolling
	start := min(m.scrollOff, len(renderedLines))
	end := min(start+contentH, len(renderedLines))

	visible := renderedLines[start:end]
	for _, line := range visible {
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Pad remaining space
	for i := len(visible); i < contentH; i++ {
		b.WriteString("\n")
	}

	// Autocomplete dropdown (above input line)
	if acLines > 0 {
		b.WriteString(m.renderAutocomplete())
	}

	// Input line
	if m.isRunning {
		b.WriteString("  ") // disabled prompt while running
	} else {
		// Reserve columns for the "> " prompt (2) and the panel's 2-col padding so
		// the textinput viewport + the outer truncateContent don't clip the tail.
		m.input.Width = max(m.width-len(m.input.Prompt)-2, 10)
		b.WriteString(m.input.View())
	}

	return b.String()
}

// autocompleteHeight returns the number of lines the autocomplete dropdown occupies.
func (m ChatModel) autocompleteHeight() int {
	if !m.acVisible || len(m.acFiltered) == 0 {
		return 0
	}
	return min(len(m.acFiltered), 5) + 2 // items + top/bottom border
}

// renderAutocomplete renders the autocomplete dropdown box.
func (m ChatModel) renderAutocomplete() string {
	if !m.acVisible || len(m.acFiltered) == 0 {
		return ""
	}
	maxItems := min(len(m.acFiltered), 5)
	innerW := max(m.width-4, 10)

	var b strings.Builder
	b.WriteString("  " + strings.Repeat("─", innerW) + "\n")
	for i := range maxItems {
		sc := m.acFiltered[i]
		name := "/" + sc.Name
		desc := sc.Description
		// Truncate to fit
		nameW := len(name)
		descW := innerW - nameW - 5 // 2 prefix + 2 spacing + 1 margin
		if descW > 0 && len(desc) > descW {
			desc = desc[:descW] + "…"
		}
		line := fmt.Sprintf("%-*s  %s", nameW, name, desc)
		if i == m.acCursor {
			b.WriteString("  " + m.theme.Selected.Render("> "+line))
		} else {
			b.WriteString("    " + m.theme.Subtitle.Render(line))
		}
		b.WriteString("\n")
	}
	b.WriteString("  " + strings.Repeat("─", innerW) + "\n")
	return b.String()
}

func (m ChatModel) renderMessages() []string {
	var lines []string

	// Wrap width = chat inner width. Keep in sync with truncateContent's maxW so
	// long tool commands/results fold instead of being clipped at the right edge.
	wrapW := max(m.width-1, 10)

	// Show collapsed turns indicator (PRD: "earlier N turns collapsed")
	if m.collapsedTurns > 0 {
		lines = append(lines, m.theme.Subtitle.Render(
			fmt.Sprintf("(%d earlier turns collapsed)", m.collapsedTurns)))
	}

	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			lines = append(lines, wrapWithPrefix(m.theme.AccentStyle().Render("You: "), "     ", msg.Content, wrapW)...)
		case "assistant":
			// Use cached markdown render if available (only after EventComplete).
			// glamour already word-wraps, so emit its lines as-is with the "AI: "/indent prefix.
			content := msg.Content
			if rendered, ok := m.mdRenderer.Get(msg.ID); ok {
				content = rendered
				for j, line := range strings.Split(content, "\n") {
					if j == 0 {
						lines = append(lines, m.theme.Status.Render("AI: ")+line)
					} else {
						lines = append(lines, "    "+line)
					}
				}
				continue
			}
			// Raw (streaming) text: wrap it ourselves.
			lines = append(lines, wrapWithPrefix(m.theme.Status.Render("AI: "), "    ", content, wrapW)...)
		case "tool":
			style := lipgloss.NewStyle().Foreground(m.theme.DimFG)
			for line := range strings.SplitSeq(msg.Content, "\n") {
				for _, wl := range wrapPlain(line, wrapW) {
					lines = append(lines, style.Render(wl))
				}
			}
		case "error":
			lines = append(lines, wrapWithPrefix(m.theme.Error.Render("Error: "), "       ", msg.Content, wrapW)...)
		case "status":
			if msg.Content == "Thinking..." && m.isRunning {
				lines = append(lines, m.spinner.View()+" "+m.theme.AccentStyle().Render("Thinking..."))
			} else {
				lines = append(lines, m.theme.Subtitle.Render(msg.Content))
			}
		}
	}
	return lines
}

// wrapPlain word-wraps plain text to width, force-breaking overlong unbreakable
// tokens so a long no-space string (e.g. a URL) still folds instead of clipping.
func wrapPlain(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	wrapped := wrap.String(wordwrap.String(s, width), width)
	return strings.Split(wrapped, "\n")
}

// wrapWithPrefix wraps text to width and prepends prefix to the first visual line
// and contIndent to each continuation line, so multi-line messages stay aligned.
func wrapWithPrefix(prefix, contIndent, text string, width int) []string {
	// Reserve the indent width so wrapped lines + indent don't exceed the panel.
	body := max(width-len(contIndent), 1)
	segs := wrapPlain(text, body)
	out := make([]string, 0, len(segs))
	for i, seg := range segs {
		if i == 0 {
			out = append(out, prefix+seg)
		} else {
			out = append(out, contIndent+seg)
		}
	}
	if len(out) == 0 {
		out = append(out, prefix)
	}
	return out
}

// AccentStyle returns a style using the accent foreground color.
func (t Theme) AccentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.AccentFG).Bold(true)
}
