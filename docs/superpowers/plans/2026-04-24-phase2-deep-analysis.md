# Phase 2: Deep Analysis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AI analysis enhancement (severity_stats, compare_logs, get_pod_metrics tools), conversation history persistence (modernc.org/sqlite + FTS5), cross-service trace correlation (trace_logs tool), and AI Chat Markdown rendering (glamour, complete-then-render).

**Architecture:** New `internal/history` module (Layer 1) handles SQLite persistence with FTS5 search. Agent gets 4 new analysis tools. Chat model gains glamour-based Markdown rendering with complete-then-render strategy (raw text during streaming, glamour render after EventComplete). System prompt gets structured analysis instructions.

**Tech Stack:** Go 1.24, modernc.org/sqlite (pure Go, no CGO), charmbracelet/glamour (Markdown terminal rendering), testify

**Module path:** `github.com/Orwell-Yu/korthex`

---

## File Structure

| Action | Path | Responsibility |
|--------|------|---------------|
| Create | `internal/history/history.go` | History store: open/close DB, session lifecycle, message CRUD |
| Create | `internal/history/schema.go` | DDL: sessions, messages, messages_fts tables |
| Create | `internal/history/search.go` | FTS5 search, namespace filter, time filter parsing |
| Create | `internal/history/history_test.go` | Tests for store operations and FTS search |
| Modify | `internal/config/config.go:13-18` | Add `History` and `Analysis` config sections |
| Modify | `configs/default.yaml` | Add history and analysis config defaults |
| Modify | `internal/agent/tools.go:27-149` | Add severity_stats, compare_logs, get_pod_metrics, trace_logs tool defs + handlers |
| Modify | `internal/agent/safety.go:9-24` | Add 4 new tools to whitelist |
| Modify | `internal/agent/prompt.go` | Add analysis_mode instructions, structured output format |
| Modify | `internal/ui/chat.go:17-51,443-480` | Add glamour renderer, MarkdownRenderedMsg, complete-then-render |
| Create | `internal/ui/markdown.go` | Glamour renderer wrapper: theme mapping, async render, cache |
| Create | `internal/ui/markdown_test.go` | Tests for markdown rendering |
| Create | `internal/ui/historypanel.go` | History search overlay: FTS query, result display, context injection |
| Modify | `internal/ui/messages.go` | Add MarkdownRenderedMsg, HistorySearchMsg types |
| Modify | `internal/app/app.go:26-32,36-101` | Wire history store, glamour, cleanup on shutdown |

---

### Task 1: History Store — Schema and Core

**Files:**
- Create: `internal/history/schema.go`
- Create: `internal/history/history.go`
- Create: `internal/history/history_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/history/history_test.go`:

```go
package history

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempDBPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test-history.db")
}

func TestStore_OpenClose(t *testing.T) {
	store, err := Open(tempDBPath(t))
	require.NoError(t, err)
	require.NotNil(t, store)
	err = store.Close()
	assert.NoError(t, err)
}

func TestStore_CreateSession(t *testing.T) {
	store, err := Open(tempDBPath(t))
	require.NoError(t, err)
	defer store.Close()

	sessionID, err := store.CreateSession("test-context")
	require.NoError(t, err)
	assert.NotEmpty(t, sessionID)
}

func TestStore_AddMessage(t *testing.T) {
	store, err := Open(tempDBPath(t))
	require.NoError(t, err)
	defer store.Close()

	sid, _ := store.CreateSession("test-context")
	err = store.AddMessage(sid, "user", "查 order-service 的日志", "production")
	assert.NoError(t, err)

	err = store.AddMessage(sid, "assistant", "找到了 18 条 ConnectionTimeout", "production")
	assert.NoError(t, err)
}

func TestStore_EndSession(t *testing.T) {
	store, err := Open(tempDBPath(t))
	require.NoError(t, err)
	defer store.Close()

	sid, _ := store.CreateSession("test-context")
	store.AddMessage(sid, "user", "test query", "")

	err = store.EndSession(sid, "测试会话摘要")
	assert.NoError(t, err)
}

func TestStore_CleanupOld(t *testing.T) {
	store, err := Open(tempDBPath(t))
	require.NoError(t, err)
	defer store.Close()

	// Cleanup with 0 days retention should remove everything
	sid, _ := store.CreateSession("test-context")
	store.AddMessage(sid, "user", "old message", "")
	store.EndSession(sid, "old session")

	deleted, err := store.Cleanup(0)
	assert.NoError(t, err)
	assert.Equal(t, 1, deleted)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/history/... -v -run "TestStore"`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement schema**

Create `internal/history/schema.go`:

```go
package history

const schemaSQL = `
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    cluster     TEXT NOT NULL,
    started_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at    DATETIME,
    summary     TEXT
);

CREATE TABLE IF NOT EXISTS messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    role        TEXT NOT NULL,
    content     TEXT NOT NULL,
    namespace   TEXT,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    content,
    namespace,
    content='messages',
    content_rowid='id'
);

-- Triggers to keep FTS index in sync
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, content, namespace) VALUES (new.id, new.content, new.namespace);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, content, namespace) VALUES('delete', old.id, old.content, old.namespace);
END;

CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);
CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_cluster ON sessions(cluster);
`
```

- [ ] **Step 4: Implement store**

Create `internal/history/history.go`:

```go
package history

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Store provides conversation history persistence backed by SQLite.
type Store struct {
	db *sql.DB
}

// Open creates or opens a history database at the given path.
func Open(dbPath string) (*Store, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create history dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open history db: %w", err)
	}

	// Apply schema
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	slog.Debug("history store opened", "path", dbPath)
	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// CreateSession starts a new conversation session.
func (s *Store) CreateSession(cluster string) (string, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(
		"INSERT INTO sessions (id, cluster) VALUES (?, ?)",
		id, cluster,
	)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

// AddMessage adds a message to an existing session.
func (s *Store) AddMessage(sessionID, role, content, namespace string) error {
	_, err := s.db.Exec(
		"INSERT INTO messages (session_id, role, content, namespace) VALUES (?, ?, ?, ?)",
		sessionID, role, content, namespace,
	)
	if err != nil {
		return fmt.Errorf("add message: %w", err)
	}
	return nil
}

// EndSession marks a session as ended with a summary.
func (s *Store) EndSession(sessionID, summary string) error {
	_, err := s.db.Exec(
		"UPDATE sessions SET ended_at = datetime('now'), summary = ? WHERE id = ?",
		summary, sessionID,
	)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

// Cleanup deletes sessions older than retentionDays. Returns count of deleted sessions.
func (s *Store) Cleanup(retentionDays int) (int, error) {
	result, err := s.db.Exec(
		"DELETE FROM sessions WHERE started_at < datetime('now', ? || ' days')",
		fmt.Sprintf("-%d", retentionDays),
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup: %w", err)
	}
	rows, _ := result.RowsAffected()

	// Also clean orphaned messages
	s.db.Exec("DELETE FROM messages WHERE session_id NOT IN (SELECT id FROM sessions)")

	slog.Debug("history cleanup", "deleted_sessions", rows, "retention_days", retentionDays)
	return int(rows), nil
}
```

- [ ] **Step 5: Add dependencies**

Run: `go get modernc.org/sqlite && go get github.com/google/uuid`

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/history/... -v -run "TestStore"`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/history/ go.mod go.sum
git commit -m "feat(history): SQLite-backed conversation history store with FTS5"
```

---

