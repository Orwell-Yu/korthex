package ui

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DataViewModel is a full-screen overlay that renders query_database results as
// tab-based tables. It follows the same overlay pattern as KubeSwitchModel and
// HelpModel: AppModel.View() returns dataViewer.View(w,h) directly when Visible()
// is true, so the overlay never participates in JoinHorizontal/JoinVertical and
// can NOT corrupt the main three-panel layout.
//
// Sizing contract: View(width, height) drives all rendering from its parameters
// (assigned into the value-receiver copy), so dimensions are always live and can
// never be zero after the first frame — this is the structural fix for the
// "main path goes blank" bug in the reverted dual-mode implementation.
type DataViewModel struct {
	visible   bool
	tabs      []DataTab
	activeTab int
	maxTabs   int
	width     int // updated by Update(WindowSizeMsg) and overwritten by View params
	height    int
	scrollRow int // vertical scroll offset (row)
	scrollCol int // horizontal scroll offset (column)
	theme     Theme

	// Export status
	exportMsg string
	exportEnd time.Time
}

// DataTab holds a single data result tab.
type DataTab struct {
	Name      string
	JoinPath  string
	IsPrimary bool
	Columns   []string
	Rows      [][]string
	RowCount  int
	Truncated bool
	CreatedAt time.Time
	FlashEnd  time.Time // tab name flash animation end time
}

// NewDataViewModel creates a Data Viewer overlay with the given max tab count.
// Dimensions are supplied per-frame via View(w, h); none are stored at creation.
func NewDataViewModel(maxTabs int, theme Theme) DataViewModel {
	if maxTabs <= 0 {
		maxTabs = 10
	}
	return DataViewModel{
		maxTabs: maxTabs,
		theme:   theme,
	}
}

// Visible reports whether the overlay is currently shown.
func (m DataViewModel) Visible() bool {
	return m.visible
}

// Reopen re-shows the overlay if there are cached result tabs. Returns false if
// there is nothing to show (no query has been run yet). Tabs survive Esc, so this
// brings the user back to the last results.
func (m *DataViewModel) Reopen() bool {
	if len(m.tabs) == 0 {
		return false
	}
	m.visible = true
	return true
}

// TabCount returns the number of open tabs.
func (m DataViewModel) TabCount() int {
	return len(m.tabs)
}

// AddTab adds a new data tab from a DataResultMsg and shows the overlay.
func (m *DataViewModel) AddTab(msg DataResultMsg) {
	tab := DataTab{
		Name:      msg.TableName,
		JoinPath:  msg.JoinPath,
		IsPrimary: msg.IsPrimary,
		Columns:   msg.Columns,
		Rows:      msg.Rows,
		RowCount:  msg.RowCount,
		Truncated: msg.Truncated,
		CreatedAt: time.Now(),
		FlashEnd:  time.Now().Add(2 * time.Second),
	}

	if msg.IsPrimary {
		// Primary tab always at position 0, replace existing primary if any.
		if len(m.tabs) > 0 && m.tabs[0].IsPrimary {
			m.tabs[0] = tab
		} else {
			m.tabs = append([]DataTab{tab}, m.tabs...)
			if len(m.tabs) > 1 {
				m.activeTab++ // we inserted at 0, shift active index
			}
		}
	} else {
		// Non-primary: append, enforce FIFO if over limit.
		m.tabs = append(m.tabs, tab)
		m.evictNonPrimary()
	}

	// Focus the newly added tab and show the overlay.
	for i := range m.tabs {
		if m.tabs[i].CreatedAt.Equal(tab.CreatedAt) && m.tabs[i].Name == tab.Name {
			m.activeTab = i
			break
		}
	}
	m.scrollRow = 0
	m.scrollCol = 0
	m.visible = true
}

// evictNonPrimary removes oldest non-primary tabs when count exceeds maxTabs.
func (m *DataViewModel) evictNonPrimary() {
	nonPrimary := m.countNonPrimary()
	for nonPrimary > m.maxTabs {
		oldestIdx := -1
		for i, t := range m.tabs {
			if !t.IsPrimary {
				oldestIdx = i
				break
			}
		}
		if oldestIdx < 0 {
			break
		}
		m.tabs = append(m.tabs[:oldestIdx], m.tabs[oldestIdx+1:]...)
		if m.activeTab >= len(m.tabs) {
			m.activeTab = max(len(m.tabs)-1, 0)
		} else if m.activeTab > oldestIdx {
			m.activeTab--
		}
		nonPrimary--
	}
}

