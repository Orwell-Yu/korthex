# internal/db — CLAUDE.md

> Database Intelligence: discover DB Pods, auto-fetch credentials, introspect schema, execute read-only SQL via kubectl exec.

## Parent Reference

- **When confused about architecture:** Read [`../../CLAUDE.md`](../../CLAUDE.md) (root project rules, especially Rule #5: Phase 3 只读)
- **When confused about interface design:** Read [`../../SPEC.md`](../../SPEC.md) Section 10.1 (db interfaces)
- **When confused about adapter pattern:** Read [`../../SPEC.md`](../../SPEC.md) Section 10.3 (Adapter interface)
- **When confused about DB features:** Read [Phase 3 PRD](../../docs/superpowers/specs/2026-04-29-phase3-prd-design.md) Section 4 (DB Intelligence)
- **When confused about SQL safety:** Read [Phase 3 PRD](../../docs/superpowers/specs/2026-04-29-phase3-prd-design.md) Section 4.7 (SQL Safety)
- **When confused about discovery strategy:** Read [Phase 3 PRD](../../docs/superpowers/specs/2026-04-29-phase3-prd-design.md) Section 4.5 (Discovery)
- **When confused about K8s exec:** Read [`../k8s/CLAUDE.md`](../k8s/CLAUDE.md) (PodExecutor interface)

## Module Rules

| # | Rule | Reason |
|---|------|--------|
| 1 | **Layer 1: 只能 import `internal/config` 和 `internal/k8s`** | 与 llm, history 同层，禁止互相 import，禁止 import agent/ui |
| 2 | **kubectl exec only** — 所有数据库交互通过 `k8s.PodExecutor.ExecInPod()` 执行，禁止引入 database/sql 驱动或端口转发 | PRD P3-D1: 零端口暴露，只需 exec RBAC |
| 3 | **Adapter 独立** — `mysql_adapter.go` 和 `postgres_adapter.go` 之间禁止互相引用 | 类似 llm adapter 隔离模式，避免方言耦合 |
| 4 | **SQL 安全双层检查必须通过** — `Query()` 执行前必须调用 `safety.go` 的 `CheckSQL()`，先过关键词白名单，再过 AST 解析器。两层都通过才执行 | PRD P3-D6: pgweb 的 --readonly 双层模式 |
| 5 | **仅 SELECT** — 白名单: `SELECT`, `SHOW`, `DESCRIBE`, `EXPLAIN`, `WITH` (CTE)。黑名单: `INSERT`, `UPDATE`, `DELETE`, `DROP`, `ALTER`, `TRUNCATE`, `CREATE`, `GRANT`, `REVOKE`。禁止未被引号包裹的分号 (多语句) | PRD P3-D5: Phase 3 严格只读 |
| 6 | **凭据不持久化** — 成功获取的凭据仅缓存在内存中 (sync.Map)，会话结束清除。Chat 交互获取的凭据同样不持久化 | 安全性: 不在磁盘上留存数据库密码 |
| 7 | **Schema 缓存内存** — INFORMATION_SCHEMA 查询结果缓存在 `schemaCache sync.Map` 中，key 为 `namespace/pod/database`。会话结束清除，用户可通过 "刷新 schema" 手动清除 | 避免重复 exec 查询 INFORMATION_SCHEMA |
| 8 | **INFORMATION_SCHEMA 标准化** — schema 自省仅通过 INFORMATION_SCHEMA 查询，不使用 `SHOW` 命令。MySQL 和 PostgreSQL 共用同一数据源 | 跨数据库一致性，SQL 标准 |
| 9 | **输出解析容错** — `ParseOutput()` 必须处理: 空结果、列头无数据行、NULL 值、CLI 警告消息、非 UTF-8 字节。解析失败返回清晰错误而非 panic | exec 输出不可控，必须防御性解析 |
| 10 | **单表行数上限** — `Query()` 在 SQL 末尾自动追加 `LIMIT N`（N = min(用户传入 limit, config max_rows_per_table)）。如果 SQL 已有 LIMIT 子句，取两者较小值 | PRD §7: 防止大量数据拉取 |

## Interfaces (defined in this module)

```go
type DatabaseService interface {
    Discover(ctx context.Context, namespace string) ([]DatabaseInfo, error)
    GetCredentials(ctx context.Context, namespace, podName string) (*Credentials, error)
    GetSchema(ctx context.Context, namespace, podName, database string) ([]TableSchema, []ForeignKey, error)
    Query(ctx context.Context, namespace, podName, database, sql string, limit int) (*QueryResult, error)
    GetForeignKeys(ctx context.Context, namespace, podName, database, table string) ([]ForeignKey, error)
}

type Adapter interface {
    BuildCommand(sql string, creds Credentials) []string
    ParseOutput(raw []byte) (*QueryResult, error)
    ListTablesSQL(database string) string
    DescribeTableSQL(database, table string) string
    ForeignKeysSQL(database string) string
    DetectCLI() []string
    Type() DatabaseType
}

func AdapterFor(dbType DatabaseType) (Adapter, bool)
```

> Full type definitions (DatabaseInfo, QueryResult, etc.) → [`README.md`](./README.md) | SPEC Section 10 → [`../../SPEC.md`](../../SPEC.md)

## Files

| File | Do | Don't |
|------|-----|-------|
| `types.go` | DatabaseInfo, DatabaseType, TableSchema, Column, ForeignKey, QueryResult, Credentials 结构体 | 不要 import 任何数据库驱动类型 |
| `discovery.go` | 扫描 namespace Pods: 镜像名匹配 → 端口匹配 → label 匹配 → StatefulSet 关联。复用 k8s.ResourceLister | 不要直接调 client-go API，通过 k8s.Client |
| `credentials.go` | Pod env → Secret volume → Operator 命名约定 → 返回 Credentials。缓存在 sync.Map | 不要持久化密码到磁盘 |
| `executor.go` | 通过 k8s.PodExecutor.ExecInPod() 执行 SQL，调用 Adapter.BuildCommand() 构造命令，Adapter.ParseOutput() 解析结果 | 不要直接引入 remotecommand，由 k8s 模块封装 |
| `schema.go` | INFORMATION_SCHEMA 查询：调用 Adapter.ListTablesSQL() / DescribeTableSQL() / ForeignKeysSQL() 生成 SQL → executor 执行 → 解析为 TableSchema / ForeignKey。结果缓存 | 不要用 SHOW 命令代替 INFORMATION_SCHEMA |
| `safety.go` | Layer 1 关键词白名单 (正则/字符串) + Layer 2 AST 校验 (MySQL: sqlparser, PG: go-pgquery)。`CheckSQL(dbType, sql) error` | 不要跳过任何一层检查 |
| `mysql_adapter.go` | MySQL CLI 命令构造 (`mysql -u ... -p... -e "..."`)、输出解析 (tab-separated)、Schema SQL | 不要引用 postgres_adapter.go |
| `postgres_adapter.go` | PostgreSQL CLI 命令构造 (`psql -U ... -c "..."`)、输出解析 (pipe-separated)、Schema SQL | 不要引用 mysql_adapter.go |
| `adapter.go` | Adapter 接口定义 + 注册 map + `AdapterFor()` 查找函数 | 不要放具体适配器实现 |
| `db_test.go` | 测试: safety 白名单/黑名单、adapter 输出解析、discovery 匹配规则 | 不要依赖真实 K8s 集群 |

## Cross-Module Dependencies

| This module | → | Dependency | Via |
|-------------|---|-----------|-----|
| db | imports | config | `config.DatabaseConfig` |
| db | imports | k8s | `k8s.PodExecutor` (exec), `k8s.ResourceLister` (discovery) |
| agent | imports | db | `db.DatabaseService` interface (5 个 DB tools) |
| ui | imports | db | `db.QueryResult`, `db.DatabaseType` (类型定义, Data Viewer 渲染) |

## Key Design Patterns

### kubectl exec Pipeline

```
Agent tool call → db.Query(sql)
  → safety.CheckSQL(sql)           // 双层安全检查
  → adapter.BuildCommand(sql, creds) // 构造 CLI 命令
  → k8s.ExecInPod(ns, pod, cmd)    // SPDY exec
  → adapter.ParseOutput(stdout)    // 解析结构化结果
  → QueryResult
```

### Discovery Scan (priority order)

```
Scan Pods in namespace:
  1. 镜像名匹配: mysql*, mariadb*, postgres*, postgis*
  2. 容器端口: 3306 (MySQL), 5432 (PostgreSQL)
  3. Label 匹配: app.kubernetes.io/name=mysql|postgresql
  4. StatefulSet 关联: 数据库通常是 StatefulSet
  → 合并去重 → []DatabaseInfo
```

### SQL Safety Dual-Layer

```
SQL input
  → Layer 1: 关键词白名单 (<1ms)
     首词: SELECT|SHOW|DESCRIBE|EXPLAIN|WITH
     黑名单: INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|CREATE|GRANT|REVOKE
     多语句: 禁止未被引号包裹的分号
  → Layer 2: AST 解析器 (<10ms)
     MySQL: xwb1989/sqlparser → *sqlparser.Select check
     PostgreSQL: wasilibs/go-pgquery → statement type check
  → 两层都通过才执行
```

## Testing Checklist

- [ ] Safety: SELECT 语句通过双层检查
- [ ] Safety: INSERT/UPDATE/DELETE/DROP 被关键词白名单拒绝
- [ ] Safety: 注释内嵌 DROP (如 `SELECT /* DROP TABLE */ 1`) 被 AST 解析器正确处理
- [ ] Safety: 多语句 (`;`) 被拒绝
- [ ] Safety: WITH (CTE) 只读子句通过
- [ ] MySQL adapter: BuildCommand 生成正确的 mysql CLI 命令
- [ ] MySQL adapter: ParseOutput 解析 tab-separated 输出
- [ ] MySQL adapter: ParseOutput 处理空结果集
- [ ] MySQL adapter: ParseOutput 处理 NULL 值
- [ ] PG adapter: BuildCommand 生成正确的 psql CLI 命令
- [ ] PG adapter: ParseOutput 解析 pipe-separated 输出
- [ ] Discovery: 镜像名 `mysql:8.0` 被正确识别为 MySQL
- [ ] Discovery: 端口 5432 被正确识别为 PostgreSQL
- [ ] Discovery: 无匹配 Pod 时返回空列表
- [ ] Credentials: 从 Pod env var 提取 MYSQL_ROOT_PASSWORD
- [ ] Credentials: 缓存命中（同一 pod 不重复获取）
- [ ] Schema: INFORMATION_SCHEMA 查询解析为 TableSchema + ForeignKey
- [ ] Schema: 缓存命中（同一 database 不重复查询）
- [ ] Query: LIMIT 自动追加
- [ ] Query: 已有 LIMIT 时取较小值