### Task 2: History Search

**Files:**
- Create: `internal/history/search.go`
- Modify: `internal/history/history_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/history/history_test.go`:

```go
func TestStore_SearchFTS(t *testing.T) {
	store, err := Open(tempDBPath(t))
	require.NoError(t, err)
	defer store.Close()

	sid, _ := store.CreateSession("test-context")
	store.AddMessage(sid, "user", "查 order-service 的 timeout 错误", "production")
	store.AddMessage(sid, "assistant", "找到 18 条 ConnectionTimeout，根因是连接池耗尽", "production")
	store.EndSession(sid, "order-service timeout: 连接池耗尽")

	results, err := store.Search("timeout")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, sid, results[0].SessionID)
	assert.Contains(t, results[0].Summary, "timeout")
}

func TestStore_SearchByNamespace(t *testing.T) {
	store, err := Open(tempDBPath(t))
	require.NoError(t, err)
	defer store.Close()

	sid1, _ := store.CreateSession("ctx1")
	store.AddMessage(sid1, "user", "查 order-service", "production")
	store.EndSession(sid1, "production order-service")

	sid2, _ := store.CreateSession("ctx1")
	store.AddMessage(sid2, "user", "查 payment-service", "staging")
	store.EndSession(sid2, "staging payment-service")

	results, err := store.SearchWithFilter("order", SearchFilter{Namespace: "production"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, sid1, results[0].SessionID)
}

func TestStore_ListRecentSessions(t *testing.T) {
	store, err := Open(tempDBPath(t))
	require.NoError(t, err)
	defer store.Close()

	sid1, _ := store.CreateSession("ctx1")
	store.AddMessage(sid1, "user", "first session", "")
	store.EndSession(sid1, "first")

	sid2, _ := store.CreateSession("ctx1")
	store.AddMessage(sid2, "user", "second session", "")
	store.EndSession(sid2, "second")

	sessions, err := store.ListRecent(10)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	// Most recent first
	assert.Equal(t, sid2, sessions[0].ID)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/history/... -v -run "TestStore_Search|TestStore_List"`
Expected: FAIL — `Search`, `SearchWithFilter`, `ListRecent` undefined

- [ ] **Step 3: Implement search**

Create `internal/history/search.go`:

```go
package history

import (
	"fmt"
	"strings"
	"time"
)

// SearchResult represents a matching session from a history search.
type SearchResult struct {
	SessionID string
	Cluster   string
	StartedAt time.Time
	Summary   string
	Snippet   string // matching text snippet
}

// SearchFilter provides optional filters for history search.
type SearchFilter struct {
	Namespace string
	After     *time.Time
}

// SessionSummary is a lightweight session record for listing.
type SessionSummary struct {
	ID        string
	Cluster   string
	StartedAt time.Time
	Summary   string
}

// Search performs a full-text search across message content.
func (s *Store) Search(query string) ([]SearchResult, error) {
	return s.SearchWithFilter(query, SearchFilter{})
}

// SearchWithFilter performs a full-text search with optional namespace and time filters.
func (s *Store) SearchWithFilter(query string, filter SearchFilter) ([]SearchResult, error) {
	// Parse ns: and after: prefixes from query
	actualQuery := query
	tokens := strings.Fields(query)
	var remaining []string
	for _, tok := range tokens {
		if strings.HasPrefix(tok, "ns:") && filter.Namespace == "" {
			filter.Namespace = strings.TrimPrefix(tok, "ns:")
		} else if strings.HasPrefix(tok, "after:") && filter.After == nil {
			if t, err := time.Parse("2006-01-02", strings.TrimPrefix(tok, "after:")); err == nil {
				filter.After = &t
			}
		} else {
			remaining = append(remaining, tok)
		}
	}
	actualQuery = strings.Join(remaining, " ")

	var args []interface{}
	var conditions []string

	// FTS match
	if actualQuery != "" {
		conditions = append(conditions, "messages_fts MATCH ?")
		args = append(args, actualQuery)
	}

	// Namespace filter
	if filter.Namespace != "" {
		conditions = append(conditions, "m.namespace = ?")
		args = append(args, filter.Namespace)
	}

	// Time filter
	if filter.After != nil {
		conditions = append(conditions, "s.started_at >= ?")
		args = append(args, filter.After.Format("2006-01-02 15:04:05"))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	sql := fmt.Sprintf(`
		SELECT DISTINCT s.id, s.cluster, s.started_at, COALESCE(s.summary, ''),
		       snippet(messages_fts, 0, '>>>', '<<<', '...', 64)
		FROM messages m
		JOIN sessions s ON m.session_id = s.id
		JOIN messages_fts ON messages_fts.rowid = m.id
		%s
		ORDER BY s.started_at DESC
		LIMIT 20
	`, where)

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search history: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var startedAt string
		if err := rows.Scan(&r.SessionID, &r.Cluster, &startedAt, &r.Summary, &r.Snippet); err != nil {
			continue
		}
		r.StartedAt, _ = time.Parse("2006-01-02 15:04:05", startedAt)
		results = append(results, r)
	}
	return results, nil
}

// ListRecent returns the most recent sessions.
func (s *Store) ListRecent(limit int) ([]SessionSummary, error) {
	rows, err := s.db.Query(
		"SELECT id, cluster, started_at, COALESCE(summary, '') FROM sessions ORDER BY started_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recent: %w", err)
	}
	defer rows.Close()

	var sessions []SessionSummary
	for rows.Next() {
		var ss SessionSummary
		var startedAt string
		if err := rows.Scan(&ss.ID, &ss.Cluster, &startedAt, &ss.Summary); err != nil {
			continue
		}
		ss.StartedAt, _ = time.Parse("2006-01-02 15:04:05", startedAt)
		sessions = append(sessions, ss)
	}
	return sessions, nil
}

// GetSessionMessages returns all messages for a given session.
func (s *Store) GetSessionMessages(sessionID string) ([]SessionMessage, error) {
	rows, err := s.db.Query(
		"SELECT role, content, COALESCE(namespace, ''), created_at FROM messages WHERE session_id = ? ORDER BY id",
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get session messages: %w", err)
	}
	defer rows.Close()

	var msgs []SessionMessage
	for rows.Next() {
		var m SessionMessage
		var createdAt string
		if err := rows.Scan(&m.Role, &m.Content, &m.Namespace, &createdAt); err != nil {
			continue
		}
		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// SessionMessage is a message within a session.
type SessionMessage struct {
	Role      string
	Content   string
	Namespace string
	CreatedAt time.Time
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/history/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/history/search.go internal/history/history_test.go
git commit -m "feat(history): add FTS5 search with namespace/time filters"
```

---

### Task 3: Config Extension for History and Analysis

**Files:**
- Modify: `internal/config/config.go:13-18`
- Modify: `configs/default.yaml`

- [ ] **Step 1: Add config types**

In `internal/config/config.go`, extend the `Config` struct (after line 18):

```go
type Config struct {
	Kubernetes KubernetesConfig
	LLM        LLMConfig
	Agent      AgentConfig
	UI         UIConfig
	// Phase 2
	Privacy  PrivacyConfig
	History  HistoryConfig
	Analysis AnalysisConfig
}
```

Add the new config types (after existing config type definitions):

