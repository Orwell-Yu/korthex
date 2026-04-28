package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/Orwell-Yu/korthex/pkg/logparse"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LogViewerModel displays streaming logs with search, filter, follow, and export.
type LogViewerModel struct {
	buffer   *RingBuffer
	parser   logparse.Parser
	theme    Theme
	pageSize int

	// Viewport state
	lines        []logparse.LogEntry // current filtered+visible entries
	lineToBuffer []int               // maps lines[i] → unfiltered buffer Slice() index (nil = 1:1)
	scrollOff    int                 // first visible line index (vertical)
	scrollX      int                 // horizontal scroll offset (characters)
	viewHeight   int                 // visible lines count
	width        int

	// Follow mode (auto-scroll to bottom on new lines)
	follow bool

	// Search state
	searching    bool
	searchInput  string
	searchRegex  *regexp.Regexp
	searchErr    string
	matchIndices []int // line indices with matches
	matchCurrent int   // index into matchIndices

	// Filter state
	filtering   bool
	filterInput string
	filterRegex *regexp.Regexp
	filterErr   string

	// Bookmarks
	bookmarks      BookmarkModel
	lastBufferBase int // totalWritten - count at last rebuildLines, for shift calculation

	// Log stream context
	namespace string
	podName   string
	container string
	logCancel context.CancelFunc // cancel current log stream

	// Dependencies
	k8sClient k8s.Client
	program   *tea.Program
}

// NewLogViewerModel creates a log viewer panel.
func NewLogViewerModel(buffer *RingBuffer, k8sClient k8s.Client, cfg logViewerConfig, theme Theme) LogViewerModel {
	return LogViewerModel{
		buffer:    buffer,
		parser:    logparse.NewParser(),
		theme:     theme,
		pageSize:  cfg.PageSize,
		follow:    true,
		bookmarks: NewBookmarkModel(theme),
		k8sClient: k8sClient,
	}
}

// logViewerConfig holds config values needed by the log viewer.
type logViewerConfig struct {
	PageSize int
}

// SetProgram stores the program reference for p.Send() from goroutines.
func (m *LogViewerModel) SetProgram(p *tea.Program) {
	m.program = p
}

// SetDimensions updates the panel dimensions.
func (m *LogViewerModel) SetDimensions(w, h int) {
	m.width = w
	m.viewHeight = h
}

// Init implements tea.Model.
func (m LogViewerModel) Init() tea.Cmd { return nil }

// Update handles messages for the log viewer.
func (m LogViewerModel) Update(msg tea.Msg) (LogViewerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.viewHeight = msg.Height

	case NavigateToLogsMsg:
		return m.startLogStream(msg)

	case LogsLoadedMsg:
		for _, ll := range msg.LogLines {
			entry := m.parser.Parse(ll.PodName, ll.Container, ll.Content)
			m.buffer.Append(entry)
		}
		m.rebuildLines()
		if m.follow {
			m.scrollToBottom()
		}

	case LogLineMsg:
		entry := m.parser.Parse(msg.Line.PodName, msg.Line.Container, msg.Line.Content)
		m.buffer.Append(entry)
		m.rebuildLines()
		if m.follow {
			m.scrollToBottom()
		}

	case tea.MouseMsg:
		switch msg.Button { //nolint:exhaustive // only wheel events are relevant
		case tea.MouseButtonWheelUp:
			m.scrollOff -= 3
			if m.scrollOff < 0 {
				m.scrollOff = 0
			}
			m.disableFollowIfNotAtBottom()
		case tea.MouseButtonWheelDown:
			m.scrollOff += 3
			if m.scrollOff > len(m.lines)-m.viewHeight {
				m.scrollOff = max(len(m.lines)-m.viewHeight, 0)
			}
			m.disableFollowIfNotAtBottom()
		}

	case tea.KeyMsg:
		// Bookmark overlay intercepts all keys when visible
		if m.bookmarks.Overlay() {
			m.bookmarks, _ = m.bookmarks.Update(msg)
			// If user pressed Enter on a bookmark, jump to that line
			if !m.bookmarks.Overlay() {
				if bufTarget := m.bookmarks.SelectedLineIndex(); bufTarget >= 0 {
					if linesIdx := m.bufferIdxToLinesIdx(bufTarget); linesIdx >= 0 {
						m.scrollOff = max(linesIdx-m.viewHeight/2, 0)
						maxOff := max(len(m.lines)-m.viewHeight, 0)
						if m.scrollOff > maxOff {
							m.scrollOff = maxOff
						}
						m.follow = false
					}
				}
			}
			return m, nil
		}
		if m.searching {
			return m.updateSearchInput(msg)
		}
		if m.filtering {
			return m.updateFilterInput(msg)
		}
		return m.updateNavigation(msg)
	}
	return m, nil
}

