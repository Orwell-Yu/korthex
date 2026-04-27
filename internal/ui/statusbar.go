package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// StatusBarModel displays cluster context, namespace, LLM provider, layout mode, and drop counter.
type StatusBarModel struct {
	contextName string
	namespace   string
	provider    string
	model       string
	layout      LayoutMode
	dropCount   int
	statusMsg   string // transient status message (e.g. export success)
	width       int
	theme       Theme
}

// NewStatusBarModel creates a StatusBarModel.
func NewStatusBarModel(theme Theme) StatusBarModel {
	return StatusBarModel{
		theme:       theme,
		contextName: "disconnected",
		namespace:   "-",
		provider:    "-",
		model:       "-",
	}
}

// SetContext updates the displayed K8s context.
func (m *StatusBarModel) SetContext(name string) { m.contextName = name }

// SetNamespace updates the displayed namespace.
func (m *StatusBarModel) SetNamespace(ns string) { m.namespace = ns }

// SetProvider updates the displayed LLM provider and model.
func (m *StatusBarModel) SetProvider(provider, model string) {
	m.provider = provider
	m.model = model
}

// SetLayout updates the displayed layout mode.
func (m *StatusBarModel) SetLayout(l LayoutMode) { m.layout = l }

// SetStatus sets a transient status message displayed in the status bar.
func (m *StatusBarModel) SetStatus(msg string) { m.statusMsg = msg }

// IncrementDrop increments the log drop counter.
func (m *StatusBarModel) IncrementDrop() { m.dropCount++ }

// Init implements tea.Model.
func (m StatusBarModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m StatusBarModel) Update(msg tea.Msg) (StatusBarModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case ClusterConnectedMsg:
		if len(msg.Namespaces) > 0 {
			m.namespace = msg.Namespaces[0]
		}
	}
	return m, nil
}

func layoutName(l LayoutMode) string {
	switch l {
	case LayoutFull:
		return "Full"
	case LayoutChatFocus:
		return "Chat"
	case LayoutLogFocus:
		return "Log"
	default:
		return "Full"
	}
}

// View renders the status bar.
func (m StatusBarModel) View() string {
	segments := []struct {
		key   string
		value string
	}{
		{"CTX", m.contextName},
		{"NS", m.namespace},
		{"LLM", m.provider + ":" + m.model},
		{"MODE", layoutName(m.layout)},
	}

	if m.dropCount > 0 {
		segments = append(segments, struct {
			key   string
			value string
		}{"DROP", itoa(m.dropCount)})
	}

	if m.statusMsg != "" {
		segments = append(segments, struct {
			key   string
			value string
		}{"MSG", m.statusMsg})
	}

	var b strings.Builder
	for _, seg := range segments {
		b.WriteString(m.theme.StatusBarKey.Render(seg.key))
		b.WriteString(m.theme.StatusBarValue.Render(seg.value))
	}

	rendered := b.String()

	// Pad to full width.
	return m.theme.StatusBar.Width(m.width).Render(rendered)
}