```go
// HistoryConfig controls conversation history persistence.
type HistoryConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	RetentionDays int    `mapstructure:"retention_days"`
	DBPath        string `mapstructure:"db_path"`
}

// AnalysisConfig controls AI analysis behavior.
type AnalysisConfig struct {
	TraceIDPatterns []TraceIDPatternConfig `mapstructure:"trace_id_patterns"`
}

// TraceIDPatternConfig defines a custom trace ID regex pattern.
type TraceIDPatternConfig struct {
	Name    string `mapstructure:"name"`
	Pattern string `mapstructure:"pattern"`
}
```

Note: `PrivacyConfig` is already defined by the Data Safety plan — only add `HistoryConfig` and `AnalysisConfig` if `PrivacyConfig` hasn't been added yet.

- [ ] **Step 2: Add defaults**

In the `applyDefaults()` function, add:

```go
	// History defaults
	if !v.IsSet("history.enabled") {
		v.SetDefault("history.enabled", true)
	}
	if !v.IsSet("history.retention_days") {
		v.SetDefault("history.retention_days", 30)
	}
	if !v.IsSet("history.db_path") {
		home, _ := os.UserHomeDir()
		v.SetDefault("history.db_path", filepath.Join(home, ".korthex", "history.db"))
	}
```

- [ ] **Step 3: Update default.yaml**

Add to `configs/default.yaml`:

```yaml
# Phase 2: Conversation history
history:
  enabled: true                    # persist conversation history
  retention_days: 30               # auto-delete sessions older than this
  db_path: ~/.korthex/history.db   # SQLite database path

# Phase 2: Analysis configuration
analysis:
  trace_id_patterns: []            # custom trace ID regex patterns
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./internal/config/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go configs/default.yaml
git commit -m "feat(config): add history and analysis config sections"
```

---

### Task 4: AI Analysis Tools — severity_stats and compare_logs

**Files:**
- Modify: `internal/agent/tools.go`
- Modify: `internal/agent/safety.go`
- Create: `internal/agent/tools_analysis_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/agent/tools_analysis_test.go`:

```go
package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Orwell-Yu/korthex/internal/k8s"
)

// mockLogBuffer implements LogBufferReader for testing severity_stats.
type mockLogBuffer struct {
	lines []string
}

func (m *mockLogBuffer) Len() int { return len(m.lines) }
func (m *mockLogBuffer) Slice() []interface{} {
	result := make([]interface{}, len(m.lines))
	for i, l := range m.lines {
		result[i] = l
	}
	return result
}

func TestToolExecution_SeverityStats(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	executor := NewToolExecutor(mockK8s)

	// Set up log buffer with test data
	te := executor.(*toolExecutor)
	te.logBuffer = &mockLogBuffer{
		lines: []string{
			"2024-01-01T14:00:00Z INFO Starting service",
			"2024-01-01T14:01:00Z ERROR NullPointerException",
			"2024-01-01T14:02:00Z WARN Memory usage high",
			"2024-01-01T14:03:00Z ERROR ConnectionTimeout",
			"2024-01-01T14:04:00Z INFO Request processed",
		},
	}

	result, _, err := executor.ExecuteTool(context.Background(), "severity_stats", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Total lines: 5") {
		t.Errorf("should contain total line count, got: %s", result)
	}
	if !strings.Contains(result, "ERROR") {
		t.Errorf("should contain ERROR severity, got: %s", result)
	}
}

func TestToolExecution_CompareLogs(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			FindDeploymentByNameFunc: func(ns, name string) (*k8s.Deployment, error) {
				return &k8s.Deployment{
					Name:     "order-service",
					Selector: map[string]string{"app": "order-service"},
				}, nil
			},
			ListPodsBySelectorFunc: func(ns string, selector map[string]string) ([]k8s.Pod, error) {
				return []k8s.Pod{{Name: "order-svc-abc", Namespace: ns}}, nil
			},
		},
		MockLogs: &k8s.MockLogStreamer{
			GetLogsFunc: func(ctx context.Context, req k8s.LogRequest) ([]k8s.LogLine, error) {
				return []k8s.LogLine{
					{PodName: "order-svc-abc", Content: "2024-01-01T14:00:00Z ERROR timeout"},
					{PodName: "order-svc-abc", Content: "2024-01-01T14:01:00Z INFO ok"},
				}, nil
			},
		},
	}

	executor := NewToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "compare_logs",
		map[string]string{
			"namespace": "production",
			"selector":  "app=order-service",
			"since1":    "2h",
			"since2":    "26h",
			"duration":  "1h",
		})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Period 1") {
		t.Errorf("should contain period comparison, got: %s", result)
	}
}

func TestSafety_AnalysisToolsAllowed(t *testing.T) {
	checker := NewSafetyChecker()
	tools := []string{"severity_stats", "compare_logs", "get_pod_metrics", "trace_logs"}
	for _, tool := range tools {
		level, reason := checker.Check(tool, nil)
		if level != SafetyAllowed {
			t.Errorf("tool %s should be allowed, got level=%d reason=%s", tool, level, reason)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/... -v -run "TestToolExecution_(SeverityStats|CompareLogs)|TestSafety_Analysis"`
Expected: FAIL — tools not defined yet

- [ ] **Step 3: Add tool definitions**

In `internal/agent/tools.go`, add to `ToolDefinitions()`:

```go
		// Phase 2: analysis tools
		{
			Name:        "severity_stats",
			Description: "Compute severity distribution statistics from the Log Viewer buffer. Use this for quantitative analysis before making claims about error rates or log patterns.",
			Parameters: []llm.ParameterDef{
				{Name: "sinceMinutes", Type: "string", Description: "Only count lines from the last N minutes (default: all buffer)", Required: false},
			},
		},
		{
			Name:        "compare_logs",
			Description: "Compare log patterns between two time periods for the same service. Useful for answering 'is today worse than yesterday' type questions.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "selector", Type: "string", Description: "Label selector (e.g., app=order-service)", Required: true},
				{Name: "since1", Type: "string", Description: "Period 1 start as duration from now (e.g., '2h' = 2 hours ago)", Required: true},
				{Name: "since2", Type: "string", Description: "Period 2 start as duration from now (e.g., '26h' = 26 hours ago)", Required: true},
				{Name: "duration", Type: "string", Description: "Duration of each period (e.g., '1h')", Required: true},
			},
		},
		{
			Name:        "get_pod_metrics",
			Description: "Get current CPU and memory usage for a pod's containers from the Metrics API. Returns '-' if Metrics Server is not installed.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "podName", Type: "string", Description: "Pod name", Required: true},
			},
		},
		{
			Name:        "trace_logs",
			Description: "Search for a trace ID or request ID across multiple namespaces to build a cross-service call timeline. Use when investigating a specific request's journey.",
			Parameters: []llm.ParameterDef{
				{Name: "traceId", Type: "string", Description: "The trace ID, request ID, or correlation ID to search for", Required: true},
				{Name: "namespaces", Type: "string", Description: "Comma-separated namespaces to search (default: all namespaces)", Required: false},
				{Name: "timeRange", Type: "string", Description: "How far back to search (default: 1h)", Required: false},
			},
		},
```

- [ ] **Step 4: Add dispatch cases**

In `ExecuteTool`, add before `default:`:

```go
	case "severity_stats":
		return t.severityStats(args)
	case "compare_logs":
		return t.compareLogs(ctx, args)
	case "get_pod_metrics":
		return t.getPodMetrics(args)
	case "trace_logs":
		return t.traceLogs(ctx, args)
```

- [ ] **Step 5: Implement severity_stats handler**