func (m *DataViewModel) countNonPrimary() int {
	count := 0
	for _, t := range m.tabs {
		if !t.IsPrimary {
			count++
		}
	}
	return count
}

// SwitchTab changes active tab and resets scroll offsets.
func (m *DataViewModel) SwitchTab(index int) {
	if index < 0 || index >= len(m.tabs) {
		return
	}
	m.activeTab = index
	m.scrollRow = 0
	m.scrollCol = 0
}

// Update handles keyboard and mouse events while the overlay is visible.
// Esc closes the overlay. WindowSizeMsg keeps stored dims fresh for scroll
// clamping (View still overrides dims from its params each frame).
func (m DataViewModel) Update(msg tea.Msg) (DataViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case csvExportedMsg:
		if msg.err == nil {
			m.exportMsg = "Exported to " + msg.path
			m.exportEnd = time.Now().Add(5 * time.Second)
		} else {
			m.exportMsg = "Export failed: " + msg.err.Error()
			m.exportEnd = time.Now().Add(5 * time.Second)
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m DataViewModel) handleKey(msg tea.KeyMsg) (DataViewModel, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.visible = false
		return m, nil
	case "left", "[":
		if m.activeTab > 0 {
			m.SwitchTab(m.activeTab - 1)
		}
	case "right", "]":
		if m.activeTab < len(m.tabs)-1 {
			m.SwitchTab(m.activeTab + 1)
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx, _ := strconv.Atoi(msg.String())
		idx-- // 1-indexed → 0-indexed
		if idx < len(m.tabs) {
			m.SwitchTab(idx)
		}
	case "j", "down":
		if tab := m.activeTabData(); tab != nil {
			maxScroll := max(len(tab.Rows)-m.dataHeight(), 0)
			if m.scrollRow < maxScroll {
				m.scrollRow++
			}
		}
	case "k", "up":
		if m.scrollRow > 0 {
			m.scrollRow--
		}
	case "h":
		if m.scrollCol > 0 {
			m.scrollCol--
		}
	case "l":
		if tab := m.activeTabData(); tab != nil {
			maxCol := max(len(tab.Columns)-1, 0) // first col is fixed
			if m.scrollCol < maxCol {
				m.scrollCol++
			}
		}
	case "s":
		return m, m.exportCSV()
	}
	return m, nil
}

func (m DataViewModel) handleMouse(msg tea.MouseMsg) (DataViewModel, tea.Cmd) {
	switch msg.Button { //nolint:exhaustive // only wheel + left click are relevant
	case tea.MouseButtonWheelUp:
		m.scrollRow = max(m.scrollRow-3, 0)
	case tea.MouseButtonWheelDown:
		if tab := m.activeTabData(); tab != nil {
			maxScroll := max(len(tab.Rows)-m.dataHeight(), 0)
			m.scrollRow = min(m.scrollRow+3, maxScroll)
		}
	case tea.MouseButtonLeft:
		if msg.Y == 0 {
			m.handleTabClick(msg.X)
		}
	}
	return m, nil
}

// handleTabClick maps an X position in the header to a tab index.
func (m *DataViewModel) handleTabClick(x int) {
	pos := 0
	for i, tab := range m.tabs {
		label := m.tabLabel(tab)
		labelWidth := utf8.RuneCountInString(label) + 1 // +1 for trailing space
		if x >= pos && x < pos+labelWidth {
			m.SwitchTab(i)
			return
		}
		pos += labelWidth
	}
}

// View renders the overlay. Dimensions come from the parameters (assigned into
// this value-receiver copy) so rendering is always driven by live, non-zero
// terminal dimensions. A recover guard ensures a malformed result can never
// crash the whole TUI.
func (m DataViewModel) View(width, height int) (out string) {
	m.width = width
	m.height = height

	defer func() {
		if r := recover(); r != nil {
			out = lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
				m.theme.Subtitle.Render(fmt.Sprintf("data viewer render error: %v (press Esc)", r)))
		}
	}()

	out = lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, m.render())
	return out
}

