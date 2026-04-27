# internal/history — CLAUDE.md

> Conversation history persistence: SQLite storage, FTS5 full-text search, session lifecycle, automatic cleanup.

## Parent Reference

- **When confused about architecture:** Read [`../../CLAUDE.md`](../../CLAUDE.md) (root project rules)
- **When confused about interface design:** Read [`../../SPEC.md`](../../SPEC.md) Section 9.3 (Phase 2 interfaces)
- **When confused about history requirements:** Read [Phase 2 PRD](../../docs/superpowers/specs/2026-04-24-phase2-prd-design.md) Section 3.2 (对话历史持久化)
- **When confused about config fields:** Read [`../config/CLAUDE.md`](../config/CLAUDE.md) (HistoryConfig)

## Module Rules

| # | Rule | Reason |
|---|------|--------|
| 1 | **Layer 1: 只能 import `internal/config`** | 与 k8s、llm 同层，禁止互相 import，禁止 import agent/ui |
| 2 | **纯 Go SQLite** — 必须使用 `modernc.org/sqlite`，禁止 CGO-based `mattn/go-sqlite3` | 保持单二进制零 CGO 依赖原则 |
| 3 | **所有 DB 操作接受 `context.Context`** — 支持取消和超时 | Korthex 退出时需要 graceful cancel |
| 4 | **FTS5 搜索 < 200ms** — 10000 条消息量级下全文搜索必须在 200ms 内返回 | 用户体验要求: 搜索即时响应 |
| 5 | **Retention 清理在启动时同步执行** — 不用后台 goroutine，`CleanExpired()` 在 `app.New()` 中调用 | 简单可靠，避免并发清理与写入竞争 |
| 6 | **Tool result 压缩存储** — 超过 2000 字符的 tool result 截取前 500 字符 + 统计摘要后存储，标记 `[compressed]` | 控制数据库体积，30 天历史 < 50MB |
| 7 | **DB 路径从 config 读取** — 默认 `~/.korthex/history.db`，首次使用时自动创建父目录 | 配置灵活性 + 零配置开箱即用 |

## Interfaces (defined in this module)

```go
type Store interface {
    CreateSession(ctx context.Context, cluster string) (sessionID string, err error)
    EndSession(ctx context.Context, sessionID string, summary string) error
    AddMessage(ctx context.Context, sessionID string, msg Message) error
    Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)
    CleanExpired(ctx context.Context, retentionDays int) error
}
```

> Full type definitions → [`README.md`](./README.md) | SPEC Section 9.3 → [`../../SPEC.md`](../../SPEC.md)

## Files

| File | Do | Don't |
|------|-----|-------|
| `store.go` | Store interface, SQLite 实现, Message/Session 类型, Open/Close | 不要 import agent 或 ui 类型 |
| `schema.go` | DDL 语句 (sessions, messages, messages_fts), 索引, 版本迁移 | 不要使用 ORM |
| `search.go` | FTS5 query 构建, `ns:` 前缀解析, `after:` 时间过滤 | 不要用字符串拼接构建 SQL，用参数化查询 |
| `store_test.go` | in-memory SQLite 测试 (`file::memory:?cache=shared`) | 不要依赖持久化 DB 文件 |

## Cross-Module Dependencies

| This module | → | Dependency | Via |
|-------------|---|-----------|-----|
| history | imports | config | `config.HistoryConfig` |
| agent | imports | history | `history.Store` interface (消息持久化) |
| ui | imports | history | `history.Store` interface (搜索覆盖层) |

## Schema Overview

```sql
-- 3 tables: sessions, messages, messages_fts (FTS5 virtual table)
-- 3 indexes: idx_messages_session, idx_messages_created, idx_sessions_cluster
-- See Phase 2 PRD Section 3.2 for full DDL
```

## Testing Checklist

- [ ] CreateSession + AddMessage + 读取验证
- [ ] FTS5 关键词搜索 (单词、短语)
- [ ] FTS5 `ns:` namespace 前缀过滤
- [ ] FTS5 `after:` 时间范围过滤
- [ ] CleanExpired 清理过期 session (retention_days 正确执行)
- [ ] 并发写入安全 (多 goroutine AddMessage)
- [ ] In-memory SQLite 测试隔离
- [ ] Tool result 压缩存储验证 (>2000 字符 → 截取 + 摘要)
- [ ] EndSession 更新 ended_at 和 summary
