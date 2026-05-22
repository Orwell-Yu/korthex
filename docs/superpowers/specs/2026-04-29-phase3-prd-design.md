# Korthex Phase 3 — Product Requirements Document

> **Korthex** = K8s + Cortex — The Intelligent Brain of Your Kubernetes Cluster
>
> *"Speak to your cluster, see everything."*

**Version:** v0.3 (Phase 3 - DB Intelligence & Token Metrics)
**Date:** 2026-04-29
**Author:** Mio
**Prerequisite:** Phase 2 (Deep Analysis & Enhanced Browser) fully implemented

---

## 1. Phase 3 Vision

Phase 1 让 Korthex 能**查日志**。Phase 2 让 Korthex 能**分析日志**。Phase 3 让 Korthex 能**查数据库**。

Phase 3 的核心跨越：
- **数据源扩展**：从仅日志到直接查询 K8s 集群内的数据库，用自然语言代替 SQL
- **AI 能力**：从"帮你查日志"到"帮你理解数据库中的业务数据"，自动发现 schema、外键关联、多表跟进
- **可观测性**：Chat Panel 实时显示 LLM 调用指标（iterations、tokens、context usage），让用户对 AI 行为有完整可见性

**市场空白：** 调研确认，目前没有任何工具将 K8s kubeconfig 访问 + 自然语言数据库查询 + TUI 结合在一起。Korthex 将是这个领域的首创。

---

## 2. Scope (范围)

### 2.1 Phase 3 确认范围

| 能力域 | 功能 | 优先级 |
|--------|------|--------|
| **DB Intelligence** | AI 自动发现集群内 DB Pod（MySQL/PostgreSQL） | P0 |
| **DB Intelligence** | 凭据自动获取（Secret/env → Chat 交互兜底） | P0 |
| **DB Intelligence** | NL → Schema 自省 → SQL 生成 → 执行（严格只读） | P0 |
| **DB Intelligence** | 多表关联发现（外键 + AI 推断） + 自动跟进查询 | P0 |
| **DB Intelligence** | SQL 安全双层检查（关键词白名单 + AST 解析器校验） | P0 |
| **Data Viewer** | Log Panel 双模式（Log Viewer / Data Viewer 自动切换） | P0 |
| **Data Viewer** | 表格渲染 + tab 切换（鼠标 + 键盘） | P0 |
| **Data Viewer** | 实时 tab 追加（每查出一个关联表立即发射到 Data Viewer） | P0 |
| **Data Viewer** | 关联路径 FIFO 管理（默认 10 路径，可配置，上限 100） | P1 |
| **Token Metrics** | Chat Panel header 常驻显示 iter / in / out / cache / ctx% | P0 |
| **Token Metrics** | LLM adapter 提取 TokenUsage（各 Provider SDK response） | P0 |
| **Token Metrics** | Agent 层 metrics 聚合 + 实时更新 | P0 |

### 2.2 明确排除

| 功能 | 原因 | 推迟到 |
|------|------|--------|
| MongoDB 支持 | 无外键概念，"多表关联查询"语义不同 | Phase 5 |
| Redis 支持 | KV 结构，无表/外键概念 | Phase 5 |
| 数据库写操作（INSERT/UPDATE/DELETE） | 安全策略：Phase 3 严格只读 | 后续 Phase |
| 集群管理操作（scale/restart/delete） | Phase 4 专属 | Phase 4 |
| Port-forward + 原生 DB 驱动方式 | Phase 3 使用 kubectl exec 方式 | 后续可选 |

---

## 3. Competitive Research & Reference Projects (竞品调研与可参考项目)

### 3.1 市场空白

调研确认：**没有任何现有工具** 将 K8s kubeconfig 访问 + 自然语言数据库查询 + TUI 结合在一起。现有工具分布：

| 类别 | 代表工具 | 缺失能力 |
|------|---------|---------|
| K8s TUI | k9s, KDash | 无数据库查询，无 AI |
| TUI DB Client | lazysql, dblab | 无 K8s 集成，无自然语言 |
| NL→SQL | LangChain SQL Agent, Vanna | 无 K8s 集成，无 TUI |
| K8s + AI | kubectl-ai, k8sgpt | 无数据库查询，无 TUI |

### 3.2 可参考项目