func (m DataViewModel) render() string {
	if len(m.tabs) == 0 {
		return "No data results yet. (press Esc)"
	}

	var b strings.Builder

	// Tab header line.
	b.WriteString(m.renderTabHeader())
	b.WriteString("\n")

	tab := m.activeTabData()
	if tab == nil || len(tab.Columns) == 0 {
		b.WriteString("(empty result)\n")
		b.WriteString(m.renderHint())
		return b.String()
	}

	colWidths := m.computeColumnWidths(tab)

	// Column header.
	b.WriteString(m.renderColumnHeaders(tab, colWidths))
	b.WriteString("\n")

	// Separator line.
	sepWidth := min(m.sumVisible(colWidths, tab), m.width)
	b.WriteString(strings.Repeat("─", max(sepWidth, 0)))
	b.WriteString("\n")

	// Data rows (bounded by dataHeight and row count).
	dataH := m.dataHeight()
	endRow := min(m.scrollRow+dataH, len(tab.Rows))
	for i := m.scrollRow; i < endRow; i++ {
		b.WriteString(m.renderRow(tab, i, colWidths))
		b.WriteString("\n")
	}

	b.WriteString(m.renderFooter(tab))
	b.WriteString("\n")
	b.WriteString(m.renderHint())

	return b.String()
}

// dataHeight returns available rows for data display.
// Reserved lines: tab header (1) + column header (1) + separator (1) + footer (1) + hint (1).
func (m DataViewModel) dataHeight() int {
	return max(m.height-5, 1)
}

func (m DataViewModel) renderHint() string {
	return m.theme.Subtitle.Render("[ ]/1-9 tabs · j/k rows · h/l cols · s export CSV · Esc close (Ctrl+T reopens)")
}

// renderTabHeader renders the tab bar at the top.
func (m DataViewModel) renderTabHeader() string {
	var parts []string
	now := time.Now()
	for i, tab := range m.tabs {
		label := m.tabLabel(tab)
		switch {
		case i == m.activeTab:
			parts = append(parts, m.theme.Title.Render(label))
		case now.Before(tab.FlashEnd):
			parts = append(parts, m.theme.Selected.Render(label))
		default:
			parts = append(parts, m.theme.Subtitle.Render(label))
		}
	}
	return strings.Join(parts, " ")
}

// tabLabel creates a display label for a tab. Primary tabs are prefixed with "*".
func (m DataViewModel) tabLabel(tab DataTab) string {
	name := tab.Name
	if name == "" {
		name = "result"
	}
	if tab.IsPrimary {
		return "[*" + name + "]"
	}
	return "[" + name + "]"
}

// computeColumnWidths determines the width of each column (header + sampled data),
// capped per column and padded.
func (m DataViewModel) computeColumnWidths(tab *DataTab) []int {
	widths := make([]int, len(tab.Columns))
	const maxColWidth = 30

	for i, col := range tab.Columns {
		widths[i] = min(utf8.RuneCountInString(col), maxColWidth)
	}

	sampleEnd := min(len(tab.Rows), 100)
	for _, row := range tab.Rows[:sampleEnd] {
		for i, cell := range row {
			if i >= len(widths) {
				break // row has more cells than declared columns — ignore extras
			}
			if w := utf8.RuneCountInString(cell); w > widths[i] {
				widths[i] = min(w, maxColWidth)
			}
		}
	}

	for i := range widths {
		widths[i] += 2 // 1 char padding each side
	}
	return widths
}

// renderColumnHeaders renders the bold column header row.
func (m DataViewModel) renderColumnHeaders(tab *DataTab, colWidths []int) string {
	var b strings.Builder
	for _, ci := range m.visibleColumns(tab) {
		if ci >= len(tab.Columns) || ci >= len(colWidths) {
			break
		}
		b.WriteString(m.theme.Title.Render(padRight(tab.Columns[ci], colWidths[ci])))
	}
	return b.String()
}