func (m LogViewerModel) startLogStream(msg NavigateToLogsMsg) (LogViewerModel, tea.Cmd) {
	// Cancel existing stream
	if m.logCancel != nil {
		m.logCancel()
	}
	m.buffer.Clear()
	m.namespace = msg.Namespace
	m.podName = msg.PodName
	m.container = msg.Container
	m.follow = true
	m.lines = nil
	m.lineToBuffer = nil
	m.scrollOff = 0
	m.lastBufferBase = 0
	m.scrollX = 0
	m.clearSearch()
	m.clearFilter()

	if m.k8sClient == nil || m.program == nil {
		return m, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.logCancel = cancel
	p := m.program

	k8sClient := m.k8sClient
	req := k8s.LogRequest{
		Namespace: msg.Namespace,
		PodName:   msg.PodName,
		Container: msg.Container,
		Follow:    true,
	}

	return m, func() tea.Msg {
		ch := make(chan k8s.LogLine, 50)
		go func() {
			defer close(ch)
			_ = k8sClient.Logs().StreamLogs(ctx, req, ch)
		}()
		go func() {
			for line := range ch {
				p.Send(LogLineMsg{Line: line})
			}
		}()
		return nil
	}
}

func (m LogViewerModel) updateNavigation(msg tea.KeyMsg) (LogViewerModel, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.scrollOff < len(m.lines)-m.viewHeight {
			m.scrollOff++
		}
		m.disableFollowIfNotAtBottom()

	case "k", "up":
		if m.scrollOff > 0 {
			m.scrollOff--
		}
		m.disableFollowIfNotAtBottom()

	case "g":
		m.scrollOff = 0
		m.follow = false

	case "G":
		m.scrollToBottom()
		m.follow = true

	case "ctrl+d", "pgdown":
		m.scrollOff += m.pageSize
		if m.scrollOff > len(m.lines)-m.viewHeight {
			m.scrollOff = max(len(m.lines)-m.viewHeight, 0)
		}
		m.disableFollowIfNotAtBottom()

	case "ctrl+u", "pgup":
		m.scrollOff -= m.pageSize
		if m.scrollOff < 0 {
			m.scrollOff = 0
		}
		m.follow = false

	case "/":
		m.searching = true
		m.searchInput = ""
		m.searchErr = ""

	case "f":
		m.filtering = true
		m.filterInput = ""
		m.filterErr = ""

	case "F":
		m.follow = !m.follow
		if m.follow {
			m.scrollToBottom()
		}

	case "n", "ctrl+n":
		if m.searchRegex != nil {
			m.nextMatch()
		} else {
			// Bookmark: next — find next bookmark's buffer index, map to lines index
			currentBufIdx := m.scrollOffToBufferIdx()
			if target := m.bookmarks.NextBookmark(currentBufIdx); target >= 0 {
				if linesIdx := m.bufferIdxToLinesIdx(target); linesIdx >= 0 {
					m.scrollOff = max(linesIdx-m.viewHeight/2, 0)
					maxOff := max(len(m.lines)-m.viewHeight, 0)
					if m.scrollOff > maxOff {
						m.scrollOff = maxOff
					}
					m.follow = false
				}
			}
		}

	case "N", "ctrl+p":
		if m.searchRegex != nil {
			m.prevMatch()
		} else {
			// Bookmark: prev
			currentBufIdx := m.scrollOffToBufferIdx()
			if target := m.bookmarks.PrevBookmark(currentBufIdx); target >= 0 {
				if linesIdx := m.bufferIdxToLinesIdx(target); linesIdx >= 0 {
					m.scrollOff = max(linesIdx-m.viewHeight/2, 0)
					m.follow = false
				}
			}
		}

	case "m":
		// Toggle bookmark on current line (use unfiltered buffer index)
		if len(m.lines) > 0 {
			idx := m.scrollOff
			if idx >= len(m.lines) {
				idx = len(m.lines) - 1
			}
			bufIdx := idx
			if m.lineToBuffer != nil && idx < len(m.lineToBuffer) {
				bufIdx = m.lineToBuffer[idx]
			}
			m.bookmarks.Toggle(bufIdx, m.lines[idx])
		}

	case "'":
		// Show bookmark list overlay
		m.bookmarks.ShowOverlay()

	case "s":
		return m, m.exportLogs()

	case "y":
		// Copy current line to clipboard — requires OS integration, noop for now

	case "h", "left":
		if m.scrollX > 0 {
			m.scrollX--
		}

	case "l", "right":
		m.scrollX++

	case "0", "home":
		m.scrollX = 0
	}
	return m, nil
}

