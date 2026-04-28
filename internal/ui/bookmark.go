package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Orwell-Yu/korthex/pkg/logparse"

	tea "github.com/charmbracelet/bubbletea"
)

const maxBookmarks = 50

// BookmarkEntry represents a single bookmarked log line.
type BookmarkEntry struct {
	LineIndex int
	Content   string // first 80 chars of the line
	PodName   string
	Severity  logparse.Severity
	CreatedAt time.Time
}

// BookmarkModel manages log line bookmarks with list overlay.
type BookmarkModel struct {
	entries []BookmarkEntry
	overlay bool // true = showing the bookmark list
	cursor  int  // cursor within overlay list
	theme   Theme
}

// NewBookmarkModel creates a BookmarkModel.
func NewBookmarkModel(theme Theme) BookmarkModel {
	return BookmarkModel{
		theme: theme,
	}
}

// Overlay returns whether the bookmark list overlay is visible.
func (m BookmarkModel) Overlay() bool {
	return m.overlay
}

// Entries returns a copy of current bookmarks.
func (m BookmarkModel) Entries() []BookmarkEntry {
	out := make([]BookmarkEntry, len(m.entries))
	copy(out, m.entries)
	return out
}

// Toggle adds or removes a bookmark at the given line index.
func (m *BookmarkModel) Toggle(lineIdx int, entry logparse.LogEntry) {
	// Check if already bookmarked
	for i, bm := range m.entries {
		if bm.LineIndex == lineIdx {
			// Remove
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return
		}
	}
	// Add (enforce max)
	if len(m.entries) >= maxBookmarks {
		// Remove oldest
		m.entries = m.entries[1:]
	}
	content := entry.Raw
	if len(content) > 80 {
		content = content[:80]
	}
	m.entries = append(m.entries, BookmarkEntry{
		LineIndex: lineIdx,
		Content:   content,
		PodName:   entry.PodName,
		Severity:  entry.Severity,
		CreatedAt: time.Now(),
	})
}

// IsBookmarked checks if a line index has a bookmark.
func (m BookmarkModel) IsBookmarked(lineIdx int) bool {
	for _, bm := range m.entries {
		if bm.LineIndex == lineIdx {
			return true
		}
	}
	return false
}

// ShiftIndices adjusts all bookmark indices by subtracting shift (number of evicted lines).
// Bookmarks that shift to negative indices are removed (their lines were evicted).
func (m *BookmarkModel) ShiftIndices(shift int) {
	if shift <= 0 {
		return
	}
	kept := m.entries[:0]
	for _, bm := range m.entries {
		bm.LineIndex -= shift
		if bm.LineIndex >= 0 {
			kept = append(kept, bm)
		}
	}
	m.entries = kept
}

// NextBookmark returns the line index of the next bookmark after the given index.
// Returns -1 if none found.
func (m BookmarkModel) NextBookmark(currentLineIdx int) int {
	for _, bm := range m.entries {
		if bm.LineIndex > currentLineIdx {
			return bm.LineIndex
		}
	}
	return -1
}

// PrevBookmark returns the line index of the previous bookmark before the given index.
// Returns -1 if none found.
func (m BookmarkModel) PrevBookmark(currentLineIdx int) int {
	for i := len(m.entries) - 1; i >= 0; i-- {
		if m.entries[i].LineIndex < currentLineIdx {
			return m.entries[i].LineIndex
		}
	}
	return -1
}

// SelectedLineIndex returns the line index of the currently selected bookmark in the overlay.
// Returns -1 if overlay is not visible or no entries.
func (m BookmarkModel) SelectedLineIndex() int {
	if !m.overlay || len(m.entries) == 0 {
		return -1
	}
	c := m.cursor
	if c >= len(m.entries) {
		c = len(m.entries) - 1
	}
	return m.entries[c].LineIndex
}

// Update handles keys for the bookmark list overlay.
func (m BookmarkModel) Update(msg tea.Msg) (BookmarkModel, bool) {
	if !m.overlay {
		return m, false
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, false
	}

	switch keyMsg.String() {
	case "esc", "'":
		m.overlay = false
		return m, true
	case "j", "down":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
		return m, true
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, true
	case "enter":
		m.overlay = false
		return m, true
	case "x", "d":
		// Delete selected bookmark
		if len(m.entries) > 0 && m.cursor < len(m.entries) {
			m.entries = append(m.entries[:m.cursor], m.entries[m.cursor+1:]...)
			if m.cursor >= len(m.entries) && m.cursor > 0 {
				m.cursor--
			}
		}
		return m, true
	}
	return m, false
}

// Clear removes all bookmarks.
func (m *BookmarkModel) Clear() {
	m.entries = nil
	m.overlay = false
	m.cursor = 0
}

// ShowOverlay opens the bookmark list overlay.
func (m *BookmarkModel) ShowOverlay() {
	m.overlay = true
	m.cursor = 0
}

// View renders the bookmark list overlay.
func (m BookmarkModel) View(width, height int) string {
	var b strings.Builder
	b.WriteString(m.theme.Title.Render("Bookmarks"))
	b.WriteString("\n\n")

	if len(m.entries) == 0 {
		b.WriteString(m.theme.Subtitle.Render("  No bookmarks. Press 'm' on a log line to bookmark it."))
		b.WriteString("\n\n")
		b.WriteString(m.theme.Subtitle.Render("(Esc to close)"))
		return b.String()
	}

	viewportH := max(height-5, 1)
	scrollOff := 0
	if m.cursor >= viewportH {
		scrollOff = m.cursor - viewportH + 1
	}

	rendered := 0
	for i, bm := range m.entries {
		if i < scrollOff {
			continue
		}
		if rendered >= viewportH {
			break
		}
		line := fmt.Sprintf("[%d] %s: %s", bm.LineIndex, bm.PodName, bm.Content)
		if i == m.cursor {
			b.WriteString(m.theme.Selected.Render("> " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
		rendered++
	}

	b.WriteString("\n")
	b.WriteString(m.theme.Subtitle.Render("(Enter=jump, x=delete, Esc=close)"))

	return b.String()
}
