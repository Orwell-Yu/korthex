package ui

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

// MarkdownRenderer wraps glamour for terminal markdown rendering.
// It caches rendered output per message index and clears cache on resize.
type MarkdownRenderer struct {
	mu    sync.Mutex
	cache map[int]string // message index → rendered content
	width int
	theme string
}

// NewMarkdownRenderer creates a MarkdownRenderer.
func NewMarkdownRenderer(themeName string, width int) *MarkdownRenderer {
	return &MarkdownRenderer{
		cache: make(map[int]string),
		width: width,
		theme: themeName,
	}
}

// Get returns the cached rendered content for the given message index, or empty if not cached.
func (r *MarkdownRenderer) Get(idx int) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.cache[idx]
	return v, ok
}

// Put stores rendered content in the cache.
func (r *MarkdownRenderer) Put(idx int, content string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[idx] = content
}

// ClearCache invalidates all cached renders (e.g. on terminal resize).
func (r *MarkdownRenderer) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[int]string)
}

// SetWidth updates the render width and clears the cache.
func (r *MarkdownRenderer) SetWidth(w int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.width != w {
		r.width = w
		r.cache = make(map[int]string)
	}
}

// markdownRenderedMsg carries the result of an async glamour render.
type markdownRenderedMsg struct {
	Index   int
	Content string
}

// RenderAsync returns a tea.Cmd that renders markdown content asynchronously.
// On success, it sends a markdownRenderedMsg; on failure, the message is not sent
// (the caller falls back to raw text).
func (r *MarkdownRenderer) RenderAsync(idx int, raw string) tea.Cmd {
	width := r.width
	theme := r.theme
	return func() tea.Msg {
		rendered, err := renderMarkdown(raw, theme, width)
		if err != nil {
			return nil // silent fallback to raw text
		}
		return markdownRenderedMsg{Index: idx, Content: rendered}
	}
}

// renderMarkdown renders a markdown string for the terminal.
func renderMarkdown(content, theme string, width int) (string, error) {
	if width < 10 {
		width = 80
	}
	// Map Korthex theme to glamour style name
	style := "dark"
	switch theme {
	case ThemeLight:
		style = "light"
	case ThemeDracula:
		style = "dracula"
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(style),
		glamour.WithWordWrap(width-4), // leave padding for indented AI output
	)
	if err != nil {
		return "", err
	}

	rendered, err := renderer.Render(content)
	if err != nil {
		return "", err
	}

	// Trim trailing whitespace that glamour adds
	return strings.TrimRight(rendered, "\n "), nil
}