func (m LogViewerModel) updateSearchInput(msg tea.KeyMsg) (LogViewerModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.clearSearch()
	case "enter":
		m.searching = false
		m.compileSearch()
	case "backspace":
		if len(m.searchInput) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.searchInput)
			m.searchInput = m.searchInput[:len(m.searchInput)-size]
		}
	default:
		if len(msg.Runes) > 0 {
			m.searchInput += string(msg.Runes)
		}
	}
	return m, nil
}

func (m LogViewerModel) updateFilterInput(msg tea.KeyMsg) (LogViewerModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.clearFilter()
		m.rebuildLines()
	case "enter":
		m.filtering = false
		m.compileFilter()
		m.rebuildLines()
		m.scrollOff = 0
	case "backspace":
		if len(m.filterInput) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.filterInput)
			m.filterInput = m.filterInput[:len(m.filterInput)-size]
		}
	default:
		if len(msg.Runes) > 0 {
			m.filterInput += string(msg.Runes)
		}
	}
	return m, nil
}

// rebuildLines refreshes the visible line set from the ring buffer.
func (m *LogViewerModel) rebuildLines() {
	// Shift bookmark indices to account for evicted buffer entries.
	newBase := m.buffer.TotalWritten() - m.buffer.Len()
	if shift := newBase - m.lastBufferBase; shift > 0 {
		m.bookmarks.ShiftIndices(shift)
	}
	m.lastBufferBase = newBase

	all := m.buffer.Slice()
	if m.filterRegex != nil {
		m.lines = nil
		m.lineToBuffer = nil
		for i, entry := range all {
			if m.filterRegex.MatchString(entry.Raw) {
				m.lines = append(m.lines, entry)
				m.lineToBuffer = append(m.lineToBuffer, i)
			}
		}
	} else {
		m.lines = all
		m.lineToBuffer = nil
	}
	m.rebuildSearchMatches()
}

// scrollOffToBufferIdx maps the current scroll position in m.lines to the unfiltered buffer Slice index.
func (m LogViewerModel) scrollOffToBufferIdx() int {
	idx := m.scrollOff
	if idx >= len(m.lines) {
		idx = max(len(m.lines)-1, 0)
	}
	if m.lineToBuffer != nil && idx < len(m.lineToBuffer) {
		return m.lineToBuffer[idx]
	}
	return idx
}

