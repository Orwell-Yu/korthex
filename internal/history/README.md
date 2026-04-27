# internal/history - Conversation History Persistence

> **Parent docs:** [CLAUDE.md (root)](../../CLAUDE.md) | [SPEC.md Section 9.3](../../SPEC.md) | [Phase 2 PRD Section 3.2](../../docs/superpowers/specs/2026-04-24-phase2-prd-design.md)
>
> **Module rules:** [CLAUDE.md (this module)](./CLAUDE.md)
>
> **Depends on:** [config](../config/README.md) | **Depended by:** [agent](../agent/README.md), [ui](../ui/README.md)

## Responsibility

Persist conversation history to SQLite. Provide FTS5-powered full-text search across sessions. Manage session lifecycle (create, message recording, summary, end) and automatic retention cleanup on startup.

## Public Interfaces

```go
type Store interface {
    CreateSession(ctx context.Context, cluster string) (sessionID string, err error)
    EndSession(ctx context.Context, sessionID string, summary string) error
    AddMessage(ctx context.Context, sessionID string, msg Message) error
    Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)
    CleanExpired(ctx context.Context, retentionDays int) error
}
```

## Key Types

```go
type Message struct {
    Role      string    // user | assistant | tool_call | tool_result
    Content   string    // message text or compressed tool result
    Namespace string    // associated namespace (for search filtering)
    CreatedAt time.Time
}

type SearchQuery struct {
    Keyword   string    // FTS5 full-text search term
    Namespace string    // optional: filter by namespace (ns: prefix)
    After     time.Time // optional: filter by time (after: prefix)
    Limit     int       // max results, default 20
}

type SearchResult struct {
    SessionID string
    Cluster   string
    StartedAt time.Time
    Summary   string     // AI-generated session summary
    Messages  []Message  // preview messages matching the query
}

type Session struct {
    ID        string
    Cluster   string
    StartedAt time.Time
    EndedAt   *time.Time
    Summary   string
}
```

## Files

| File | Responsibility |
|------|---------------|
| `store.go` | Store interface, SQLite implementation (`sqliteStore`), Open/Close, CreateSession, EndSession, AddMessage |
| `schema.go` | DDL statements for 3 tables (sessions, messages, messages_fts), 3 indexes, schema migration logic |
| `search.go` | FTS5 query builder: keyword extraction, `ns:` prefix parsing, `after:` time filter, result ranking |
| `store_test.go` | In-memory SQLite tests (`file::memory:?cache=shared`), concurrent write tests, search accuracy tests |

## Dependencies

- **External**: `modernc.org/sqlite` (BSD-3-Clause, pure Go SQLite)
- **Internal**: `config` (for HistoryConfig)

## Schema Design

```sql
CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,  -- UUID
    cluster     TEXT NOT NULL,     -- K8s context name
    started_at  DATETIME NOT NULL,
    ended_at    DATETIME,
    summary     TEXT               -- AI auto-generated session summary
);

CREATE TABLE messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    role        TEXT NOT NULL,     -- user | assistant | tool_call | tool_result
    content     TEXT NOT NULL,     -- message text or compressed tool result
    namespace   TEXT,              -- associated namespace (for search)
    created_at  DATETIME NOT NULL
);

CREATE VIRTUAL TABLE messages_fts USING fts5(content, namespace);

CREATE INDEX idx_messages_session ON messages(session_id);
CREATE INDEX idx_messages_created ON messages(created_at);
CREATE INDEX idx_sessions_cluster ON sessions(cluster);
```

## Session Lifecycle

1. **Startup**: `CreateSession(ctx, cluster)` → new session row. `CleanExpired(ctx, retentionDays)` → delete sessions + messages older than retention period.
2. **Runtime**: Each user input and AI response → `AddMessage()` in real-time. Tool results > 2000 chars stored as compressed version (first 500 chars + stats summary, marked `[compressed]`).
3. **Shutdown**: `EndSession(ctx, sessionID, summary)` → update `ended_at` + AI-generated 1-2 sentence summary.

## Search Capabilities

- **Keyword search**: Direct FTS5 match on message content
- **Namespace filter**: `ns:production timeout` → FTS5 keyword="timeout" AND namespace="production"
- **Time filter**: `after:2026-04-20 error` → FTS5 keyword="error" AND created_at > "2026-04-20"
- **Result format**: Session summary + matching message previews, ordered by recency

## Testing Strategy

- **In-memory SQLite**: All tests use `file::memory:?cache=shared` for isolation and speed
- **Session lifecycle tests**: Create → AddMessage → EndSession → verify all fields
- **FTS5 search tests**: Keyword match, namespace filter, time filter, combined filters
- **Concurrent write tests**: Multiple goroutines calling AddMessage simultaneously
- **Cleanup tests**: Verify retention_days correctly deletes old sessions and associated messages
- **Benchmark**: FTS5 search over 10k messages < 200ms

## Phase 3+ Extension Points

- Cross-session analytics (common error patterns across time)
- Session export/import (JSON format for sharing)
- Natural language search ("之前那个 timeout 的问题" → keyword extraction via LLM)