```go
func (t *toolExecutor) severityStats(args map[string]string) (string, []k8s.LogLine, error) {
	if t.logBuffer == nil {
		return "Log Viewer buffer is empty. Load logs first using kubectl_logs.", nil, nil
	}

	entries := t.logBuffer.Slice()
	if len(entries) == 0 {
		return "Log Viewer buffer is empty (0 lines).", nil, nil
	}

	parser := logparse.NewParser()
	severityCounts := map[string]int{}
	errorPatterns := map[string]int{}
	total := 0
	var firstTimestamp, lastTimestamp string

	// Per-5min buckets for spike detection
	type bucket struct {
		minute int
		errors int
	}
	errorBuckets := map[int]int{} // minute-of-hour → error count

	sinceMinutes := 0
	if sm := args["sinceMinutes"]; sm != "" {
		fmt.Sscanf(sm, "%d", &sinceMinutes)
	}

	for _, entry := range entries {
		line, ok := entry.(string)
		if !ok {
			continue
		}
		total++
		// Parse with empty podName/container — severity_stats only needs severity + message
		parsed := parser.Parse("", "", line)
		sev := severityName(parsed.Severity)
		severityCounts[sev]++

		// Track time range from parsed timestamps
		if !parsed.Timestamp.IsZero() {
			ts := parsed.Timestamp.Format("15:04:05")
			if firstTimestamp == "" {
				firstTimestamp = ts
			}
			lastTimestamp = ts
		}

		// Collect error patterns (first meaningful token after ERROR/FATAL)
		if parsed.Severity == logparse.SeverityError || parsed.Severity == logparse.SeverityFatal {
			pattern := extractErrorPattern(parsed.Raw)
			if pattern != "" {
				errorPatterns[pattern]++
			}
			// Track error distribution by 5-min bucket for spike detection
			if !parsed.Timestamp.IsZero() {
				bucketKey := parsed.Timestamp.Hour()*60 + parsed.Timestamp.Minute()/5*5
				errorBuckets[bucketKey]++
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Total lines: %d\n", total)
	if firstTimestamp != "" && lastTimestamp != "" {
		fmt.Fprintf(&b, "Time range: %s - %s\n", firstTimestamp, lastTimestamp)
	}
	fmt.Fprintf(&b, "Severity distribution:\n")
	for _, sev := range []string{"FATAL", "ERROR", "WARN", "INFO", "DEBUG", "UNKNOWN"} {
		if count, ok := severityCounts[sev]; ok {
			pct := float64(count) / float64(total) * 100
			// Detect spikes: find 5-min bucket with >50% of all errors
			spikeSuffix := ""
			if sev == "ERROR" || sev == "FATAL" {
				totalErrors := count
				for bucketKey, bucketCount := range errorBuckets {
					if totalErrors > 5 && bucketCount > totalErrors/2 {
						h, m := bucketKey/60, bucketKey%60
						spikeSuffix = fmt.Sprintf("  [spike at %02d:%02d-%02d:%02d: %d lines]", h, m, h, m+5, bucketCount)
					}
				}
			}
			fmt.Fprintf(&b, "  %-8s %d (%.1f%%)%s\n", sev+":", count, pct, spikeSuffix)
		}
	}

	if len(errorPatterns) > 0 {
		fmt.Fprintf(&b, "Top error patterns:\n")
		for pattern, count := range errorPatterns {
			fmt.Fprintf(&b, "  %s: %d occurrences\n", pattern, count)
		}
	}

	return b.String(), nil, nil
}

// severityName maps logparse.Severity to display string.
func severityName(s logparse.Severity) string {
	switch s {
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
	default:
		return "UNKNOWN"
	}
}

func extractErrorPattern(msg string) string {
	// Extract the first exception/error class name from the message
	patterns := []string{
		`([A-Z][a-zA-Z]*Exception)`,
		`([A-Z][a-zA-Z]*Error)`,
		`([A-Z][a-zA-Z]*Timeout)`,
		`([A-Z][a-zA-Z]*Failure)`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if match := re.FindString(msg); match != "" {
			return match
		}
	}
	// Fallback: first 50 chars of the message
	if len(msg) > 50 {
		return msg[:50] + "..."
	}
	return msg
}
```

- [ ] **Step 6: Implement compare_logs handler**

```go
func (t *toolExecutor) compareLogs(ctx context.Context, args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	selector := args["selector"]
	if ns == "" || selector == "" {
		return "", nil, fmt.Errorf("namespace and selector are required")
	}

	since1, err := time.ParseDuration(args["since1"])
	if err != nil {
		return "", nil, fmt.Errorf("invalid since1 duration: %w", err)
	}
	since2, err := time.ParseDuration(args["since2"])
	if err != nil {
		return "", nil, fmt.Errorf("invalid since2 duration: %w", err)
	}
	duration, err := time.ParseDuration(args["duration"])
	if err != nil {
		return "", nil, fmt.Errorf("invalid duration: %w", err)
	}

	// Parse selector string into map and find pods
	selMap := parseSelector(selector)
	pods, err := t.k8sClient.Resources().ListPodsBySelector(ns, selMap)
	if err != nil {
		return "", nil, fmt.Errorf("list pods by selector: %w", err)
	}
	if len(pods) == 0 {
		return "No pods found matching selector " + selector + " in namespace " + ns, nil, nil
	}

	// Fetch logs for each pod in period 1 (recent)
	now := time.Now()
	p1Start := now.Add(-since1)
	var logs1 []k8s.LogLine
	for _, pod := range pods {
		podLogs, err := t.k8sClient.Logs().GetLogs(ctx, k8s.LogRequest{
			Namespace: ns,
			PodName:   pod.Name,
			SinceTime: &p1Start,
		})
		if err != nil {
			continue
		}
		logs1 = append(logs1, podLogs...)
	}

	// Fetch logs for period 2 (older)
	p2Start := now.Add(-since2)
	var logs2 []k8s.LogLine
	var p2Err error
	for _, pod := range pods {
		podLogs, err := t.k8sClient.Logs().GetLogs(ctx, k8s.LogRequest{
			Namespace: ns,
			PodName:   pod.Name,
			SinceTime: &p2Start,
		})
		if err != nil {
			p2Err = err
			continue
		}
		logs2 = append(logs2, podLogs...)
	}

	parser := logparse.NewParser()
	p1Pods := len(pods)

	// Count severities and collect error patterns for each period
	counts1, patterns1 := countSeveritiesAndPatterns(logs1, parser)
	counts2, patterns2 := countSeveritiesAndPatterns(logs2, parser)

	var b strings.Builder
	fmt.Fprintf(&b, "Period 1 (%s ago, %s): %d lines (%d pods)", args["since1"], args["duration"], len(logs1), p1Pods)
	for _, sev := range []string{"ERROR", "WARN", "INFO"} {
		if count, ok := counts1[sev]; ok {
			fmt.Fprintf(&b, ", %d %s", count, sev)
		}
	}
	b.WriteString("\n")

	if p2Err != nil && len(logs2) == 0 {
		fmt.Fprintf(&b, "Period 2 (%s ago): logs unavailable (likely rotated)\n", args["since2"])
	} else {
		fmt.Fprintf(&b, "Period 2 (%s ago, %s): %d lines", args["since2"], args["duration"], len(logs2))
		for _, sev := range []string{"ERROR", "WARN", "INFO"} {
			if count, ok := counts2[sev]; ok {
				fmt.Fprintf(&b, ", %d %s", count, sev)
			}
		}
		b.WriteString("\n")

		// Volume skew warning
		if len(logs2) > 0 && len(logs1)/len(logs2) > 10 || (len(logs1) > 0 && len(logs2)/len(logs1) > 10) {
			b.WriteString("⚠️ Note: volume difference >10x may skew comparison\n")
		}

		// Delta analysis
		b.WriteString("Delta:\n")
		for _, sev := range []string{"ERROR", "WARN"} {
			c1, c2 := counts1[sev], counts2[sev]
			if c2 > 0 {
				change := float64(c1-c2) / float64(c2) * 100
				marker := ""
				if change > 50 {
					marker = " ⚠️ significant increase"
				}
				fmt.Fprintf(&b, "  %s: %+.1f%% (%d → %d)%s\n", sev, change, c2, c1, marker)
			} else if c1 > 0 {
				fmt.Fprintf(&b, "  %s: new (%d in period 1, 0 in period 2)\n", sev, c1)
			}
		}

		// New error patterns in period 1
		var newPatterns []string
		for pattern := range patterns1 {
			if _, found := patterns2[pattern]; !found {
				newPatterns = append(newPatterns, fmt.Sprintf("  - %s (%d occurrences)", pattern, patterns1[pattern]))
			}
		}
		if len(newPatterns) > 0 {
			b.WriteString("New error patterns in Period 1 (not seen in Period 2):\n")
			for _, p := range newPatterns {
				b.WriteString(p + "\n")
			}
		}
	}

	return b.String(), nil, nil
}

func countSeveritiesAndPatterns(logs []k8s.LogLine, parser logparse.Parser) (map[string]int, map[string]int) {
	counts := map[string]int{}
	patterns := map[string]int{}
	for _, l := range logs {
		parsed := parser.Parse(l.PodName, l.Container, l.Content)
		sev := severityName(parsed.Severity)
		counts[sev]++
		if parsed.Severity == logparse.SeverityError || parsed.Severity == logparse.SeverityFatal {
			pattern := extractErrorPattern(parsed.Raw)
			if pattern != "" {
				patterns[pattern]++
			}
		}
	}
	return counts, patterns
}

func parseSelector(sel string) map[string]string {
	result := map[string]string{}
	for _, pair := range strings.Split(sel, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}
```

