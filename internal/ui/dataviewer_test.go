package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestDataViewer() DataViewModel {
	return NewDataViewModel(10, GetTheme("dark"))
}

func TestDataViewModel_AddTab_Primary(t *testing.T) {
	m := newTestDataViewer()
	m.AddTab(DataResultMsg{
		TableName: "users",
		IsPrimary: true,
		Columns:   []string{"id", "name", "email"},
		Rows:      [][]string{{"1", "Alice", "alice@test.com"}},
		RowCount:  1,
	})

	if len(m.tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(m.tabs))
	}
	if !m.tabs[0].IsPrimary {
		t.Error("expected tab to be primary")
	}
	if m.tabs[0].Name != "users" {
		t.Errorf("expected tab name 'users', got %q", m.tabs[0].Name)
	}
	if !m.Visible() {
		t.Error("AddTab should make the overlay visible")
	}
}

func TestDataViewModel_AddTab_NonPrimary(t *testing.T) {
	m := newTestDataViewer()
	m.AddTab(DataResultMsg{TableName: "users", IsPrimary: true, Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	m.AddTab(DataResultMsg{TableName: "orders", IsPrimary: false, Columns: []string{"id"}, Rows: [][]string{{"1"}}})

	if len(m.tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(m.tabs))
	}
	if m.tabs[1].Name != "orders" || m.tabs[1].IsPrimary {
		t.Errorf("expected second tab 'orders' non-primary, got %q primary=%v", m.tabs[1].Name, m.tabs[1].IsPrimary)
	}
}

func TestDataViewModel_FIFO_Eviction(t *testing.T) {
	m := NewDataViewModel(3, GetTheme("dark"))
	m.AddTab(DataResultMsg{TableName: "primary", IsPrimary: true, Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	for _, name := range []string{"a", "b", "c", "d"} {
		m.AddTab(DataResultMsg{TableName: name, Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	}

	// 1 primary + at most 3 non-primary
	if m.countNonPrimary() != 3 {
		t.Fatalf("expected 3 non-primary tabs after eviction, got %d", m.countNonPrimary())
	}
	// Oldest non-primary ("a") should be evicted
	for _, tab := range m.tabs {
		if tab.Name == "a" {
			t.Error("oldest non-primary tab 'a' should have been evicted")
		}
	}
}

func TestDataViewModel_PrimaryNeverEvicted(t *testing.T) {
	m := NewDataViewModel(2, GetTheme("dark"))
	m.AddTab(DataResultMsg{TableName: "keepme", IsPrimary: true, Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		m.AddTab(DataResultMsg{TableName: name, Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	}
	found := false
	for _, tab := range m.tabs {
		if tab.Name == "keepme" {
			found = true
		}
	}
	if !found {
		t.Error("primary tab was evicted but should be pinned")
	}
}

func TestDataViewModel_SwitchTab_ResetsScroll(t *testing.T) {
	m := newTestDataViewer()
	m.AddTab(DataResultMsg{TableName: "a", IsPrimary: true, Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	m.AddTab(DataResultMsg{TableName: "b", Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	m.scrollRow = 5
	m.scrollCol = 2
	m.SwitchTab(0)
	if m.scrollRow != 0 || m.scrollCol != 0 {
		t.Errorf("SwitchTab should reset scroll, got row=%d col=%d", m.scrollRow, m.scrollCol)
	}
}

func TestDataViewModel_Render_BasicTable(t *testing.T) {
	m := newTestDataViewer()
	m.AddTab(DataResultMsg{
		TableName: "users",
		IsPrimary: true,
		Columns:   []string{"id", "name"},
		Rows:      [][]string{{"1", "Alice"}, {"2", "Bob"}},
		RowCount:  2,
	})
	out := m.View(80, 24)
	for _, want := range []string{"id", "name", "Alice", "Bob", "2 rows"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q\n%s", want, out)
		}
	}
}

func TestDataViewModel_Render_NullValues(t *testing.T) {
	m := newTestDataViewer()
	m.AddTab(DataResultMsg{
		TableName: "t",
		IsPrimary: true,
		Columns:   []string{"id", "note"},
		Rows:      [][]string{{"1", ""}},
		RowCount:  1,
	})
	if out := m.View(80, 24); !strings.Contains(out, "NULL") {
		t.Errorf("empty cell should render as NULL\n%s", out)
	}
}

func TestDataViewModel_Render_Truncated(t *testing.T) {
	m := newTestDataViewer()
	m.AddTab(DataResultMsg{
		TableName: "t", IsPrimary: true,
		Columns: []string{"id"}, Rows: [][]string{{"1"}},
		RowCount: 1, Truncated: true,
	})
	if out := m.View(80, 24); !strings.Contains(out, "truncated") {
		t.Errorf("truncated result should be marked\n%s", out)
	}
}

// TestDataViewModel_Render_RaggedRows is the regression guard for the panic that
// (combined with the absence of a recover) blanked the whole TUI: rows whose
// length disagrees with the column count must not panic.
func TestDataViewModel_Render_RaggedRows(t *testing.T) {
	m := newTestDataViewer()
	m.AddTab(DataResultMsg{
		TableName: "ragged",
		IsPrimary: true,
		Columns:   []string{"a", "b", "c"},
		Rows: [][]string{
			{"1"},                // fewer cells than columns
			{"1", "2", "3", "4"}, // more cells than columns
			{"1", "2", "3"},      // exact
		},
		RowCount: 3,
	})
	// Must not panic.
	out := m.View(80, 24)
	if out == "" {
		t.Error("expected non-empty render for ragged rows")
	}
}

func TestDataViewModel_Render_ZeroDimsNoPanic(t *testing.T) {
	m := newTestDataViewer()
	m.AddTab(DataResultMsg{TableName: "t", IsPrimary: true, Columns: []string{"id"}, Rows: [][]string{{"1"}}, RowCount: 1})
	// Degenerate dims must not panic (defensive against early frames).
	_ = m.View(0, 0)
	_ = m.View(1, 1)
}

func TestDataViewModel_EmptyState(t *testing.T) {
	m := newTestDataViewer()
	if out := m.View(80, 24); !strings.Contains(out, "No data results") {
		t.Errorf("expected empty-state message, got %q", out)
	}
	// Tab with no columns.
	m.AddTab(DataResultMsg{TableName: "empty", IsPrimary: true, Columns: nil, Rows: nil})
	if out := m.View(80, 24); !strings.Contains(out, "empty result") {
		t.Errorf("expected empty-result message, got %q", out)
	}
}

func TestDataViewModel_Lifecycle_EscCloses(t *testing.T) {
	m := newTestDataViewer()
	m.AddTab(DataResultMsg{TableName: "t", IsPrimary: true, Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	if !m.Visible() {
		t.Fatal("overlay should be visible after AddTab")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.Visible() {
		t.Error("Esc should close the overlay")
	}
}

func TestDataViewModel_Reopen(t *testing.T) {
	m := newTestDataViewer()
	// Nothing to reopen before any query.
	if m.Reopen() {
		t.Error("Reopen should return false when there are no tabs")
	}
	m.AddTab(DataResultMsg{TableName: "t", IsPrimary: true, Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.Visible() {
		t.Fatal("Esc should have closed the overlay")
	}
	// Tabs survive Esc → Reopen brings the overlay back.
	if !m.Reopen() {
		t.Error("Reopen should return true when cached tabs exist")
	}
	if !m.Visible() {
		t.Error("overlay should be visible again after Reopen")
	}
}

func TestDataViewModel_TabNavigation(t *testing.T) {
	m := newTestDataViewer()
	m.AddTab(DataResultMsg{TableName: "a", IsPrimary: true, Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	m.AddTab(DataResultMsg{TableName: "b", Columns: []string{"id"}, Rows: [][]string{{"1"}}})
	m.AddTab(DataResultMsg{TableName: "c", Columns: []string{"id"}, Rows: [][]string{{"1"}}})

	m.activeTab = 0
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	if m.activeTab != 1 {
		t.Errorf("']' should move to tab 1, got %d", m.activeTab)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	if m.activeTab != 0 {
		t.Errorf("'[' should move back to tab 0, got %d", m.activeTab)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.activeTab != 2 {
		t.Errorf("'3' should jump to tab index 2, got %d", m.activeTab)
	}
}

func TestDataViewModel_ColumnWidthCap(t *testing.T) {
	m := newTestDataViewer()
	long := strings.Repeat("x", 100)
	m.AddTab(DataResultMsg{
		TableName: "t", IsPrimary: true,
		Columns: []string{"col"}, Rows: [][]string{{long}}, RowCount: 1,
	})
	tab := m.activeTabData()
	widths := m.computeColumnWidths(tab)
	// 30 cap + 2 padding
	if widths[0] > 32 {
		t.Errorf("column width should be capped at 32 (30+pad), got %d", widths[0])
	}
}

// TestParseQueryResult_TableName verifies the table_name field flows from the
// query_database tool JSON into DataResultMsg.TableName (so the tab is labelled).
func TestParseQueryResult_TableName(t *testing.T) {
	tool := "[query_result]\n" +
		`{"columns":["id"],"rows":[["1"]],"row_count":1,"truncated":false,` +
		`"database":"bizdb","namespace":"ns","pod_name":"app-1","table_name":"users"}` +
		"\n[/query_result]\n"
	msg, ok := parseQueryResult(tool)
	if !ok {
		t.Fatal("expected parseQueryResult to succeed")
	}
	if msg.TableName != "users" {
		t.Errorf("TableName = %q, want %q", msg.TableName, "users")
	}
	if msg.Database != "bizdb" || msg.PodName != "app-1" {
		t.Errorf("unexpected db/pod: %q/%q", msg.Database, msg.PodName)
	}
}
