package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Orwell-Yu/korthex/internal/history"

	tea "github.com/charmbracelet/bubbletea"
)

// HistorySearchModel provides a full-screen overlay for searching conversation history.
// Triggered when the user types "/history" in the chat input.
type HistorySearchModel struct {
	visible bool
	cursor  int

	// Search input
	searchInput string

	// Results
	results []history.SearchResult

	// Selected session ID (to load full conversation)
	selectedSessionID string

	// Dependencies
	store history.Store

	// Dimensions
	width, height int
	theme         Theme
}

// historySearchResultMsg carries async search results.
type historySearchResultMsg struct {
	Results []history.SearchResult
}

// NewHistorySearchModel creates a HistorySearchModel.
func NewHistorySearchModel(store history.Store, theme Theme) HistorySearchModel {
	return HistorySearchModel{
		store: store,
		theme: theme,
	}
}

// Visible returns whether the overlay is shown.
func (m HistorySearchModel) Visible() bool {
	return m.visible
}

// Show opens the history search overlay and returns a Cmd to load recent sessions.
func (m *HistorySearchModel) Show() tea.Cmd {
	m.visible = true
	m.searchInput = ""
	m.results = nil
	m.cursor = 0
	m.selectedSessionID = ""
	return m.doSearch()
}

// SelectedSessionID returns the session ID chosen by the user.
// Non-empty after Enter on a result. Caller should read and clear.
func (m HistorySearchModel) SelectedSessionID() string {
	return m.selectedSessionID
}

// ClearSelectedSessionID clears the selection after it has been consumed.
func (m *HistorySearchModel) ClearSelectedSessionID() {
	m.selectedSessionID = ""
}

// Update handles keys for the history search overlay.
func (m HistorySearchModel) Update(msg tea.Msg) (HistorySearchModel, tea.Cmd) {
	switch msg := msg.(type) {
	case historySearchResultMsg:
		m.results = msg.Results
		m.cursor = 0
		return m, nil

	case tea.KeyMsg:
		if !m.visible {
			return m, nil
		}
		switch msg.String() {
		case "esc":
			m.visible = false
			return m, nil
		case "enter":
			if len(m.results) > 0 {
				c := m.cursor
				if c >= len(m.results) {
					c = len(m.results) - 1
				}
				m.selectedSessionID = m.results[c].SessionID
				m.visible = false
			}
			return m, nil
		case "j", "down":
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}
			return m, nil
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "backspace":
			if len(m.searchInput) > 0 {
				_, size := utf8.DecodeLastRuneInString(m.searchInput)
				m.searchInput = m.searchInput[:len(m.searchInput)-size]
				return m, m.doSearch()
			}
			return m, nil
		default:
			if len(msg.Runes) > 0 {
				m.searchInput += string(msg.Runes)
				return m, m.doSearch()
			}
		}
	}
	return m, nil
}

// doSearch performs an async search. Empty keyword lists recent sessions.
func (m HistorySearchModel) doSearch() tea.Cmd {
	if m.store == nil {
		return func() tea.Msg {
			return historySearchResultMsg{Results: nil}
		}
	}
	query := m.searchInput
	store := m.store
	return func() tea.Msg {
		results, err := store.Search(context.Background(), history.SearchQuery{
			Keyword: query,
			Limit:   20,
		})
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("history search: %w", err)}
		}
		return historySearchResultMsg{Results: results}
	}
}

// View renders the history search overlay.
func (m HistorySearchModel) View() string {
	var b strings.Builder
	b.WriteString(m.theme.Title.Render("History Search"))
	b.WriteString("\n")
	b.WriteString(m.theme.Subtitle.Render("/ " + m.searchInput + "_"))
	b.WriteString("\n\n")

	if len(m.results) == 0 {
		if m.searchInput != "" {
			b.WriteString(m.theme.Subtitle.Render("  No results found."))
		} else {
			b.WriteString(m.theme.Subtitle.Render("  Loading..."))
		}
		b.WriteString("\n\n")
		b.WriteString(m.theme.Subtitle.Render("(Esc to close)"))
		return b.String()
	}

	viewportH := max(m.height-6, 1)
	scrollOff := 0
	if m.cursor >= viewportH {
		scrollOff = m.cursor - viewportH + 1
	}

	rendered := 0
	for i, result := range m.results {
		if i < scrollOff {
			continue
		}
		if rendered >= viewportH {
			break
		}
		line := formatResultLine(result)
		if i == m.cursor {
			b.WriteString(m.theme.Selected.Render("> " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
		rendered++
	}

	b.WriteString("\n")
	b.WriteString(m.theme.Subtitle.Render("(Enter=view history, j/k=navigate, Esc=close)"))

	return b.String()
}

// SetDimensions updates the overlay dimensions.
func (m *HistorySearchModel) SetDimensions(w, h int) {
	m.width = w
	m.height = h
}

// formatResultLine renders a single search result line.
func formatResultLine(r history.SearchResult) string {
	ts := r.StartedAt.Format("2006-01-02 15:04")
	preview := r.Summary
	if preview == "" && len(r.Messages) > 0 {
		preview = r.Messages[0].Content
	}
	if len(preview) > 60 {
		preview = preview[:60] + "..."
	}
	cluster := r.Cluster
	if cluster == "" {
		cluster = "unknown"
	}
	return fmt.Sprintf("[%s] %s: %s", ts, cluster, preview)
}