- [ ] **Step 7: Implement get_pod_metrics handler**

```go
func (t *toolExecutor) getPodMetrics(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	podName := args["podName"]
	if ns == "" || podName == "" {
		return "", nil, fmt.Errorf("namespace and podName are required")
	}

	metrics := t.k8sClient.Metrics()
	if metrics == nil || !metrics.IsAvailable() {
		return "Metrics API (metrics.k8s.io) not available. Metrics Server may not be installed.", nil, nil
	}

	pm, err := metrics.GetPodMetrics(ns, podName)
	if err != nil {
		return "", nil, fmt.Errorf("get metrics: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Pod: %s/%s\n", ns, podName)
	fmt.Fprintf(&b, "CONTAINER\tCPU\tMEMORY\n")
	for _, c := range pm.Containers {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", c.Name, c.CPUUsage, c.MemUsage)
	}
	return b.String(), nil, nil
}
```

- [ ] **Step 8: Add to safety whitelist**

In `internal/agent/safety.go`, add:

```go
		"severity_stats":   SafetyAllowed,
		"compare_logs":     SafetyAllowed,
		"get_pod_metrics":  SafetyAllowed,
		"trace_logs":       SafetyAllowed,
```

- [ ] **Step 9: Add to GenerateCommandDisplay**

```go
	case "severity_stats":
		cmd := "[TUI] Log Viewer severity distribution"
		if sm := args["sinceMinutes"]; sm != "" {
			cmd += fmt.Sprintf(" (last %s min)", sm)
		}
		return cmd
	case "compare_logs":
		return fmt.Sprintf("kubectl logs -l %s -n %s (comparing %s ago vs %s ago)",
			args["selector"], args["namespace"], args["since1"], args["since2"])
	case "get_pod_metrics":
		return fmt.Sprintf("kubectl top pod %s -n %s", args["podName"], args["namespace"])
	case "trace_logs":
		cmd := fmt.Sprintf("[Trace] grep %q across namespaces", args["traceId"])
		if ns := args["namespaces"]; ns != "" {
			cmd += fmt.Sprintf(" (%s)", ns)
		}
		return cmd
```

- [ ] **Step 10: Run tests to verify they pass**

Run: `go test ./internal/agent/... -v -run "TestToolExecution_(SeverityStats|CompareLogs)|TestSafety_Analysis"`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add internal/agent/tools.go internal/agent/safety.go internal/agent/tools_analysis_test.go
git commit -m "feat(agent): add severity_stats, compare_logs, get_pod_metrics, trace_logs tools"
```

---

### Task 5: Trace Logs Tool

**Files:**
- Modify: `internal/agent/tools.go`

- [ ] **Step 1: Implement trace_logs handler**

This is the most complex tool — it searches across namespaces concurrently with semaphore control.

```go
func (t *toolExecutor) traceLogs(ctx context.Context, args map[string]string) (string, []k8s.LogLine, error) {
	traceID := args["traceId"]
	if traceID == "" {
		return "", nil, fmt.Errorf("traceId is required")
	}

	// Determine namespaces to search
	var namespaces []string
	if ns := args["namespaces"]; ns != "" {
		namespaces = strings.Split(ns, ",")
		for i := range namespaces {
			namespaces[i] = strings.TrimSpace(namespaces[i])
		}
	} else {
		// Search all namespaces
		nsList, err := t.k8sClient.Resources().ListNamespaces()
		if err != nil {
			return "", nil, fmt.Errorf("list namespaces: %w", err)
		}
		for _, ns := range nsList {
			if ns.Status == "Active" {
				namespaces = append(namespaces, ns.Name)
			}
		}
	}

	// Parse time range (default 1h)
	timeRange := 1 * time.Hour
	if tr := args["timeRange"]; tr != "" {
		if d, err := time.ParseDuration(tr); err == nil {
			timeRange = d
		}
	}

	// Concurrent search with semaphore (max 10 concurrent)
	type nsResult struct {
		namespace string
		lines     []k8s.LogLine
		podCount  int
		err       error
	}

	sem := make(chan struct{}, 10)
	results := make(chan nsResult, len(namespaces))

	searchCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, ns := range namespaces {
		wg.Add(1)
		go func(namespace string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			nsCtx, nsCancel := context.WithTimeout(searchCtx, 30*time.Second)
			defer nsCancel()

			// Get all pods in namespace and grep for trace ID
			pods, err := t.k8sClient.Resources().ListPods(namespace)
			if err != nil {
				results <- nsResult{namespace: namespace, err: err}
				return
			}

			var matched []k8s.LogLine
			sinceTime := time.Now().Add(-timeRange)
			for _, pod := range pods {
				logs, err := t.k8sClient.Logs().GetLogs(nsCtx, k8s.LogRequest{
					Namespace: namespace,
					PodName:   pod.Name,
					SinceTime: &sinceTime,
				})
				if err != nil {
					continue
				}
				for _, l := range logs {
					if strings.Contains(l.Content, traceID) {
						matched = append(matched, l)
					}
				}
			}
			results <- nsResult{namespace: namespace, lines: matched, podCount: len(pods)}
		}(ns)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all results
	var allLines []k8s.LogLine
	searchedNS := 0
	searchedPods := 0
	services := map[string]bool{} // unique services (namespaces with matches)
	var errors []string

	for r := range results {
		searchedNS++
		searchedPods += r.podCount
		if r.err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", r.namespace, r.err))
			continue
		}
		if len(r.lines) > 0 {
			services[r.namespace] = true
		}
		allLines = append(allLines, r.lines...)
	}

	// Sort by content (which should have timestamps)
	sort.Slice(allLines, func(i, j int) bool {
		return allLines[i].Content < allLines[j].Content
	})

	// Cap at 5000 lines
	if len(allLines) > 5000 {
		allLines = allLines[:5000]
	}

	// Format output
	var b strings.Builder
	fmt.Fprintf(&b, "Trace ID: %s\n", traceID)
	fmt.Fprintf(&b, "Searched: %d namespaces, %d pods\n", searchedNS, searchedPods)
	fmt.Fprintf(&b, "Found: %d services, %d log lines\n", len(services), len(allLines))

	if len(errors) > 0 {
		fmt.Fprintf(&b, "Errors: %s\n", strings.Join(errors, "; "))
	}

	if len(allLines) > 0 {
		b.WriteString("\nTimeline:\n")
		for _, l := range allLines {
			fmt.Fprintf(&b, "  [%s/%s] %s\n", l.PodName, l.Container, l.Content)
		}
	} else {
		b.WriteString("\nNo matching log lines found for this trace ID.\n")
	}

	return b.String(), allLines, nil
}
```

Note: Add `"sort"`, `"sync"`, and `"regexp"` to the imports in `tools.go`. Also add `severityName` (defined in severity_stats step) which is shared across analysis tools.

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/agent/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agent/tools.go
git commit -m "feat(agent): implement trace_logs tool with concurrent cross-namespace search"
```