// bufferIdxToLinesIdx maps an unfiltered buffer Slice index to a m.lines index.
// Returns -1 if the entry is filtered out.
func (m LogViewerModel) bufferIdxToLinesIdx(bufIdx int) int {
	if m.lineToBuffer == nil {
		// No filter: 1:1 mapping
		if bufIdx >= 0 && bufIdx < len(m.lines) {
			return bufIdx
		}
		return -1
	}
	for i, bi := range m.lineToBuffer {
		if bi == bufIdx {
			return i
		}
	}
	return -1
}

func (m *LogViewerModel) compileSearch() {
	if m.searchInput == "" {
		m.searchRegex = nil
		m.matchIndices = nil
		m.matchCurrent = 0
		m.searchErr = ""
		return
	}
	re, err := regexp.Compile(m.searchInput)
	if err != nil {
		m.searchErr = "invalid regex"
		m.searchRegex = nil
		m.matchIndices = nil
		return
	}
	m.searchRegex = re
	m.searchErr = ""
	m.rebuildSearchMatches()
	if len(m.matchIndices) > 0 {
		m.matchCurrent = 0
		m.scrollToMatch()
	}
}

func (m *LogViewerModel) rebuildSearchMatches() {
	if m.searchRegex == nil {
		m.matchIndices = nil
		m.matchCurrent = 0
		return
	}
	m.matchIndices = nil
	for i, entry := range m.lines {
		if m.searchRegex.MatchString(entry.Raw) {
			m.matchIndices = append(m.matchIndices, i)
		}
	}
	if m.matchCurrent >= len(m.matchIndices) {
		m.matchCurrent = 0
	}
}

func (m *LogViewerModel) nextMatch() {
	if len(m.matchIndices) == 0 {
		return
	}
	m.matchCurrent = (m.matchCurrent + 1) % len(m.matchIndices)
	m.scrollToMatch()
}

func (m *LogViewerModel) prevMatch() {
	if len(m.matchIndices) == 0 {
		return
	}
	m.matchCurrent = (m.matchCurrent - 1 + len(m.matchIndices)) % len(m.matchIndices)
	m.scrollToMatch()
}

func (m *LogViewerModel) scrollToMatch() {
	if len(m.matchIndices) == 0 {
		return
	}
	target := m.matchIndices[m.matchCurrent]
	// Center the match in viewport
	m.scrollOff = max(target-m.viewHeight/2, 0)
	maxOff := max(len(m.lines)-m.viewHeight, 0)
	if m.scrollOff > maxOff {
		m.scrollOff = maxOff
	}
}

func (m *LogViewerModel) scrollToBottom() {
	m.scrollOff = max(len(m.lines)-m.viewHeight, 0)
}

func (m *LogViewerModel) disableFollowIfNotAtBottom() {
	maxOff := max(len(m.lines)-m.viewHeight, 0)
	if m.scrollOff < maxOff {
		m.follow = false
	}
}

func (m *LogViewerModel) clearSearch() {
	m.searching = false
	m.searchInput = ""
	m.searchRegex = nil
	m.searchErr = ""
	m.matchIndices = nil
	m.matchCurrent = 0
}

func (m *LogViewerModel) clearFilter() {
	m.filtering = false
	m.filterInput = ""
	m.filterRegex = nil
	m.filterErr = ""
}

func (m *LogViewerModel) compileFilter() {
	if m.filterInput == "" {
		m.filterRegex = nil
		m.filterErr = ""
		return
	}
	re, err := regexp.Compile(m.filterInput)
	if err != nil {
		m.filterErr = "invalid regex"
		m.filterRegex = nil
		return
	}
	m.filterRegex = re
	m.filterErr = ""
}

func (m LogViewerModel) exportLogs() tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("export: get home dir: %w", err)}
		}
		dir := filepath.Join(home, ".korthex", "logs")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return ErrorMsg{Err: fmt.Errorf("export: create dir: %w", err)}
		}
		ts := time.Now().Format("20060102_150405")
		ns := m.namespace
		if ns == "" {
			ns = "default"
		}
		pod := m.podName
		if pod == "" {
			pod = "all"
		}
		filename := fmt.Sprintf("%s_%s_%s.log", ns, pod, ts)
		path := filepath.Join(dir, filename)

		entries := m.buffer.Slice()
		var b strings.Builder
		for _, entry := range entries {
			b.WriteString(entry.Raw)
			b.WriteString("\n")
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
			return ErrorMsg{Err: fmt.Errorf("export: write file: %w", err)}
		}
		return logExportedMsg{path: path}
	}
}

