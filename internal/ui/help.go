package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpModel is a togglable overlay showing keyboard shortcuts.
type HelpModel struct {
	visible bool
	theme   Theme
}

// NewHelpModel creates a HelpModel.
func NewHelpModel(theme Theme) HelpModel {
	return HelpModel{theme: theme}
}

// Toggle flips the help overlay visibility.
func (m *HelpModel) Toggle() {
	m.visible = !m.visible
}

// Visible returns whether the overlay is currently shown.
func (m HelpModel) Visible() bool {
	return m.visible
}

// Update handles keys when the overlay is visible.
func (m HelpModel) Update(msg tea.Msg) (HelpModel, bool) {
	if !m.visible {
		return m, false
	}
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "?", "esc":
			m.visible = false
			return m, true
		}
	}
	return m, true // consume all keys while visible
}

type helpSection struct {
	title string
	keys  []helpEntry
}

type helpEntry struct {
	key  string
	desc string
}

var helpSections = []helpSection{
	{
		title: "Global",
		keys: []helpEntry{
			{"Tab", "Switch panel focus"},
			{"F1/F2/F3", "Layout: Full / Chat / Log"},
			{":", "Focus AI Chat input"},
			{"?", "Toggle help"},
			{"q", "Quit (not in Chat)"},
			{"Ctrl+C", "Cancel AI operation"},
			{"Esc", "Exit mode / go back"},
			{"Scroll", "Scroll focused panel"},
			{"Copy text", "iTerm2: Opt+Drag | Terminal.app: fn+Drag | Other: Shift+Drag"},
		},
	},
	{
		title: "Resource Browser",
		keys: []helpEntry{
			{"j/k", "Move up/down"},
			{"Enter", "Drill into"},
			{"Esc", "Go back"},
			{"/", "Search/filter"},
			{"l", "View pod logs"},
			{"d", "Describe resource"},
			{"y", "Copy resource name"},
			{"1-5", "Switch: Deploy/SS/DS/Job/CJ"},
		},
	},
	{
		title: "Log Viewer",
		keys: []helpEntry{
			{"j/k", "Scroll up/down"},
			{"h/l", "Scroll left/right"},
			{"0", "Reset horizontal scroll"},
			{"g/G", "Top / bottom"},
			{"Ctrl+D/U", "Page down / up"},
			{"/", "Search pattern"},
			{"f", "Filter mode"},
			{"F", "Follow (tail)"},
			{"s", "Save to file"},
			{"y", "Copy selection"},
			{"m", "Toggle bookmark"},
			{"'", "Bookmark list"},
			{"n/N", "Next/prev bookmark"},
		},
	},
	{
		title: "AI Chat",
		keys: []helpEntry{
			{"Enter", "Send query"},
			{"↑/↓", "Browse query history"},
			{"Ctrl+U/PgUp", "Scroll up half page"},
			{"Ctrl+D/PgDn", "Scroll down half page"},
			{"Ctrl+C", "Cancel operation"},
			{"/history", "Search conversation history"},
			{"/clear", "Clear chat messages"},
			{"/help", "Show this help overlay"},
			{"Esc", "Unfocus chat"},
		},
	},
}

// View renders the help overlay centered within the given width and height.
func (m HelpModel) View(width, height int) string {
	if !m.visible {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.theme.Title.Render("Keyboard Shortcuts"))
	b.WriteString("\n\n")

	for i, section := range helpSections {
		b.WriteString(m.theme.Subtitle.Bold(true).Render(section.title))
		b.WriteString("\n")
		for _, e := range section.keys {
			b.WriteString(m.theme.HelpKey.Render(e.key))
			b.WriteString(m.theme.HelpDesc.Render(e.desc))
			b.WriteString("\n")
		}
		if i < len(helpSections)-1 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(m.theme.Subtitle.Render("Press ? or Esc to close"))

	content := m.theme.HelpOverlay.Render(b.String())

	// Center the overlay within the terminal.
	overlayH := lipgloss.Height(content)
	overlayW := lipgloss.Width(content)

	padTop := max((height-overlayH)/2, 0)
	padLeft := max((width-overlayW)/2, 0)

	return strings.Repeat("\n", padTop) +
		strings.Repeat(" ", padLeft) +
		content
}