---

### Task 6: System Prompt — Analysis Mode

**Files:**
- Modify: `internal/agent/prompt.go`

- [ ] **Step 1: Add analysis mode instructions**

In `internal/agent/prompt.go`, add a new section after the existing output guidelines (after line ~119):

```go
	// Phase 2: Structured analysis mode
	b.WriteString(`
## Analysis Mode
When performing log analysis (anomaly detection, error attribution, pattern recognition),
follow this structured approach:

1. **Data Collection**: Use severity_stats first for quantitative baseline. Check buffer state.
2. **Hypothesis Formation**: Based on data, form specific hypotheses about the issue.
3. **Verification**: Use targeted log searches (search_visible_logs, kubectl_logs with grepPattern) to verify.
4. **Cross-Reference**: Check events (kubectl_get_events) and describe output for corroboration.

**Output Format for Analysis:**
Structure your analysis response with these sections:
## 发现 (Findings)
Specific observations backed by data (line counts, error rates, time patterns).

## 可能原因 (Possible Causes)
Ranked list of hypotheses with evidence for each.

## 建议操作 (Recommended Actions)
Concrete next steps the user can take.

## 置信度 (Confidence)
How confident you are in this analysis (high/medium/low) and what additional data would help.

## Log Comparison
When asked to compare time periods, always use compare_logs tool first for quantitative delta,
then drill into specific error patterns that are new or significantly increased.

## Trace Correlation
When a user provides a trace ID, request ID, or correlation ID:
1. Call trace_logs with the ID
2. Present findings as a chronological timeline
3. Identify the point of failure and upstream/downstream impact
4. Suggest which service to investigate further
`)
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/agent/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agent/prompt.go
git commit -m "feat(agent): add structured analysis mode to system prompt"
```

---

### Task 7: Markdown Renderer

**Files:**
- Create: `internal/ui/markdown.go`
- Create: `internal/ui/markdown_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/markdown_test.go`:

```go
package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkdownRenderer_BasicRender(t *testing.T) {
	renderer, err := NewMarkdownRenderer("dark", 80)
	require.NoError(t, err)

	input := "# Hello\n\nThis is **bold** and `inline code`."
	output, err := renderer.Render(input)
	require.NoError(t, err)

	// glamour output should contain styled text (exact output varies by terminal)
	assert.NotEqual(t, input, output) // should be different from raw input
	assert.NotEmpty(t, output)
}

func TestMarkdownRenderer_EmptyInput(t *testing.T) {
	renderer, err := NewMarkdownRenderer("dark", 80)
	require.NoError(t, err)

	output, err := renderer.Render("")
	require.NoError(t, err)
	assert.Empty(t, output)
}

func TestMarkdownRenderer_FallbackOnError(t *testing.T) {
	renderer, err := NewMarkdownRenderer("dark", 80)
	require.NoError(t, err)

	// Well-formed markdown should always render
	input := "Just plain text"
	output, err := renderer.Render(input)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
}

func TestMarkdownRenderer_Cache(t *testing.T) {
	renderer, err := NewMarkdownRenderer("dark", 80)
	require.NoError(t, err)

	input := "# Test\n\nContent here."
	out1, _ := renderer.Render(input)
	out2, _ := renderer.Render(input)

	assert.Equal(t, out1, out2)
}

func TestMarkdownRenderer_AllThemes(t *testing.T) {
	for _, theme := range []string{"dark", "light", "dracula", "nord"} {
		t.Run(theme, func(t *testing.T) {
			renderer, err := NewMarkdownRenderer(theme, 80)
			require.NoError(t, err)

			output, err := renderer.Render("# Title\n\n- item 1\n- item 2")
			require.NoError(t, err)
			assert.NotEmpty(t, output)
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/... -v -run "TestMarkdownRenderer"`
Expected: FAIL — `NewMarkdownRenderer` undefined

- [ ] **Step 3: Add glamour dependency**

Run: `go get github.com/charmbracelet/glamour`

- [ ] **Step 4: Implement markdown renderer**

Create `internal/ui/markdown.go`:

```go
package ui

import (
	"sync"

	"github.com/charmbracelet/glamour"
)

// MarkdownRenderer wraps glamour for theme-aware Markdown rendering.
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
	cache    map[string]string // input hash → rendered output
	mu       sync.RWMutex
	width    int
}

// NewMarkdownRenderer creates a renderer for the given theme and terminal width.
func NewMarkdownRenderer(theme string, width int) (*MarkdownRenderer, error) {
	style := glamourStyleForTheme(theme)
	renderer, err := glamour.NewTermRenderer(
		style,
		glamour.WithWordWrap(width-4), // leave margin for panel border
	)
	if err != nil {
		return nil, err
	}

	return &MarkdownRenderer{
		renderer: renderer,
		cache:    make(map[string]string),
		width:    width,
	}, nil
}

// Render converts markdown text to terminal-styled output.
// Results are cached by input text.
func (m *MarkdownRenderer) Render(input string) (string, error) {
	if input == "" {
		return "", nil
	}

	// Check cache
	m.mu.RLock()
	if cached, ok := m.cache[input]; ok {
		m.mu.RUnlock()
		return cached, nil
	}
	m.mu.RUnlock()

	// Render
	output, err := m.renderer.Render(input)
	if err != nil {
		// Fallback: return raw text on render failure
		return input, nil
	}

	// Cache result
	m.mu.Lock()
	m.cache[input] = output
	m.mu.Unlock()

	return output, nil
}

// InvalidateCache clears the render cache (call on window resize).
func (m *MarkdownRenderer) InvalidateCache() {
	m.mu.Lock()
	m.cache = make(map[string]string)
	m.mu.Unlock()
}

// glamourStyleForTheme maps Korthex themes to glamour styles.
func glamourStyleForTheme(theme string) glamour.TermRendererOption {
	switch theme {
	case "light":
		return glamour.WithStylePath("light")
	case "dark", "dracula", "nord":
		return glamour.WithAutoStyle()
	default:
		return glamour.WithAutoStyle()
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/... -v -run "TestMarkdownRenderer"`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/markdown.go internal/ui/markdown_test.go go.mod go.sum
git commit -m "feat(ui): add glamour-based Markdown renderer with theme mapping and caching"
```

---

### Task 8: Chat Model — Complete-Then-Render Integration

**Files:**
- Modify: `internal/ui/messages.go`
- Modify: `internal/ui/chat.go:17-51,238-285,443-480`

- [ ] **Step 1: Add message types**

In `internal/ui/messages.go`, add:

```go
// MarkdownRenderedMsg signals that a markdown render has completed.
type MarkdownRenderedMsg struct {
	Index    int    // message index in the messages slice
	Rendered string // glamour-rendered output
}
```

- [ ] **Step 2: Add renderer field to ChatModel**

In `internal/ui/chat.go`, add to `ChatModel` struct (after `collapsedTurns`):

```go
	// Phase 2: Markdown rendering
	mdRenderer *MarkdownRenderer
	rendered   map[int]string // message index → rendered markdown (cache)