// logExportedMsg signals that logs were exported successfully.
type logExportedMsg struct {
	path string
}

// Reset clears log state for cluster switch.
func (m *LogViewerModel) Reset() {
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}
	m.buffer.Clear()
	m.lines = nil
	m.lineToBuffer = nil
	m.scrollOff = 0
	m.scrollX = 0
	m.follow = true
	m.lastBufferBase = 0
	m.namespace = ""
	m.podName = ""
	m.container = ""
	m.clearSearch()
	m.clearFilter()
	m.bookmarks.Clear()
}

// View renders the log viewer panel.
func (m LogViewerModel) View() string {
	// Bookmark overlay takes precedence
	if m.bookmarks.Overlay() {
		return m.bookmarks.View(m.width, m.viewHeight)
	}

	var b strings.Builder

	// Header
	header := m.renderHeader()
	b.WriteString(header)
	b.WriteString("\n")

	// Input line for search/filter
	if m.searching {
		b.WriteString(m.theme.Subtitle.Render("/" + m.searchInput + "_"))
		if m.searchErr != "" {
			b.WriteString(" " + m.theme.Error.Render(m.searchErr))
		}
		b.WriteString("\n")
	} else if m.filtering {
		b.WriteString(m.theme.Subtitle.Render("filter: " + m.filterInput + "_"))
		if m.filterErr != "" {
			b.WriteString(" " + m.theme.Error.Render(m.filterErr))
		}
		b.WriteString("\n")
	}

	// Calculate content height
	headerLines := 1
	if m.searching || m.filtering {
		headerLines = 2
	}
	footerLines := 1
	contentH := max(m.viewHeight-headerLines-footerLines, 1)

	// Render visible log lines
	if len(m.lines) == 0 {
		b.WriteString(m.theme.Subtitle.Render("  (no logs)"))
		b.WriteString("\n")
	} else {
		rendered := 0
		for i := m.scrollOff; i < len(m.lines) && rendered < contentH; i++ {
			line := m.renderLogLine(i)
			if m.scrollX > 0 {
				line = shiftLineLeft(line, m.scrollX)
			}
			b.WriteString(line)
			b.WriteString("\n")
			rendered++
		}
	}

	// Footer: search matches / filter status
	footer := m.renderFooter()
	b.WriteString(footer)

	return b.String()
}

func (m LogViewerModel) renderHeader() string {
	var parts []string
	parts = append(parts, m.theme.Title.Render("Logs"))

	if m.podName != "" {
		parts = append(parts, m.theme.Subtitle.Render(m.podName))
	}
	if m.namespace != "" {
		parts = append(parts, m.theme.Subtitle.Render("("+m.namespace+")"))
	}

	header := strings.Join(parts, " ")

	// Follow indicator on the right
	if m.follow {
		followTag := m.theme.StatusBarKey.Render("FOLLOW")
		pad := max(m.width-lipgloss.Width(header)-lipgloss.Width(followTag), 1)
		header += strings.Repeat(" ", pad) + followTag
	}

	return header
}