| 参考维度 | 项目 | Stars | License | 参考内容 |
|---------|------|-------|---------|---------|
| **NL→SQL Agent 架构** | [LangChain SQL Agent](https://github.com/langchain-ai/langchain) | 135k | MIT | 工具拆分模式：`list_tables` / `describe_table` / `run_query` 作为独立 tool，LLM agent 自主编排。直接适配 Korthex 已有的 agentic loop |
| **NL→SQL 精度** | [vanna-ai/vanna](https://github.com/vanna-ai/vanna) | 23k | MIT | RAG 增强 schema 理解：存储 DDL + 成功查询对作为上下文注入，提升 SQL 生成准确率 |
| **Schema 关联发现** | [FalkorDB/QueryWeaver](https://github.com/FalkorDB/QueryWeaver) | 335 | AGPL | 将 schema 建模为图（表=节点、外键=边），复杂 JOIN 发现更自然。架构可参考，License 不可直接引用 |
| **Go TUI DB 交互** | [jorgerojas26/lazysql](https://github.com/jorgerojas26/lazysql) | 3.7k | MIT | Go + TUI 数据库管理，Vim 键绑定，内置 read-only 模式，表格浏览+tab切换布局。最接近 Korthex Data Viewer 的 UX 参考 |
| **Go TUI DB 客户端** | [danvergara/dblab](https://github.com/danvergara/dblab) | 2.9k | MIT | Go + Bubble Tea/Lipgloss 同技术栈，单二进制零依赖，数据库连接管理和结果渲染 |
| **SQL 只读安全** | [sosedoff/pgweb](https://github.com/sosedoff/pgweb) | 9.3k | MIT | Go 实现的双层只读保护：应用层关键词白名单 + 数据库层 read-only transaction |
| **SQL 解析（MySQL）** | [xwb1989/sqlparser](https://github.com/xwb1989/sqlparser) | - | Apache-2.0 | 从 Vitess 提取的 MySQL SQL 解析器，AST 检查语句类型 |
| **SQL 解析（PG）** | [wasilibs/go-pgquery](https://github.com/wasilibs/go-pgquery) | - | BSD-3 | 纯 Go 无 CGO 的 PostgreSQL 解析器（WASM），保持单二进制原则 |
| **Token 指标 UX** | [paul-gauthier/aider](https://github.com/paul-gauthier/aider) `/tokens` | 30k+ | Apache-2.0 | 按组件拆分 token 消耗（system/repo map/chat history），最清晰的指标分解 UX |
| **Go AI TUI Token 统计** | [opencode-ai/opencode](https://github.com/opencode-ai/opencode) | 4.6k | MIT | Go + Bubble Tea 的 AI 编码 TUI，同技术栈，SQLite 持久化 token 统计 |
| **表格渲染** | charmbracelet/bubbles table | 7.7k | MIT | Korthex 已有依赖，交互式表格组件，零额外开销 |
| **表格渲染增强** | [olekukonko/tablewriter](https://github.com/olekukonko/tablewriter) | 4.7k | MIT | 数字对齐、cell 合并、流式渲染，适合大查询结果展示 |
| **K8s DB Operator** | [cloudnative-pg/cloudnative-pg](https://github.com/cloudnative-pg/cloudnative-pg) | 5k+ | Apache-2.0 | kubectl 插件发现 PostgreSQL Pod 并提供工具链。Pod 元数据发现模式可参考 |

### 3.3 关键借鉴总结

1. **LangChain SQL Agent 的 tool 设计** → 直接映射到 Korthex agent tools（`discover_databases` / `get_db_schema` / `query_database`）
2. **pgweb 的双层只读** → 应用层 SQL 关键词白名单 + 解析器 AST 校验
3. **lazysql 的 UX** → 数据库 TUI 的表格浏览、tab 切换、Vim 键绑定
4. **aider 的 token 分解** → 按组件显示 token 消耗，不只是一个总数
5. **dblab 的架构** → Go + Bubble Tea/Lipgloss 同技术栈的数据库交互参考

---

## 4. Feature Specification — DB Intelligence

### 4.1 Architecture Overview

新增 `internal/db/` 模块（Layer 1），类似 `internal/k8s/` 的定位：

```
Layer 0 (Foundation):   config, pkg/logparse, pkg/redact
Layer 1 (Core):         k8s, llm, history, db (新)    ← db 依赖 config + k8s(exec 能力)
Layer 2 (Intelligence): agent                         ← 新增 db 相关 tools
Layer 3 (Presentation): ui                            ← Data Viewer 模式
Layer 4 (Integration):  app
```

**Import 规则：**
- `db` 可 import `config`、`k8s`（exec 能力）
- `db` 不可 import `agent`、`ui`、`llm`
- `agent` import `db`（调用发现/查询/schema）
- `ui` 仅 import `db` 的类型定义（`QueryResult`、`DataTab` 等），不 import db 逻辑
- `app` 负责注入 db 实例到 agent

### 4.2 Module Structure

```
internal/db/
├── CLAUDE.md           # 模块规则
├── README.md           # 接口/文件/测试
├── types.go            # Korthex 内部 DB 类型（不泄漏任何数据库驱动类型）
├── discovery.go        # DB Pod 自动发现（扫描镜像名/端口/label）
├── credentials.go      # 凭据获取（Secret/env → 缓存）
├── executor.go         # kubectl exec SQL 执行引擎
├── schema.go           # Schema 自省（INFORMATION_SCHEMA 查询 + 解析）
├── safety.go           # SQL 安全白名单（仅允许 SELECT/SHOW/DESCRIBE/EXPLAIN）
├── mysql_adapter.go    # MySQL 适配器（CLI 命令构造 + 输出解析）
├── postgres_adapter.go # PostgreSQL 适配器（CLI 命令构造 + 输出解析）
├── adapter.go          # Adapter 接口定义 + 注册器
└── db_test.go          # 测试
```

### 4.3 Core Types

```go
// internal/db/types.go

// DatabaseInfo — 发现的数据库实例
type DatabaseInfo struct {
    Namespace   string
    PodName     string
    ServiceName string       // 关联的 Service（可选）
    DBType      DatabaseType // MySQL | PostgreSQL
    Port        int
    Database    string       // 数据库名
    Detected    time.Time
}

type DatabaseType string
const (
    MySQL      DatabaseType = "mysql"
    PostgreSQL DatabaseType = "postgresql"
)

// TableSchema — 表结构
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

// ForeignKey — 外键关系
type ForeignKey struct {
    ChildTable    string
    ChildColumn   string
    ParentTable   string
    ParentColumn  string
}

// QueryResult — 查询结果（结构化）
type QueryResult struct {
    Columns   []string
    Rows      [][]string   // 文本形式，exec 输出解析
    RowCount  int
    Truncated bool         // 是否被截断（超过 max_rows_per_table）
}

// Credentials — 数据库凭据
type Credentials struct {
    Username string
    Password string
    Database string
    Host     string  // 通常 localhost（exec 进 Pod 后）
    Port     int
}
```

### 4.4 Adapter Interface

> **参考：** lazysql / dblab 的多数据库抽象模式

```go
// internal/db/adapter.go

type Adapter interface {
    // 构造 exec 命令：返回在 Pod 内执行的完整命令字符串
    BuildCommand(sql string, creds Credentials) []string

    // 解析 CLI 输出为结构化结果
    ParseOutput(raw []byte) (*QueryResult, error)

    // Schema 自省 SQL（INFORMATION_SCHEMA 查询）
    ListTablesSQL(database string) string
    DescribeTableSQL(database, table string) string
    ForeignKeysSQL(database string) string

    // 检测 Pod 内 CLI 工具是否可用
    DetectCLI() []string  // 返回要执行的检测命令

    // 数据库类型
    Type() DatabaseType
}

var adapters = map[DatabaseType]Adapter{
    MySQL:      &MySQLAdapter{},
    PostgreSQL: &PostgresAdapter{},
}

func AdapterFor(dbType DatabaseType) (Adapter, bool)
```

### 4.5 Database Discovery

**发现策略（优先级从高到低）：**

| 信号 | 优先级 | 匹配规则 |
|------|--------|---------|
| 镜像名 | 1 | `mysql*`, `mariadb*`, `postgres*`, `postgis*` |
| 容器端口 | 2 | 3306 (MySQL), 5432 (PostgreSQL) |
| Label | 3 | `app.kubernetes.io/name=mysql\|postgresql`, `app=mysql\|postgres` |
| StatefulSet 关联 | 4 | 数据库通常是 StatefulSet，优先检查 |

**发现流程：**

```
用户: "查一下 production 的订单数据"
      │
      ▼
AI 解析意图 → 调用 discover_databases(namespace="production")
      │
      ▼
discovery.go 扫描 namespace 下所有 Pod:
  1. 镜像名匹配: mysql*, postgres*, mariadb*
  2. 端口匹配: 3306(MySQL), 5432(PG)
  3. Label 匹配: app.kubernetes.io/name=mysql 等
  4. StatefulSet 关联（数据库通常是 StatefulSet）
      │
      ▼
找到候选 DB Pod 列表
      │
      ├── 找到 1 个 → 直接使用
      ├── 找到多个 → 返回列表让 AI 决策（或问用户）
      └── 找到 0 个 → AI 告知用户 "未在 production 中发现数据库 Pod，请提供更多信息"
```

**用户可自定义镜像匹配规则：**

```yaml
database:
  discovery:
    image_patterns:
      - "custom-mysql-*"
      - "my-company/postgres-*"
```

### 4.6 Credentials Discovery

> **参考：** CloudNativePG kubectl 插件的 Pod 元数据发现模式

**发现顺序（优先级从高到低）：**

| 优先级 | 来源 | 查找方式 |
|--------|------|---------|
| 1 | Pod 环境变量 | 扫描 `MYSQL_ROOT_PASSWORD`, `POSTGRES_PASSWORD`, `DATABASE_URL`, `DB_PASSWORD`, `MYSQL_USER`, `PGUSER`, `PGPASSWORD` 等常见 key |
| 2 | 关联 Secret | Pod `spec.volumes[].secret` 和 `spec.containers[].envFrom[].secretRef` |
| 3 | Operator 命名约定 | `{name}-mysql-secret`, `{name}-postgresql`, `{name}-pguser-*` |
| 4 | Chat 交互兜底 | 以上全部失败 → AI 在 Chat 中提示用户输入（一次性使用，不持久化密码） |

**凭据缓存：**
- 成功获取的凭据缓存在内存中（会话内有效）
- 会话结束时清除，不持久化到磁盘
- 不同 namespace/Pod 独立缓存

### 4.7 SQL Safety — Dual-Layer Check

> **参考：** pgweb `--readonly` 双层只读保护模式

```
AI 生成 SQL
      │
      ▼ ── Layer 1: 关键词白名单（轻量，<1ms）──
  前缀检查: 去除空格/注释后，首词必须是 SELECT|SHOW|DESCRIBE|EXPLAIN|WITH
  黑名单扫描: 全文扫描 INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|CREATE|GRANT|REVOKE
  多语句检测: 禁止包含未被引号包裹的分号
      │
      ├── 不通过 → 拒绝，告知用户 "仅支持只读查询"
      │
      ▼ ── Layer 2: SQL 解析器 AST 校验（稳健，<10ms）──
  MySQL: xwb1989/sqlparser → 检查 AST 根节点是否为 *sqlparser.Select
  PostgreSQL: wasilibs/go-pgquery → 检查语句类型是否为 SELECT
      │
      ├── 不通过 → 拒绝（捕获关键词检查遗漏的边缘 case）
      │
      ▼
  通过 → 执行
```

**SQL 解析器依赖：**

| 解析器 | 数据库 | License | 纯 Go | 说明 |
|--------|--------|---------|-------|------|
| xwb1989/sqlparser | MySQL | Apache-2.0 | ✓ | 从 Vitess 提取，成熟稳定 |
| wasilibs/go-pgquery | PostgreSQL | BSD-3-Clause | ✓ (WASM) | 原生 PG 解析精度，无 CGO |

两者均保持 Korthex 的单二进制零外部依赖原则。

### 4.8 Schema Introspection

**MySQL via INFORMATION_SCHEMA：**

```sql
-- 表和列
SELECT table_name, column_name, data_type, is_nullable, column_key, column_default
FROM information_schema.columns
WHERE table_schema = '{database}'
ORDER BY table_name, ordinal_position;

-- 外键
SELECT
  kcu.TABLE_NAME AS child_table,
  kcu.COLUMN_NAME AS child_column,
  kcu.REFERENCED_TABLE_NAME AS parent_table,
  kcu.REFERENCED_COLUMN_NAME AS parent_column
FROM information_schema.KEY_COLUMN_USAGE kcu
JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
  ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
  AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA
WHERE kcu.REFERENCED_TABLE_NAME IS NOT NULL
  AND kcu.TABLE_SCHEMA = '{database}';
```

**PostgreSQL via INFORMATION_SCHEMA：**

```sql
-- 表和列
SELECT table_name, column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY table_name, ordinal_position;

-- 外键
SELECT
  tc.table_name AS child_table,
  kcu.column_name AS child_column,
  ccu.table_name AS parent_table,
  ccu.column_name AS parent_column
FROM information_schema.table_constraints AS tc
JOIN information_schema.key_column_usage AS kcu
  ON tc.constraint_name = kcu.constraint_name
JOIN information_schema.constraint_column_usage AS ccu
  ON tc.constraint_name = ccu.constraint_name
WHERE tc.constraint_type = 'FOREIGN KEY';
```

**Schema 缓存策略：**

| 操作 | 缓存行为 |
|------|---------|
| 首次连接某数据库 | 查询 INFORMATION_SCHEMA，结果缓存在内存 |
| 同一会话再次查询 | 直接用缓存的 schema，不重新查询 |
| 切换到不同数据库/namespace | 新建缓存条目 |
| 会话结束 | 缓存清除（不持久化） |
| 用户说"刷新 schema" | 清除对应缓存，重新查询 |

### 4.9 NL→SQL Full-Auto Flow

> **参考：** LangChain SQL Agent 的工具编排模式 + vanna 的 schema 上下文注入

**完整流程示例：**

```
用户: "查一下 production 里订单 #1234 的详情"
      │
      ▼ ── iteration 1 ──
AI 调用 discover_databases(namespace="production")
  → 返回: [{PodName:"mysql-0", DBType:"mysql", Database:"orderdb"}]
      │
      ▼ ── iteration 2 ──
AI 调用 get_db_credentials(namespace="production", podName="mysql-0")
  → 扫描 env/Secret → 返回: {Username:"root", Password:"***", Database:"orderdb"}
      │
      ▼ ── iteration 3 ──
AI 调用 get_db_schema(namespace="production", podName="mysql-0", database="orderdb")
  → exec: mysql -u root -p*** -e "SELECT ... FROM INFORMATION_SCHEMA..."
  → 返回:
    Tables: orders, order_items, products, users, payments
    ForeignKeys:
      order_items.order_id → orders.id
      order_items.product_id → products.id
      orders.user_id → users.id
      payments.order_id → orders.id
      │
      ▼ ── iteration 4 ──
AI 生成 SQL（基于 schema 上下文 + 外键关系 + 字段名推断）
AI 调用 query_database(sql="SELECT * FROM orders WHERE id = 1234", limit=100)
  → safety check 通过（SELECT 语句）
  → exec: mysql -u root -p*** orderdb -e "SELECT * FROM orders WHERE id = 1234"
  → 结果立即 p.Send(DataResultMsg{table:"orders", ...})
  → Data Viewer 创建第一个 tab，用户已能看到订单数据
      │
      ▼ ── iteration 5 ──
AI 调用 query_database(sql="SELECT * FROM order_items WHERE order_id = 1234")
  → 结果立即 p.Send(DataResultMsg{table:"order_items", ...})
  → Data Viewer 追加 tab
      │
      ▼ ── iteration 6 ──
AI 调用 query_database(sql="SELECT * FROM users WHERE id = 42")
  → 结果立即 p.Send(DataResultMsg{table:"users", ...})
  → Data Viewer 追加 tab
      │
      ▼ ── iteration 7 ──
AI 调用 query_database(sql="SELECT * FROM payments WHERE order_id = 1234")
  → 结果立即 p.Send(...)
      │
      ▼ ── iteration 8 ──
AI 调用 query_database(sql="SELECT * FROM products WHERE id IN (101, 102)")
  → 结果立即 p.Send(...)
      │
      ▼
Chat 显示摘要：找到订单 #1234，用户 42，3 件商品，已支付 ¥299
Data Viewer 已实时显示 5 个 tab（orders / order_items / users / payments / products）
```

**关键设计：每查出一个表立即发射到 Data Viewer，不等全部关联查询完成。** 类似现有 `NavigateToResourceMsg` 的拦截模式，`app.go` 拦截 `query_database` 的 tool result，解析后发射 `DataResultMsg`。

### 4.10 System Prompt Enhancement (Database Mode)

```
## Database Query Mode

当用户请求查询数据库时，遵循以下流程：

1. **发现**: 调用 discover_databases 定位 DB Pod
2. **认证**: 调用 get_db_credentials 获取凭据。如果失败，直接问用户
3. **Schema**: 调用 get_db_schema 理解表结构和外键关系
4. **查询**: 根据用户意图生成 SELECT SQL，调用 query_database 执行
5. **关联**: 基于外键（显式）和字段名（推断，如 xxx_id 可能指向 xxx 表），
   自动查询关联表的相关数据。遵循以下规则：
   - 关联路径最多 {max_relation_paths} 条（默认 10，FIFO 淘汰最旧路径）
   - 每个表最多拉取 {max_rows_per_table} 行（默认 100）
   - 优先跟进直接外键关系，其次推断字段名关联
   - 每查出一个关联表的结果，立即发送到 Data Viewer

**安全规则：**
- 仅生成 SELECT / SHOW / DESCRIBE / EXPLAIN 语句
- 禁止任何写操作（INSERT/UPDATE/DELETE/DROP/ALTER/TRUNCATE）
- 禁止多语句（分号分隔）
- 如果用户要求写操作，回复："数据库写操作将在后续版本支持，当前仅支持只读查询。"

**Schema 上下文注入格式：**
Database: orderdb (MySQL 8.0)
Tables:
  orders (id PK, user_id FK→users.id, status, total, created_at)
  order_items (id PK, order_id FK→orders.id, product_id FK→products.id, quantity, price)
  products (id PK, name, price, category)
  users (id PK, name, email, created_at)
  payments (id PK, order_id FK→orders.id, method, amount, paid_at)

**模糊查询处理：**
- 用户查询过于模糊（如"查一下所有数据"）时，每个表最多拉取 max_rows_per_table 行
- 最多追踪 max_relation_paths 个关联路径
- 在 Chat 中告知用户范围限制，建议缩小查询范围
```

### 4.11 New Agent Tools

| Tool | 功能 | 参数 |
|------|------|------|
| `discover_databases` | 扫描 namespace 发现 DB Pod | `namespace` (必填) |
| `get_db_credentials` | 获取数据库凭据 | `namespace`, `podName` |
| `get_db_schema` | 获取 schema（表结构+外键） | `namespace`, `podName`, `database` |
| `query_database` | 执行只读 SQL 查询 | `namespace`, `podName`, `database`, `sql`, `limit` (可选，默认 100) |
| `get_foreign_keys` | 获取指定表的外键关系 | `namespace`, `podName`, `database`, `table` |

**Command Display（给用户看的可读命令表示）：**

```
discover_databases    → "Scanning namespace 'production' for database pods..."
get_db_credentials    → "Fetching credentials for mysql-0 in production..."
get_db_schema         → "$ mysql -u root orderdb -e 'SHOW TABLES; SELECT ... FROM INFORMATION_SCHEMA'"
query_database        → "$ mysql -u root orderdb -e 'SELECT * FROM orders WHERE id = 1234'"
get_foreign_keys      → "$ mysql -u root orderdb -e 'SELECT ... FROM KEY_COLUMN_USAGE ...'"
```

---

## 5. Feature Specification — Data Viewer UI

### 5.1 Log Panel Dual-Mode Architecture

Log Panel 根据场景自动切换模式：

```go
type ViewerMode int
const (
    ModeLog  ViewerMode = iota  // 原有日志查看模式
    ModeData                     // 数据库查询结果模式
)
```

**模式切换触发：**

| 触发条件 | 行为 |
|---------|------|
| Agent `query_database` 工具返回第一个结果 | 自动切换到 Data Viewer 模式，创建 tab 并激活 |
| Agent 后续关联查询返回 | 追加新 tab，header 上新 tab 名闪烁提示，不自动切换当前 tab |
| Agent 仍在运行（还有关联查询在跑） | header 显示 spinner + `fetching relations...` |
| Agent 完成所有查询 | spinner 消失，header 显示最终 tab 数量 |
| 用户按 `Esc` | 回到 Log Viewer 模式（Data Viewer 数据保留） |
| 用户手动浏览 Pod 日志 / Agent `kubectl_logs` 返回 | 自动切换到 Log Viewer 模式 |

### 5.2 Data Viewer Layout

> **参考：** lazysql 的表格浏览 + tab 切换布局

```
┌─ Data Viewer ─ orders | order_items | users | payments ─────┐
│  ▲ tab 可鼠标点击切换，当前 tab 高亮                          │
│                                                               │
│  orders (WHERE id = 1234)                      [1/1 rows]    │
│ ┌──────┬─────────┬────────┬─────────┬──────────────────────┐ │
│ │ id   │ user_id │ status │ total   │ created_at           │ │
│ ├──────┼─────────┼────────┼─────────┼──────────────────────┤ │
│ │ 1234 │ 42      │ paid   │ 299.00  │ 2026-04-29 10:30:00  │ │
│ └──────┴─────────┴────────┴─────────┴──────────────────────┘ │
│                                                               │
│ Foreign Keys: user_id → users.id                              │
│               ← order_items.order_id, payments.order_id       │
│                                                               │
│ [←/→] switch tab  [j/k] scroll rows  [h/l] scroll cols      │
│ [Enter] 查看关联  [Esc] 返回 Log 模式  [s] 导出 CSV          │
└───────────────────────────────────────────────────────────────┘
```

### 5.3 Table Switching Interaction

| 操作 | 键盘 | 鼠标 | 行为 |
|------|------|------|------|
| 切换到左边 tab | `←` 或 `[` | 点击 header 中的 tab 名 | 切换并恢复该 tab 的滚动位置 |
| 切换到右边 tab | `→` 或 `]` | 点击 header 中的 tab 名 | 同上 |
| 直接跳转 tab | `1`-`9` 数字键 | 点击 | 跳转到第 N 个 tab |
| 行内滚动 | `j`/`k` | 滚轮 | 上下滚动表格行 |
| 列滚动 | `h`/`l` | - | 表格宽度超出 panel 时左右滚动 |
| 查看当前行关联数据 | `Enter` | - | AI 自动查询该行外键指向的关联表 |
| 导出当前 tab | `s` | - | 导出为 CSV 到 `~/.korthex/data/` |
| 退出 Data Viewer | `Esc` | - | 回到 Log Viewer 模式 |

### 5.4 Tab FIFO Management

```
关联查询路径到达上限时（默认 10）：

  tabs: [orders, order_items, users, payments, products, ...]
                                                         │
新增 tab: shipping_address                              ▼
  → 淘汰最旧的 tab (非主查询表)
  tabs: [orders, order_items, users, payments, ..., shipping_address]
  → 主查询表（用户直接查询的第一个表，IsPrimary=true）始终置顶，不被淘汰
```

### 5.5 Real-time Tab Appending

**Msg 设计：**

```go
// 每个查询结果独立发射（不等全部完成）
type DataResultMsg struct {
    TableName  string
    JoinPath   string          // "orders → order_items via order_id"
    Result     db.QueryResult
    IsPrimary  bool            // true = 用户直接查询的主表（置顶，不被 FIFO 淘汰）
}
```

`app.go` 拦截 agent 的 `query_database` tool result，解析后发射 `DataResultMsg` 给 UI。类似现有 `NavigateToResourceMsg` 的拦截模式。

**UI 行为时序：**

| 事件 | Data Viewer 反应 |
|------|-----------------|
| 收到第一个 DataResultMsg | 切换到 Data Viewer 模式，创建 tab 并激活 |
| 收到后续 DataResultMsg | 追加 tab，header 上新 tab 名闪烁提示，**不自动切换**当前 tab |
| Agent 仍在运行 | header 显示 spinner + `fetching relations...` |
| Agent 完成 | spinner 消失，header 显示最终 tab 数量 |

### 5.6 Table Rendering Strategy

> **参考：** lazysql 表格渲染 + bubbles/table 组件 + olekukonko/tablewriter 数字对齐

| 场景 | 渲染方式 |
|------|---------|
| 列数 ≤ panel 宽度 | 直接渲染，列宽自适应 |
| 列数超出 panel | 水平滚动（`h`/`l`），固定首列（主键） |
| 单元格内容过长 | 截断到 40 字符 + `…`，光标停留时 footer 显示完整值 |
| NULL 值 | 灰色 `NULL` |
| 数字列 | 右对齐 |
| 空结果 | 居中显示 "No rows returned" |

---

## 6. Feature Specification — Token Metrics

### 6.1 Data Source

LLM Provider 的 API response 中已包含 token 使用信息：

```go
// internal/llm/provider.go 扩展

type TokenUsage struct {
    InputTokens       int   // 输入 token 数
    OutputTokens      int   // 输出 token 数
    CacheReadTokens   int   // 缓存命中读取的 token 数（Anthropic/OpenAI 支持）
    CacheWriteTokens  int   // 缓存写入的 token 数
}
```

**各 Provider 数据来源：**

| Provider | Input | Output | Cache |
|----------|-------|--------|-------|
| OpenAI | `resp.Usage.PromptTokens` | `resp.Usage.CompletionTokens` | `resp.Usage.PromptTokensDetails.CachedTokens` |
| Anthropic | `resp.Usage.InputTokens` | `resp.Usage.OutputTokens` | `resp.Usage.CacheReadInputTokens` / `CacheCreationInputTokens` |
| Gemini | `resp.UsageMetadata.PromptTokenCount` | `resp.UsageMetadata.CandidatesTokenCount` | `resp.UsageMetadata.CachedContentTokenCount` |

### 6.2 Agent Metrics Aggregation

```go
// internal/agent/metrics.go (新文件)

type AgentMetrics struct {
    Iterations     int          // 当前查询的 agentic loop 迭代次数
    TotalInput     int          // 累计 input tokens（本次查询）
    TotalOutput    int          // 累计 output tokens（本次查询）
    TotalCache     int          // 累计 cache hit tokens（本次查询）
    ContextUsage   float64      // context window 使用率 (0.0 ~ 1.0)
    MaxContext     int          // 当前 model 的 context window 大小
}

// 每次 LLM 调用后累加
func (m *AgentMetrics) Add(usage llm.TokenUsage)

// Agent 新一轮查询开始时重置
func (m *AgentMetrics) Reset()
```

### 6.3 Chat Panel Header Display

> **参考：** aider `/tokens` 的按组件分解 UX

**渲染示例：**

```
正常状态（agent 未运行）:
┌─ AI Assistant ──────────────────────────────────────────────┐

agent 运行中（实时更新）:
┌─ AI Assistant ⠹ ─ iter:3 │ in:2.1k out:847 cache:1.5k │ ctx:65% ─┐

agent 完成后（保持最终值直到下次查询）:
┌─ AI Assistant ─ iter:5 │ in:4.2k out:1.3k cache:2.8k │ ctx:72% ──┐
```

**格式化规则：**

| 指标 | 格式 | 示例 | 说明 |
|------|------|------|------|
| iterations | `iter:N` | `iter:3` | 当前查询的 loop 次数 |
| input tokens | `in:Nk` | `in:2.1k` | ≥1000 用 k，小数点一位 |
| output tokens | `out:Nk` | `out:847` | <1000 直接显示数字 |
| cache tokens | `cache:Nk` | `cache:1.5k` | 0 时隐藏整个字段 |
| context usage | `ctx:N%` | `ctx:65%` | 已使用 tokens / model max context |

**更新时机：**

| 事件 | 行为 |
|------|------|
| 用户提交新查询 | `Reset()` 清零所有指标 |
| 每次 LLM 调用返回 | `Add(usage)` 累加，`Iterations++`，`p.Send(MetricsUpdateMsg{})` |
| Agent 完成 | 指标冻结，保持显示直到下次查询 |
| 窗口 resize | 空间不足时逐级隐藏：先隐藏 cache → 再隐藏 ctx → 最后只保留 iter |

### 6.4 Context Window Size Table

```go
var modelContextSizes = map[string]int{
    "gpt-4o":            128000,
    "gpt-4o-mini":       128000,
    "claude-sonnet-4-6": 200000,
    "claude-opus-4-6":   200000,
    "gemini-2.0-flash":  1048576,
    // ... 更多 model 可通过 config 覆盖
}
```

---

## 7. Configuration (Phase 3 新增配置项)

```yaml
# config.yaml Phase 3 新增项

database:
  discovery:
    enabled: true                    # 数据库发现功能开关
    image_patterns: []               # 自定义镜像名匹配（追加到内置规则）
  query:
    max_rows_per_table: 100          # 单表最多拉取行数（上限 1000）
    max_relation_paths: 10           # 最多关联表路径数（FIFO，上限 100）
    timeout_seconds: 30              # 单次 SQL 执行超时

agent:
  # 已有配置保持不变
  token_metrics: true                # Chat Panel 显示 token 指标（默认开启）
```

**与 Phase 1/2 配置项的关系：**
- Phase 1/2 的所有配置项保持不变
- Phase 3 新增项均有合理默认值，升级无需修改 config.yaml
- `database.discovery.enabled: true` 默认开启发现（但不主动连接，仅在 AI 需要时触发）
- `database.query.max_rows_per_table` 和 `max_relation_paths` 有硬上限（1000/100），防止用户误配置导致大量数据拉取

---

## 8. New Dependencies (Phase 3 新增依赖)

| 依赖 | License | 用途 | 纯 Go | 单二进制 |
|------|---------|------|-------|---------|
| xwb1989/sqlparser | Apache-2.0 | MySQL SQL 解析 → AST 安全校验 | ✓ | ✓ |
| wasilibs/go-pgquery | BSD-3-Clause | PostgreSQL SQL 解析 → AST 安全校验（WASM） | ✓ | ✓ |

**License 兼容性：** Apache-2.0 和 BSD-3-Clause 均与 Korthex 的 Apache-2.0 兼容。需更新 `NOTICE` 文件。

**不引入的依赖：** 不引入 `database/sql` 驱动（go-sql-driver/mysql, lib/pq, jackc/pgx），因为走 kubectl exec 路径，不需要原生数据库连接。

---

## 9. Keyboard Shortcuts (Phase 3 新增/变更)

| Key | Action | Context | Phase |
|-----|--------|---------|-------|
| `←` / `[` | 切换到左边 tab | Data Viewer | Phase 3 |
| `→` / `]` | 切换到右边 tab | Data Viewer | Phase 3 |
| `1`-`9` | 直接跳转到第 N 个 tab | Data Viewer | Phase 3 |
| `j`/`k` | 上下滚动表格行 | Data Viewer | Phase 3 |
| `h`/`l` | 左右滚动表格列 | Data Viewer | Phase 3 |
| `Enter` | 查看当前行关联数据 | Data Viewer | Phase 3 |
| `s` | 导出当前 tab 为 CSV | Data Viewer | Phase 3 |
| `Esc` | 退出 Data Viewer → Log Viewer | Data Viewer | Phase 3 |
| 鼠标点击 tab | 切换到对应 tab | Data Viewer header | Phase 3 |
| 滚轮 | 上下滚动表格行 | Data Viewer | Phase 3 |

**Phase 1/2 快捷键保持不变。** Data Viewer 模式下的 `j/k/h/l` 复用 Log Viewer 的键位习惯。

---

## 10. Non-Functional Requirements (Phase 3 增量)

| 需求 | Phase 2 指标 | Phase 3 指标 | 说明 |
|------|-------------|-------------|------|
| **DB 发现延迟** | N/A | < 2s（单 namespace） | 复用 Informer Cache，零 API 调用 |
| **凭据发现延迟** | N/A | < 3s（含 Secret 查询） | 需要额外 API 调用获取 Secret |
| **Schema 自省延迟** | N/A | < 5s（exec + parse） | kubectl exec 的网络开销 |
| **SQL 安全检查** | N/A | < 10ms（双层检查） | 关键词 <1ms + AST <10ms |
| **SQL 执行超时** | N/A | 可配置，默认 30s | 防止慢查询阻塞 |
| **Data Viewer 首次渲染** | N/A | < 100ms | bubbles/table 内存渲染 |
| **Tab 切换** | N/A | < 16ms (60fps) | 纯内存切换，复用预渲染 |
| **Token 指标更新** | N/A | 每次 LLM 调用后实时 | p.Send 异步更新，不阻塞 |
| **内存占用** | < 350MB | < 400MB（含 db 模块 + SQL 解析器 + schema 缓存） | 增量 < 50MB |
| **启动时间** | < 2.5s | < 2.5s（db 模块按需加载，不影响启动） | 无增量 |

---

## 11. Module Impact Analysis (模块影响分析)

| 模块 | 变更类型 | 具体变更 |
|------|---------|---------|
| **config** | 扩展 | 新增 `database` 配置段、`agent.token_metrics` |
| **internal/db/** (新) | 新增 | 数据库发现、凭据获取、exec 执行、schema 自省、SQL 安全、MySQL/PG adapter |
| **internal/k8s** | 微扩展 | 暴露 exec 能力供 db 模块使用 |
| **internal/llm** | 微扩展 | 各 adapter 提取 TokenUsage 结构（从 SDK response 中解析） |
| **internal/agent** | 扩展 | 新增 5 个 db tools + AgentMetrics + system prompt 数据库场景 |
| **internal/ui** | 扩展 | LogViewer 双模式 + DataViewModel + Chat header token 指标 |
| **internal/app** | 扩展 | db 模块依赖注入 + DataResultMsg 拦截分发 |
| **pkg/logparse** | 不变 | |
| **pkg/redact** | 微扩展 | 数据库查询结果发给 LLM 前也需过脱敏引擎 |

---

## 12. Implementation Difficulty Assessment (实现难度评估)

| 功能 | 难度 | 代码库现有支撑 | 参考项目 |
|------|------|---------------|---------|
| **DB Pod 发现** | 低 | 复用 Informer Cache 扫描 Pod 镜像/端口/label | CloudNativePG |
| **凭据获取** | 中 | Pod env 解析简单，Secret 发现需遍历 volume mount + envFrom | CloudNativePG |
| **MySQL adapter** | 中 | CLI 命令构造简单，难点在文本输出解析（对齐、边框、NULL） | dblab, lazysql |
| **PostgreSQL adapter** | 中 | 同上，psql 输出格式不同于 mysql CLI | dblab |
| **SQL 安全双层检查** | 中 | 关键词白名单简单，AST 解析需集成两个解析器 | pgweb |
| **Schema 自省** | 低-中 | INFORMATION_SCHEMA 查询标准化，解析复用 adapter | LangChain SQL Agent |
| **Agent tools (5个)** | 低 | 复用现有 tool dispatch 框架，加 case 即可 | Korthex Phase 1/2 |
| **NL→SQL prompt** | 高 | 核心难度在 prompt engineering，schema 注入格式影响 SQL 质量 | vanna, LangChain |
| **Data Viewer UI** | 中-高 | 双模式切换 + bubbles/table + tab 管理 + 鼠标支持 | lazysql |
| **实时 tab 追加** | 中 | 每次 tool result 发射 Msg，类似现有 NavigateToResourceMsg | Korthex Phase 1 |
| **Token 指标** | 低 | 各 SDK response 已有 usage 字段，聚合+渲染直接 | aider, OpenCode |

---

## 13. Success Metrics (Phase 3)

| 指标 | 目标 | 度量方式 |
|------|------|---------|
| DB 发现准确率 | > 90% 的标准 MySQL/PG Pod 被正确识别 | 在含 MySQL + PG 的测试集群中验证 |
| NL→SQL 准确率 | > 80% 首次生成正确 SQL | 20 条预设自然语言查询评估 |
| 多表关联完整性 | 外键关联表 100% 被发现 | 有外键的测试 schema 验证 |
| SQL 安全拦截率 | 100% 写操作被拦截 | 包含 INSERT/DELETE/DROP 的测试集 |
| Data Viewer 首次结果展示 | < 10s（从用户输入到第一个 tab 出现） | 端到端计时 |
| Tab 切换流畅度 | 60fps 无卡顿 | 10 个 tab 切换测试 |
| Token 指标准确性 | 与 Provider API 返回的 usage 完全一致 | 对比 API response 原始值 |

---

## 14. Decisions (Phase 3 已确认决策)

| # | 决策 | 理由 |
|---|------|------|
| P3-D1 | 数据库连接方式：kubectl exec | 零端口暴露，只需 exec RBAC 权限，与 Korthex 已有 K8s 交互模式一致 |
| P3-D2 | Phase 3 仅 MySQL + PostgreSQL | 有外键和 INFORMATION_SCHEMA，多表关联查询自然。MongoDB/Redis 推到 Phase 5 |
| P3-D3 | AI 智能发现 + 用户仅需模糊到 namespace | 降低使用门槛，AI 自动扫描 Pod 定位数据库 |
| P3-D4 | 凭据自动获取优先，Chat 交互兜底 | 优先从 Secret/env 获取，失败时不阻塞而是在 Chat 中交互式获取 |
| P3-D5 | 严格只读（仅 SELECT） | 与 Phase 1-2 的安全策略一致，数据库写操作留待后续 |
| P3-D6 | SQL 安全双层检查 | 关键词白名单 + AST 解析器，参考 pgweb 的 `--readonly` 模式 |
| P3-D7 | 关联发现：外键 + AI 智能推断 | 外键覆盖显式关系，AI 推断字段名（如 xxx_id→xxx 表）覆盖隐式关系 |
| P3-D8 | NL→SQL 全自动（无需用户确认 SQL） | SELECT 为只读安全操作，双层安全检查已保障。类比 Phase 1 日志查询直接执行 |
| P3-D9 | Log Panel 双模式（Log/Data） | 复用同一 Panel 位置，根据场景自动切换，避免布局膨胀 |
| P3-D10 | 表格切换：鼠标点击 + 键盘都支持 | 兼顾效率和易用性 |
| P3-D11 | 每查出一个表立即发射到 Data Viewer | 实时反馈，用户无需等待全部关联查询完成 |
| P3-D12 | Token 指标显示在 Chat Panel header | 常驻可见，不占用聊天区域 |
| P3-D13 | SQL 解析器：xwb1989/sqlparser (MySQL) + wasilibs/go-pgquery (PG) | 纯 Go + 无 CGO，保持单二进制原则 |

---

## Appendix A: Agent Tools Summary (Phase 3 完整列表)

### Phase 1 保留 Tools

| Tool | 功能 |
|------|------|
| `kubectl_get_namespaces` | 列出 namespaces |
| `kubectl_get_deployments` | 列出 deployments |
| `kubectl_get_pods` | 列出 pods |
| `kubectl_logs` | 获取单 Pod 日志 |
| `kubectl_logs_selector` | 按 label selector 获取多 Pod 日志 |
| `kubectl_describe` | Describe 资源 |
| `kubectl_get_events` | 获取 events |
| `navigate_resource_browser` | 导航 Resource Browser |
| `get_log_viewer_state` | 读取 Log Viewer 状态 |
| `search_visible_logs` | 搜索 Log Viewer 中的日志 |

### Phase 2 保留 Tools

| Tool | 功能 |
|------|------|
| `kubectl_get_statefulsets` | 列出 StatefulSets |
| `kubectl_get_daemonsets` | 列出 DaemonSets |
| `kubectl_get_jobs` | 列出 Jobs |
| `kubectl_get_cronjobs` | 列出 CronJobs |
| `severity_stats` | 统计 severity 分布 |
| `compare_logs` | 对比两个时间段日志 |
| `get_pod_metrics` | 获取 Pod 资源用量 |
| `trace_logs` | 跨 Service trace ID 搜索 |
| `bookmark_log_lines` | AI 标记重要日志行 |

### Phase 3 新增 Tools

| Tool | 功能 | 能力域 |
|------|------|--------|
| `discover_databases` | 扫描 namespace 发现 DB Pod | DB Intelligence |
| `get_db_credentials` | 获取数据库凭据 | DB Intelligence |
| `get_db_schema` | 获取 schema（表结构+外键） | DB Intelligence |
| `query_database` | 执行只读 SQL 查询 | DB Intelligence |
| `get_foreign_keys` | 获取指定表的外键关系 | DB Intelligence |

---

## Appendix B: Reference Projects License Compatibility

Phase 3 新增参考/依赖的 License 兼容性：

| Project | License | Usage | Compatible with Apache-2.0 |
|---------|---------|-------|---------------------------|
| xwb1989/sqlparser | Apache-2.0 | 直接依赖 | ✓ |
| wasilibs/go-pgquery | BSD-3-Clause | 直接依赖 | ✓ |
| LangChain SQL Agent | MIT | 架构参考（不直接引用代码） | ✓ |
| vanna-ai/vanna | MIT | 设计参考 | ✓ |
| lazysql | MIT | UX 参考 | ✓ |
| dblab | MIT | 架构参考 | ✓ |
| pgweb | MIT | 安全模式参考 | ✓ |
| olekukonko/tablewriter | MIT | 可能直接依赖（表格渲染增强） | ✓ |
| FalkorDB/QueryWeaver | AGPL | 架构概念参考（不引用代码） | 仅参考，不可嵌入 |

需更新 `NOTICE` 文件标注新增的直接依赖和设计参考。
