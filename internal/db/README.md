# internal/db - Database Intelligence

> **Parent docs:** [CLAUDE.md (root)](../../CLAUDE.md) | [SPEC.md Section 10](../../SPEC.md) | [Phase 3 PRD Section 4](../../docs/superpowers/specs/2026-04-29-phase3-prd-design.md)
>
> **Module rules:** [CLAUDE.md (this module)](./CLAUDE.md)
>
> **Depends on:** [config](../config/README.md), [k8s](../k8s/README.md) | **Depended by:** [agent](../agent/README.md), [ui](../ui/README.md)

## Responsibility

Database Intelligence engine for K8s clusters. Discovers database Pods (MySQL/PostgreSQL) via Informer Cache, auto-fetches credentials (env/Secret/Operator conventions), introspects schema via INFORMATION_SCHEMA, and executes read-only SQL queries via kubectl exec. All database interaction goes through `k8s.PodExecutor.ExecInPod()` — no database drivers, no port-forward. Adapter pattern abstracts MySQL/PostgreSQL dialect differences.

## Public Interfaces

```go
// DatabaseService is the top-level DB interface, consumed by agent tools
type DatabaseService interface {
    Discover(ctx context.Context, namespace string) ([]DatabaseInfo, error)
    GetCredentials(ctx context.Context, namespace, podName string) (*Credentials, error)
    GetSchema(ctx context.Context, namespace, podName, database string) ([]TableSchema, []ForeignKey, error)
    Query(ctx context.Context, namespace, podName, database, sql string, limit int) (*QueryResult, error)
    GetForeignKeys(ctx context.Context, namespace, podName, database, table string) ([]ForeignKey, error)
}

// Adapter abstracts MySQL/PostgreSQL CLI differences
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

## Key Types

```go
type DatabaseType string
const (
    MySQL      DatabaseType = "mysql"
    PostgreSQL DatabaseType = "postgresql"
)

type DatabaseInfo struct {
    Namespace   string
    PodName     string
    ServiceName string       // associated Service (optional)
    DBType      DatabaseType
    Port        int
    Database    string
    Detected    time.Time
}

type TableSchema struct {
    Name    string
    Columns []Column
}

type Column struct {
    Name       string
    DataType   string
    IsNullable bool
    IsPrimary  bool
    Default    string
}

type ForeignKey struct {
    ChildTable    string
    ChildColumn   string
    ParentTable   string
    ParentColumn  string
}

type QueryResult struct {
    Columns   []string
    Rows      [][]string   // text form, parsed from exec output
    RowCount  int
    Truncated bool         // true if rows were capped by limit
}

type Credentials struct {
    Username string
    Password string
    Database string
    Host     string  // usually localhost (inside Pod)
    Port     int
}
```

## Files

| File | Responsibility |
|------|---------------|
| `types.go` | All DB-specific type definitions: DatabaseInfo, DatabaseType, TableSchema, Column, ForeignKey, QueryResult, Credentials |
| `adapter.go` | Adapter interface, adapter registry map, `AdapterFor()` lookup |
| `mysql_adapter.go` | MySQL adapter: `mysql` CLI command construction, tab-separated output parsing, INFORMATION_SCHEMA SQL for MySQL |
| `postgres_adapter.go` | PostgreSQL adapter: `psql` CLI command construction, pipe-separated output parsing, INFORMATION_SCHEMA SQL for PostgreSQL |
| `discovery.go` | DB Pod discovery: scan namespace Pods by image name, port, labels, StatefulSet association. Uses `k8s.ResourceLister` |
| `credentials.go` | Credential discovery: Pod env vars → Secret volumes → Operator naming conventions. In-memory cache via `sync.Map` |
| `executor.go` | SQL execution engine: safety check → adapter command build → `k8s.ExecInPod()` → adapter output parse. Orchestrates the exec pipeline |
| `schema.go` | Schema introspection: INFORMATION_SCHEMA queries via executor, result parsing into TableSchema/ForeignKey. Schema cache via `sync.Map` |
| `safety.go` | SQL safety dual-layer check: Layer 1 keyword whitelist/blacklist, Layer 2 AST parser validation (sqlparser for MySQL, go-pgquery for PG) |
| `db_test.go` | Unit tests: safety checks, adapter output parsing, discovery matching rules |

## Dependencies

- **External**: `xwb1989/sqlparser` (MySQL AST), `wasilibs/go-pgquery` (PostgreSQL AST)
- **Internal**: `config` (DatabaseConfig), `k8s` (PodExecutor for exec, ResourceLister for discovery)
- **NOT imported**: `database/sql`, `go-sql-driver/mysql`, `lib/pq`, `jackc/pgx` — all DB interaction via kubectl exec

## Key Design Patterns

### kubectl exec Pipeline

```
Agent calls db.Query(namespace, pod, database, sql, limit)
  → safety.CheckSQL(dbType, sql)           // dual-layer safety
  → adapter := AdapterFor(dbType)
  → cmd := adapter.BuildCommand(sql, creds) // e.g. ["mysql", "-u", "root", "-p***", "orderdb", "-e", sql]
  → stdout, stderr, err := k8s.ExecInPod(ctx, ns, pod, "", cmd)
  → result := adapter.ParseOutput(stdout)   // structured QueryResult
  → return result
```

### Discovery Strategy (priority order)

| Signal | Priority | Match Rules |
|--------|----------|------------|
| Image name | 1 | `mysql*`, `mariadb*`, `postgres*`, `postgis*` + custom patterns from config |
| Container port | 2 | 3306 (MySQL), 5432 (PostgreSQL) |
| Labels | 3 | `app.kubernetes.io/name=mysql\|postgresql`, `app=mysql\|postgres` |
| StatefulSet | 4 | DB Pods are typically StatefulSet-managed |

### SQL Safety Dual-Layer (ref: pgweb --readonly)

| Layer | Check | Speed | Parser |
|-------|-------|-------|--------|
| 1 | Keyword whitelist + blacklist + multi-statement detection | <1ms | string/regex |
| 2 | AST parse → verify root node is SELECT | <10ms | sqlparser (MySQL), go-pgquery (PG) |

Both layers must pass before execution.

### Credential Discovery Chain

```
1. Pod env vars: MYSQL_ROOT_PASSWORD, POSTGRES_PASSWORD, DATABASE_URL, DB_PASSWORD, PGUSER, PGPASSWORD
2. Secret volumes: spec.volumes[].secret, spec.containers[].envFrom[].secretRef
3. Operator conventions: {name}-mysql-secret, {name}-postgresql, {name}-pguser-*
4. Chat fallback: AI asks user in Chat (one-time, not persisted)
→ Cache hit: sync.Map by namespace/podName key
```

## Testing Strategy

- **Safety tests**: Table-driven tests for SQL whitelist/blacklist, edge cases (comments, CTE, multi-statement)
- **Adapter tests**: Mock CLI output → `ParseOutput()` → verify QueryResult (tab-separated for MySQL, pipe-separated for PG)
- **Discovery tests**: Fake Pod list with various image/port/label combos → verify correct DatabaseInfo
- **Credential tests**: Mock Pod spec with env vars and Secret refs → verify Credentials extraction
- **Integration tests**: minikube/kind cluster with MySQL + PG StatefulSets for end-to-end discovery → schema → query

## Extension Points

- **Phase 5**: MongoDB adapter (different discovery heuristics, no INFORMATION_SCHEMA, `mongosh` CLI)
- **Phase 5**: Redis adapter (key-value semantics, `redis-cli`)
- **Future**: Port-forward + native DB driver mode (alternative to kubectl exec for better performance)
- **Future**: Query result caching / history