func (m LogViewerModel) renderLogLine(idx int) string {
	entry := m.lines[idx]

	// Bookmark marker (map to unfiltered buffer index)
	bufIdx := idx
	if m.lineToBuffer != nil && idx < len(m.lineToBuffer) {
		bufIdx = m.lineToBuffer[idx]
	}
	var parts []string
	if m.bookmarks.IsBookmarked(bufIdx) {
		parts = append(parts, m.theme.StatusBarKey.Render("*"))
	}

	// Timestamp
	if !entry.Timestamp.IsZero() {
		ts := entry.Timestamp.Format("15:04:05")
		parts = append(parts, m.theme.Subtitle.Render("["+ts+"]"))
	}

	// Pod name
	if entry.PodName != "" {
		podDisplay := "pod/" + entry.PodName
		if entry.Container != "" {
			podDisplay += ":" + entry.Container
		}
		parts = append(parts, m.theme.Subtitle.Render(podDisplay))
	}

	// Severity tag with color
	sevStyle, ok := m.theme.SeverityStyles[entry.Severity]
	if !ok {
		sevStyle = m.theme.SeverityStyles[logparse.SeverityUnknown]
	}
	sevLabel := severityLabel(entry.Severity)
	parts = append(parts, sevStyle.Render(sevLabel))

	// Message content
	message := entry.Raw
	if entry.IsJSON && entry.JSONPretty != "" {
		message = entry.JSONPretty
	}

	// Apply search highlighting
	if m.searchRegex != nil && m.searchRegex.MatchString(message) {
		message = m.highlightSearch(message)
	}

	parts = append(parts, message)

	line := strings.Join(parts, " ")

	// Highlight entire line if it's the current search match
	if len(m.matchIndices) > 0 && m.matchCurrent < len(m.matchIndices) && m.matchIndices[m.matchCurrent] == idx {
		line = lipgloss.NewStyle().Reverse(true).Render(line)
	}

	return line
}

func (m LogViewerModel) highlightSearch(text string) string {
	if m.searchRegex == nil {
		return text
	}
	highlight := lipgloss.NewStyle().Reverse(true)
	return m.searchRegex.ReplaceAllStringFunc(text, func(match string) string {
		return highlight.Render(match)
	})
}

func (m LogViewerModel) renderFooter() string {
	var parts []string

	if m.searchRegex != nil && len(m.matchIndices) > 0 {
		parts = append(parts, fmt.Sprintf("[/%s] %d matches", m.searchInput, len(m.matchIndices)))
		parts = append(parts, fmt.Sprintf("[%d/%d]", m.matchCurrent+1, len(m.matchIndices)))
		parts = append(parts, "[n/N] next/prev")
	} else if m.searchRegex != nil {
		parts = append(parts, fmt.Sprintf("[/%s] no matches", m.searchInput))
	}

	if m.filterRegex != nil {
		parts = append(parts, fmt.Sprintf("[filter: %s]", m.filterInput))
		parts = append(parts, fmt.Sprintf("%d/%d lines", len(m.lines), m.buffer.Len()))
	}

	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d lines", len(m.lines)))
	}

	if m.scrollX > 0 {
		parts = append(parts, fmt.Sprintf("[col +%d]", m.scrollX))
	}

	if bmCount := len(m.bookmarks.Entries()); bmCount > 0 {
		parts = append(parts, fmt.Sprintf("[%d bookmarks]", bmCount))
	}

	return m.theme.Subtitle.Render(strings.Join(parts, " "))
}

func severityLabel(sev logparse.Severity) string {
	switch sev {
	case logparse.SeverityFatal:
		return "FATAL"
	case logparse.SeverityError:
		return "ERROR"
	case logparse.SeverityWarn:
		return "WARN"
	case logparse.SeverityInfo:
		return "INFO"
	case logparse.SeverityDebug:
		return "DEBUG"
	case logparse.SeverityUnknown:
		return "     "
	}
	return "     "
}

// shiftLineLeft drops the first n visible characters from a string while preserving ANSI escape
// sequences. ANSI sequences in the skipped region are kept to preserve styling context for the
// visible portion. This enables horizontal scrolling through log lines that exceed the panel width.
func shiftLineLeft(s string, n int) string {
	if n <= 0 {
		return s
	}

	var out strings.Builder
	visible := 0
	inEsc := false

	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			out.WriteRune(r) // always keep ANSI sequences for styling context
			continue
		}
		if inEsc {
			out.WriteRune(r) // always keep ANSI sequence body
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		visible++
		if visible > n {
			out.WriteRune(r)
		}
	}
	return out.String()
}