```

- [ ] **Step 3: Initialize renderer in NewChatModel**

In `NewChatModel`, add after spinner creation:

```go
	mdRenderer, _ := NewMarkdownRenderer(theme.Name, 80) // width updated on WindowSizeMsg
```

And set the field:

```go
	return ChatModel{
		// ... existing fields ...
		mdRenderer: mdRenderer,
		rendered:   make(map[int]string),
	}
```

- [ ] **Step 4: Trigger render on EventComplete**

In `handleAgentEvent`, modify the `EventComplete` handler (around line 271):

```go
	case agent.EventComplete:
		m.isRunning = false
		removeThinkingStatus(&m.messages)
		m.scrollToBottom()
		m.autoScroll = true
		m.input.Focus()

		// Phase 2: Trigger Markdown rendering for the last assistant message
		if m.mdRenderer != nil {
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Role == "assistant" {
					idx := i
					content := m.messages[i].Content
					return m, tea.Batch(
						textinput.Blink,
						func() tea.Msg {
							rendered, err := m.mdRenderer.Render(content)
							if err != nil {
								return nil // silently fail, keep raw text
							}
							return MarkdownRenderedMsg{Index: idx, Rendered: rendered}
						},
					)
				}
			}
		}
		return m, textinput.Blink
```

- [ ] **Step 5: Handle MarkdownRenderedMsg**

In `ChatModel.Update`, add a case for `MarkdownRenderedMsg`:

```go
	case MarkdownRenderedMsg:
		if msg.Index >= 0 && msg.Index < len(m.messages) && msg.Rendered != "" {
			m.rendered[msg.Index] = msg.Rendered
			m.scrollToBottom()
		}
		return m, nil
```

- [ ] **Step 6: Use rendered content in renderMessages**

In `renderMessages` (around line 457-463), modify the assistant message rendering:

```go
	case "assistant":
		prefix := m.theme.AccentStyle().Render("AI: ")
		content := msg.Content
		// Use rendered markdown if available
		if rendered, ok := m.rendered[i+startIdx]; ok {
			content = rendered
		}
		lines := strings.Split(content, "\n")
		for j, line := range lines {
			if j == 0 {
				b.WriteString(prefix + line + "\n")
			} else {
				b.WriteString("    " + line + "\n")
			}
		}
```

Where `startIdx` is the offset accounting for collapsed turns (the existing code already handles this with the collapsed turns indicator).

- [ ] **Step 7: Update renderer width on WindowSizeMsg**

In the `WindowSizeMsg` handler in `ChatModel.Update`:

```go
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Recreate renderer with new width
		if m.mdRenderer != nil {
			m.mdRenderer.InvalidateCache()
			m.rendered = make(map[int]string) // clear index-based cache
			m.mdRenderer, _ = NewMarkdownRenderer(m.theme.Name, msg.Width)
		}
```

- [ ] **Step 8: Verify compilation**

Run: `go build ./internal/ui/...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/ui/chat.go internal/ui/messages.go
git commit -m "feat(ui): add complete-then-render Markdown rendering for AI responses"
```

---

### Task 9: History Search Overlay

**Files:**
- Create: `internal/ui/historypanel.go`

- [ ] **Step 1: Implement history search overlay**

Create `internal/ui/historypanel.go`:

```go
package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Orwell-Yu/korthex/internal/history"
	tea "github.com/charmbracelet/bubbletea"
)

// HistoryOverlayModel manages the /history search overlay in the Chat panel.
type HistoryOverlayModel struct {
	visible bool
	query   string
	results []history.SearchResult
	cursor  int
	store   *history.Store
	theme   Theme
	width   int
	height  int
}

// HistorySearchResultsMsg carries search results back to the overlay.
type HistorySearchResultsMsg struct {
	Results []history.SearchResult
}

// HistoryContextLoadedMsg carries the selected session context for injection.
type HistoryContextLoadedMsg struct {
	SessionID string
	Summary   string
	Messages  []history.SessionMessage
}

func NewHistoryOverlay(store *history.Store, theme Theme) HistoryOverlayModel {
	return HistoryOverlayModel{
		store: store,
		theme: theme,
	}
}

func (m *HistoryOverlayModel) Show() {
	m.visible = true
	m.query = ""
	m.results = nil
	m.cursor = 0
}

func (m *HistoryOverlayModel) Hide() {
	m.visible = false
}

func (m HistoryOverlayModel) Visible() bool {
	return m.visible
}

func (m HistoryOverlayModel) Update(msg tea.Msg) (HistoryOverlayModel, tea.Cmd) {
	switch msg := msg.(type) {
	case HistorySearchResultsMsg:
		m.results = msg.Results
		m.cursor = 0
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.Hide()
			return m, nil
		case "enter":
			if len(m.results) > 0 && m.cursor < len(m.results) {
				selected := m.results[m.cursor]
				m.Hide()
				return m, func() tea.Msg {
					msgs, _ := m.store.GetSessionMessages(selected.SessionID)
					return HistoryContextLoadedMsg{
						SessionID: selected.SessionID,
						Summary:   selected.Summary,
						Messages:  msgs,
					}
				}
			}
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}
		case "backspace":
			if len(m.query) > 0 {
				_, size := utf8.DecodeLastRuneInString(m.query)
				m.query = m.query[:len(m.query)-size]
				return m, m.search()
			}
		default:
			if len(msg.Runes) > 0 {
				m.query += string(msg.Runes)
				return m, m.search()
			}
		}
	}
	return m, nil
}

func (m HistoryOverlayModel) search() tea.Cmd {
	query := m.query
	store := m.store
	return func() tea.Msg {
		if store == nil || query == "" {
			return HistorySearchResultsMsg{}
		}
		results, err := store.Search(query)
		if err != nil {
			return HistorySearchResultsMsg{}
		}
		return HistorySearchResultsMsg{Results: results}
	}
}