// renderRow renders a single data row with NULL dimming and numeric right-align.
func (m DataViewModel) renderRow(tab *DataTab, rowIdx int, colWidths []int) string {
	if rowIdx < 0 || rowIdx >= len(tab.Rows) {
		return ""
	}
	row := tab.Rows[rowIdx]
	var b strings.Builder
	for _, ci := range m.visibleColumns(tab) {
		if ci >= len(colWidths) {
			break
		}
		w := colWidths[ci]
		var cell string
		if ci < len(row) {
			cell = row[ci]
		}

		switch {
		case cell == "" || strings.ToUpper(cell) == "NULL":
			b.WriteString(lipgloss.NewStyle().Foreground(m.theme.DimFG).Render(padRight("NULL", w)))
		case isNumeric(cell):
			b.WriteString(padLeft(cell, w))
		default:
			b.WriteString(padRight(cell, w))
		}
	}
	return b.String()
}

// visibleColumns returns the column indices to render: first column (PK) is
// always fixed, then scrollCol offset applied to the rest.
func (m DataViewModel) visibleColumns(tab *DataTab) []int {
	if len(tab.Columns) == 0 {
		return nil
	}
	indices := []int{0}
	for i := 1 + m.scrollCol; i < len(tab.Columns); i++ {
		indices = append(indices, i)
	}
	return indices
}

// sumVisible calculates the total width of visible columns.
func (m DataViewModel) sumVisible(colWidths []int, tab *DataTab) int {
	total := 0
	for _, ci := range m.visibleColumns(tab) {
		if ci < len(colWidths) {
			total += colWidths[ci]
		}
	}
	return total
}

// renderFooter renders row count, join path, and optional export status.
func (m DataViewModel) renderFooter(tab *DataTab) string {
	parts := []string{fmt.Sprintf("%d rows", tab.RowCount)}
	if tab.Truncated {
		parts[0] += " (truncated)"
	}
	if tab.JoinPath != "" {
		parts = append(parts, tab.JoinPath)
	}
	if m.exportMsg != "" && time.Now().Before(m.exportEnd) {
		parts = append(parts, m.exportMsg)
	}
	return m.theme.Subtitle.Render(strings.Join(parts, " │ "))
}

// activeTabData returns a pointer to the active tab, or nil.
func (m *DataViewModel) activeTabData() *DataTab {
	if m.activeTab < 0 || m.activeTab >= len(m.tabs) {
		return nil
	}
	return &m.tabs[m.activeTab]
}

// exportCSV exports the current tab as a CSV file under ~/.korthex/data/.
func (m *DataViewModel) exportCSV() tea.Cmd {
	tab := m.activeTabData()
	if tab == nil {
		return nil
	}

	columns := tab.Columns
	rows := tab.Rows
	name := tab.Name
	if name == "" {
		name = "result"
	}

	return func() tea.Msg {
		dir, err := os.UserHomeDir()
		if err != nil {
			return csvExportedMsg{err: err}
		}
		dataDir := filepath.Join(dir, ".korthex", "data")
		if err := os.MkdirAll(dataDir, 0o750); err != nil {
			return csvExportedMsg{err: err}
		}

		ts := time.Now().Format("20060102_150405")
		path := filepath.Join(dataDir, fmt.Sprintf("%s_%s.csv", name, ts))

		f, err := os.Create(path) //nolint:gosec // path = user home + sanitized table name + timestamp
		if err != nil {
			return csvExportedMsg{err: err}
		}
		defer f.Close()

		w := csv.NewWriter(f)
		if err := w.Write(columns); err != nil {
			return csvExportedMsg{err: err}
		}
		for _, row := range rows {
			if err := w.Write(row); err != nil {
				return csvExportedMsg{err: err}
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return csvExportedMsg{err: err}
		}
		return csvExportedMsg{path: path}
	}
}

// csvExportedMsg carries the result of a CSV export operation.
type csvExportedMsg struct {
	path string
	err  error
}

// --- Helpers ---

func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runeCount := utf8.RuneCountInString(s)
	if runeCount >= width {
		runes := []rune(s)
		if len(runes) > width-1 {
			return string(runes[:max(width-1, 0)]) + "…"
		}
		return s
	}
	return s + strings.Repeat(" ", width-runeCount)
}

func padLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runeCount := utf8.RuneCountInString(s)
	if runeCount >= width {
		runes := []rune(s)
		if len(runes) > width-1 {
			return string(runes[:max(width-1, 0)]) + "…"
		}
		return s
	}
	return strings.Repeat(" ", width-runeCount) + s
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