func (m HistoryOverlayModel) View() string {
	if !m.visible {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.theme.Title.Render("Conversation History"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("  Search: %s_\n\n", m.query))

	if len(m.results) == 0 && m.query != "" {
		b.WriteString("  (no results)\n")
	}

	maxVisible := min(len(m.results), 10)
	for i := 0; i < maxVisible; i++ {
		r := m.results[i]
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}
		timeStr := r.StartedAt.Format("2006-01-02 15:04")
		b.WriteString(fmt.Sprintf("%s● %s  [%s]\n", prefix, timeStr, r.Cluster))
		if r.Summary != "" {
			b.WriteString(fmt.Sprintf("    %s\n", truncateStr(r.Summary, m.width-8)))
		}
		if r.Snippet != "" {
			b.WriteString(fmt.Sprintf("    → %s\n", truncateStr(r.Snippet, m.width-8)))
		}
		b.WriteString("\n")
	}

	b.WriteString("  [Enter] load context  [Esc] close\n")
	return b.String()
}

func truncateStr(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/ui/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/ui/historypanel.go
git commit -m "feat(ui): add /history search overlay with FTS query and context injection"
```

---

### Task 10: Wire History into Chat and App

**Files:**
- Modify: `internal/ui/chat.go`
- Modify: `internal/ui/app.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: Add history overlay to ChatModel**

In `internal/ui/chat.go`, add to `ChatModel` struct:

```go
	// Phase 2: History search overlay
	historyOverlay HistoryOverlayModel
```

- [ ] **Step 2: Handle /history command in chat**

In `handleKey`, detect `/history` input and show the overlay:

```go
	case "enter":
		query := strings.TrimSpace(m.input.Value())
		if query == "" {
			return m, nil
		}

		// Phase 2: handle /history command
		if query == "/history" {
			m.input.SetValue("")
			m.historyOverlay.Show()
			return m, nil
		}

		// ... existing agent execution code ...
```

- [ ] **Step 3: Route key events to history overlay when visible**

At the top of `ChatModel.Update`, before other key handling:

```go
	case tea.KeyMsg:
		if m.historyOverlay.Visible() {
			var cmd tea.Cmd
			m.historyOverlay, cmd = m.historyOverlay.Update(msg)
			return m, cmd
		}
```

- [ ] **Step 4: Handle HistoryContextLoadedMsg**

In `ChatModel.Update`:

```go
	case HistoryContextLoadedMsg:
		// Inject context from previous session into current conversation
		contextMsg := fmt.Sprintf("[Previous context from %s]\n%s",
			msg.SessionID[:8], msg.Summary)
		m.addMessage(ChatMessage{Role: "status", Content: contextMsg})
		m.scrollToBottom()
		return m, nil
```

- [ ] **Step 5: Render history overlay in Chat View**

In `ChatModel.View()`, add before the message area:

```go
	// History overlay takes precedence
	if m.historyOverlay.Visible() {
		return renderPanel(m.historyOverlay.View(), m.width, m.height)
	}
```

- [ ] **Step 6: Wire history store into App**

In `internal/app/app.go`, add history store:

```go
type App struct {
	config       *config.Config
	k8sClient    k8s.Client
	llmProvider  llm.Provider
	agent        agent.Agent
	logFile      *os.File
	historyStore *history.Store // Phase 2
}
```

In `New()`, after agent creation:

```go
	// Phase 2: History store
	var historyStore *history.Store
	if cfg.History.Enabled {
		hs, err := history.Open(cfg.History.DBPath)
		if err != nil {
			slog.Warn("failed to open history store", "error", err)
		} else {
			historyStore = hs
			// Cleanup old sessions
			deleted, _ := hs.Cleanup(cfg.History.RetentionDays)
			if deleted > 0 {
				slog.Info("history cleanup", "deleted", deleted)
			}
		}
	}
```

In `Run()`, pass history store to UI:

```go
	appModel := ui.NewAppModel(a.agent, a.k8sClient, a.config)
	if a.historyStore != nil {
		appModel.SetHistoryStore(a.historyStore)
	}
```

In `Shutdown()`:

```go
	// End the current history session
	if a.historyStore != nil {
		// Generate session summary from the last few messages
		if sessionID := a.getActiveSessionID(); sessionID != "" {
			msgs, err := a.historyStore.GetSessionMessages(sessionID)
			if err == nil && len(msgs) > 0 {
				// Create a brief summary from user queries
				var queries []string
				for _, m := range msgs {
					if m.Role == "user" {
						queries = append(queries, truncateTo(m.Content, 60))
						if len(queries) >= 3 {
							break
						}
					}
				}
				summary := strings.Join(queries, "; ")
				a.historyStore.EndSession(sessionID, summary)
			}
		}
		a.historyStore.Close()
	}
```

Note: `getActiveSessionID()` retrieves the session ID from the chat model. `truncateTo()` is a simple string truncation helper. The summary is derived from user queries rather than calling the LLM (avoids blocking on exit). If LLM-generated summaries are desired later, this can be enhanced to use a fast model call with a short timeout.

- [ ] **Step 7: Add SetHistoryStore to AppModel**

In `internal/ui/app.go`:

```go
func (m *AppModel) SetHistoryStore(store *history.Store) {
	m.chat.historyOverlay = NewHistoryOverlay(store, m.chat.theme)
	m.chat.historyStore = store
	if m.k8sClient != nil && m.k8sClient.IsConnected() {
		m.chat.clusterName = m.k8sClient.CurrentContext()
	}
}
```

- [ ] **Step 8: Persist messages to history during agent execution**

In `ChatModel.handleAgentEvent`, after processing each event, persist to history if store available. This is done by adding a `historySessionID` field to ChatModel and writing messages in the event handlers.

Add to `ChatModel`:

```go
	historyStore     *history.Store
	historySessionID string
	clusterName      string // set via SetHistoryStore; avoids type-asserting agent internals
```

When agent starts (in `handleKey` enter), create session:

```go
	// Create history session on first query
	if m.historyStore != nil && m.historySessionID == "" {
		sid, _ := m.historyStore.CreateSession(m.clusterName)
		m.historySessionID = sid
	}
	// Persist user message
	if m.historyStore != nil && m.historySessionID != "" {
		m.historyStore.AddMessage(m.historySessionID, "user", query, "")
	}
```

And in `EventSummary`/`EventComplete`, persist the assistant response.

- [ ] **Step 9: Verify compilation**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/ui/chat.go internal/ui/app.go internal/app/app.go
git commit -m "feat: wire history store into chat with /history command and message persistence"
```

---

### Task 11: Final Integration Test

**Files:**
- All modified files

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -race -count=1`
Expected: All PASS

- [ ] **Step 2: Run lint**

Run: `make lint` or `golangci-lint run ./...`
Expected: No new issues

- [ ] **Step 3: Build**

Run: `make build`
Expected: Binary builds successfully

- [ ] **Step 4: Verify new tool count**

The agent should now have 19 total tools:
- 10 Phase 1
- 4 Enhanced Browser (kubectl_get_statefulsets/daemonsets/jobs/cronjobs)
- 4 Deep Analysis (severity_stats, compare_logs, get_pod_metrics, trace_logs)
- 1 Data Safety (bookmark_log_lines)

Run: `grep "Name:" internal/agent/tools.go | wc -l` (should be ~19)

- [ ] **Step 5: Verify safety whitelist count**

Run: `grep "SafetyAllowed" internal/agent/safety.go | wc -l` (should be 19)

- [ ] **Step 6: Verify history module builds independently**

Run: `go test ./internal/history/... -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit any remaining fixes**

```bash
git add -A
git commit -m "chore: fix lint issues from Deep Analysis implementation"
```
