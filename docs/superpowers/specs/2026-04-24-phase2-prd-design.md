# Korthex Phase 2 — Product Requirements Document

> **Korthex** = K8s + Cortex — The Intelligent Brain of Your Kubernetes Cluster
>
> *"Speak to your cluster, see everything."*

**Version:** v0.2 (Phase 2 - Deep Analysis & Enhanced Browser)
**Date:** 2026-04-24
**Author:** Mio
**Prerequisite:** Phase 1 (Log Intelligence) fully implemented

---

## 1. Phase 2 Vision

Phase 1 让 Korthex 能**查日志**。Phase 2 让 Korthex 能**分析日志**。

Phase 2 的核心跨越：
- **AI 能力**：从"帮你执行 kubectl"到"帮你理解日志背后的问题"
- **资源覆盖**：从仅 Deployment 到覆盖 StatefulSet/DaemonSet/Job/CronJob
- **数据安全**：从全量发送到智能脱敏，面向企业场景就绪
- **用户体验**：Markdown 渲染、日志书签、对话历史，全面提升日常使用效率

---

## 2. Scope (范围)

### 2.1 Phase 2 确认范围

| 能力域 | 功能 | 优先级 |
|--------|------|--------|
| **Deep Analysis** | AI 日志分析增强（异常检测、错误归因、模式识别） | P0 |
| **Deep Analysis** | 对话历史持久化（modernc.org/sqlite） | P0 |
| **Deep Analysis** | Tool 调用结果智能摘要压缩 | P0 |
| **Deep Analysis** | 跨 Service 日志关联（AI 智能 trace/request ID） | P1 |
| **Deep Analysis** | AI Chat Markdown 渲染（glamour） | P0 |
| **Enhanced Browser** | Resource Browser 扩展（StatefulSet/DaemonSet/Job/CronJob） | P0 |
| **Enhanced Browser** | Pod 详情面板（Conditions/Events/Metrics） | P1 |
| **Enhanced Browser** | AI Agent 对新资源类型的感知 | P0 |
| **Data Safety** | 正则脱敏规则（默认 + 自定义） | P0 |
| **Data Safety** | 日志书签（Bookmark） | P1 |

### 2.2 明确排除

| 功能 | 原因 | 推迟到 |
|------|------|--------|
| Ollama 本地模型支持 | PRD 已标注"往后移，先不做" | Phase 5 |
| 日志可视化（热力图/柱状图） | 用户决策不纳入 Phase 2 | Phase 5 |
| 集群管理操作（scale/restart/delete） | 含三次确认机制 | Phase 4 |

---

## 3. Feature Specification — Deep Analysis

### 3.1 AI 日志分析增强

**目标：** AI 从"帮你查日志"进化到"帮你分析日志"，提供异常检测、错误归因、模式识别能力。

**新增 AI 分析场景：**

| 场景 | 用户输入示例 | AI 行为 |
|------|-------------|---------|
| 异常检测 | "这些日志有什么异常吗" | 分析日志频率分布、突增突降、罕见 pattern |
| 错误归因 | "这个 NullPointerException 是什么原因" | 向上追溯调用链，关联同时间段其他服务的错误 |
| 模式识别 | "这个 timeout 有没有规律" | 分析时间分布、关联负载变化、识别周期性 |
| 对比分析 | "和昨天同一时段的日志对比一下" | 拉取两个时间段日志，对比 severity 分布差异 |
| 根因推断 | "为什么今天 ERROR 比平时多" | 综合分析 Events + Describe + 日志，给出可能的根因链 |

**System Prompt 增强：**
- 新增 `analysis_mode` 指令块，要求 AI 在分析时采用结构化思维链：现象描述 → 数据采集 → 假设生成 → 验证 → 结论
- 强制输出格式：`## 发现` → `## 可能原因` → `## 建议操作` → `## 置信度`
- 引入 `severity_stats` 工具：统计当前 buffer 中各 severity 的数量和时间分布，作为分析的定量依据

**新增 Agent Tools：**

| Tool | 功能 | 参数 |
|------|------|------|
| `severity_stats` | 统计 Ring Buffer 中的 severity 分布 | `sinceMinutes` (可选, int, 默认全部) |
| `compare_logs` | 拉取两个时间段的日志做对比 | `namespace`, `selector`, `since1` (e.g. "2h"), `since2` (e.g. "26h"), `duration` (e.g. "1h") |
| `get_pod_metrics` | 获取 Pod 的 CPU/Memory 用量 | `namespace`, `podName` |

**severity_stats 工具设计：**

```
输入：sinceMinutes=30 (可选, 默认统计全部 buffer)
输出：
  Total lines: 4523
  Time range: 14:00:00 - 14:30:00
  Severity distribution:
    ERROR:   47 (1.04%)  [spike at 14:20-14:25: 38 lines]
    WARN:   213 (4.71%)
    INFO:  4102 (90.70%)
    DEBUG:  161 (3.56%)
  Top error patterns:
    NullPointerException: 23 occurrences
    ConnectionTimeout: 18 occurrences
    OutOfMemoryError: 6 occurrences
```

**compare_logs 工具设计：**

```
输入：namespace="production", selector="app=order-service",
      since1="2h" (period1 起点, 从现在往回), duration="1h" (每段时长),
      since2="26h" (period2 起点, 从现在往回, 即"昨天同一时段")
输出：
  Period 1 (today 14:00-15:00):   4523 lines, 47 ERROR, 213 WARN
  Period 2 (yesterday 14:00-15:00): 3891 lines, 12 ERROR, 178 WARN
  Delta:
    ERROR: +291.7% (12 → 47)  ⚠️ significant increase
    WARN:  +19.7%  (178 → 213)
    Total: +16.2%  (3891 → 4523)
  New error patterns in Period 1 (not seen in Period 2):
    - OutOfMemoryError (6 occurrences)
```

**compare_logs 边界场景：**

| 边界场景 | 处理方式 |
|---------|---------|
| 旧时间段日志已轮转（kubelet 已清除） | 返回 "Period 2 logs unavailable (likely rotated)" + 仅展示 Period 1 统计 |
| 两个时间段日志量差异极大（>10x） | 在 delta 区域注明 "volume difference may skew comparison" |
| Pod 副本数在两个时间段不同 | 统计中标注各时段的 Pod 数量，提示"注意副本数变化" |
| 目标服务在旧时间段不存在 | 返回 "Service not found in Period 2 timeframe" |

### 3.2 对话历史持久化

**存储引擎：** modernc.org/sqlite（纯 Go，无 CGO，保持单二进制零依赖）

**数据库路径：** `~/.korthex/history.db`

**Schema 设计：**

```sql
-- 对话会话
CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,  -- UUID
    cluster     TEXT NOT NULL,     -- K8s context name
    started_at  DATETIME NOT NULL,
    ended_at    DATETIME,
    summary     TEXT               -- AI 自动生成的会话摘要
);

-- 对话消息（含 tool calls）
CREATE TABLE messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    role        TEXT NOT NULL,     -- user | assistant | tool_call | tool_result
    content     TEXT NOT NULL,     -- 消息文本或 tool result 摘要
    namespace   TEXT,              -- 关联的 namespace（用于搜索）
    created_at  DATETIME NOT NULL
);

-- 全文搜索索引
CREATE VIRTUAL TABLE messages_fts USING fts5(content, namespace);

-- 索引
CREATE INDEX idx_messages_session ON messages(session_id);
CREATE INDEX idx_messages_created ON messages(created_at);
CREATE INDEX idx_sessions_cluster ON sessions(cluster);
```

**会话生命周期：**
1. Korthex 启动时创建新 session（`INSERT INTO sessions`）
2. 每次用户输入和 AI 回复实时写入 messages 表
3. Tool 调用结果超过 2000 字符时，存储压缩版本（前 500 字符 + 统计摘要）
4. Korthex 退出时更新 session 的 `ended_at`，并让 AI 生成 1-2 句会话摘要
5. 后台定时任务：每次启动时清理超过 `retention_days` 的过期记录

**用户交互：**
- AI Chat 中输入 `/history` 打开历史搜索覆盖层
- 搜索界面：

```
┌─ Conversation History ──────────────────────────────────┐
│                                                          │
│  Search: timeout_____                                    │
│                                                          │
│  ● 2026-04-24 14:30  [production]                       │
│    "查 order-service 的 timeout 错误"                    │
│    → 找到 18 条 ConnectionTimeout，根因是连接池耗尽        │
│                                                          │
│  ● 2026-04-23 09:15  [staging]                          │
│    "staging 的 payment-service timeout"                   │
│    → 数据库慢查询导致，已建议添加索引                       │
│                                                          │
│  [Enter] load context  [Esc] close                       │
└──────────────────────────────────────────────────────────┘
```

- 支持搜索方式：
  - 关键词搜索：直接输入关键词，FTS5 全文匹配
  - 自然语言搜索："之前那个 timeout 的问题" → 提取关键词 → FTS5
  - 按 namespace 过滤：`ns:production timeout`
  - 按时间过滤：`after:2026-04-20 error`
- 选择历史会话后，将该会话的摘要注入当前对话的上下文中：
  ```
  [Previous context from 2026-04-24 14:30]
  User asked about order-service timeout errors in production.
  Found 18 ConnectionTimeout errors caused by connection pool exhaustion.
  ```
- 配置项：`history.retention_days: 30`（默认保留 30 天，启动时自动清理过期记录）

**Tool 调用结果压缩（纳入持久化层）：**
- 复用 Phase 1 已有的 `compressToolResult()` 逻辑
- 压缩规则：日志类 tool 结果 > 2000 字符 → 保留前 500 字符 + 统计信息（行数、ERROR/WARN 数量）
- 原始结果不存入 messages 表
- 被压缩的结果标记 `[compressed]`，不影响 AI 后续对话

### 3.3 跨 Service 日志关联

**核心能力：** AI 自动识别日志中的 trace ID / request ID，跨 Pod 和 Service 关联完整调用链。

**Trace ID 识别策略：**

内置主流 trace ID 格式正则：

| 格式 | 正则 | 示例 |
|------|------|------|
| OpenTelemetry traceId | `trace[-_]?id[=:]\s*([0-9a-f]{32})` | `traceId=4bf92f3577b34da6a3ce929d0e0e4736` |
| AWS X-Ray | `Root=1-[0-9a-f]{8}-[0-9a-f]{24}` | `Root=1-5e19a1f6-7bce4b8f3e5c2a9d1b4f6e8a` |
| 通用 request-id | `request[-_]id[=:]\s*([a-zA-Z0-9-]+)` | `request-id: abc-123-def` |
| UUID 格式 | `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}` | `550e8400-e29b-41d4-a716-446655440000` |

用户可在 config.yaml 中添加自定义 pattern：

```yaml
analysis:
  trace_id_patterns:
    - name: "custom-correlation"
      pattern: "correlationId=([\\w-]+)"
    - name: "internal-trace"
      pattern: "x-trace-id:\\s*([A-Z0-9]{16})"
```

**工作流：**

```
用户: "帮我追踪这个请求 abc-123-def"
      │
      ▼
AI 识别为 trace/request ID
      │
      ▼
调用 trace_logs 工具
      │
      ├── 遍历相关 namespace（AI 根据上下文推断，或搜索全部）
      │
      ├── 每个 namespace 中按 grep 搜索 trace ID
      │     （并发 goroutine，受信号量限制）
      │
      ├── 按时间线归并排序
      │
      ▼
Log Viewer 展示完整调用链（每行标注来源 Service + Pod）
Chat 展示时间线摘要：
  14:23:01  api-gateway     → 收到请求
  14:23:02  order-service   → 处理订单
  14:23:05  payment-service → 支付超时
  14:23:06  order-service   → 回滚事务
```

**新增 Agent Tool：**

| Tool | 功能 | 参数 |
|------|------|------|
| `trace_logs` | 按 trace/request ID 跨 namespace 搜索 | `traceId` (必填), `namespaces` (可选, 逗号分隔, 默认搜索全部), `timeRange` (可选, 默认 1h) |

**trace_logs 工具输出格式：**

```
Trace ID: abc-123-def
Searched: 5 namespaces, 23 pods
Found: 4 services, 12 log lines

Timeline:
  [14:23:01] api-gateway/pod-abc12       INFO  Received POST /orders
  [14:23:02] order-service/pod-def34     INFO  Processing order #5678
  [14:23:03] order-service/pod-def34     INFO  Calling payment-service
  [14:23:05] payment-service/pod-ghi56   ERROR ConnectionTimeout: database
  [14:23:05] order-service/pod-def34     ERROR Payment failed: timeout
  [14:23:06] order-service/pod-def34     INFO  Rolling back transaction
  ...
```

**性能约束：**
- 最多并发搜索 10 个 namespace（信号量限制）
- 单 namespace 搜索超时 30 秒
- 总搜索超时 60 秒
- 搜索结果上限 5000 行（超出提示缩小范围）

### 3.4 AI Chat Markdown 渲染

**目标：** Chat 面板中的 AI 回复从原始 Markdown 文本升级为终端渲染后的富文本，支持颜色、层级、代码高亮。

**渲染范围：**

| Markdown 元素 | 渲染效果 |
|---------------|---------|
| `# 标题` / `## 副标题` | 粗体 + 颜色区分层级（H1 亮白粗体，H2 蓝色粗体，H3 绿色粗体） |
| `**粗体**` | Lipgloss Bold |
| `*斜体*` | Lipgloss Italic (终端支持时) |
| `` `inline code` `` | 灰底 + 高亮前景色 |
| ` ```代码块``` ` | 左侧竖线 + 缩进 + 语法高亮底色 |
| `- 列表` / `1. 有序列表` | 缩进 + bullet/数字前缀 |
| `> 引用` | 左侧竖线 + 灰色文字 |
| `| 表格 |` | 对齐渲染 + 分隔线 |
| `---` 分隔线 | 全宽横线字符 |
| `[链接](url)` | 蓝色下划线文字 |

**技术方案：**
- 使用 **charmbracelet/glamour** 库（Charm 生态的 Markdown 终端渲染器）
- glamour 底层用 goldmark 解析 Markdown AST，再用 Lipgloss 渲染样式
- 自定义 glamour `StyleConfig` 匹配 Korthex 的主题系统（dark/light/dracula/nord）

> **已知限制：glamour 不支持增量/流式渲染。** Partial markdown（未闭合的代码块、表格等）会导致渲染错误。因此采用"完成后一次性渲染"策略，不在流式期间调用 glamour。这是目前 Bubble Tea + LLM 项目的通用做法。

**主题适配：**

```go
// 每个 Korthex 主题对应一套 glamour StyleConfig
var themeStyles = map[string]glamour.TermRendererOption{
    "dark":    glamour.WithAutoStyle(),       // glamour 默认暗色
    "light":   glamour.WithStylePath("light"), // glamour 内置亮色
    "dracula": glamour.WithStylesFromJSONFile("dracula.json"),  // 自定义
    "nord":    glamour.WithStylesFromJSONFile("nord.json"),     // 自定义
}
```

**渲染时机（已确认：完成后一次性渲染）：**
- glamour 不支持 partial/streaming markdown 渲染，因此**不在流式期间调用 glamour**
- 流式输出期间：显示原始文本 + spinner 动画（与 Phase 1 行为一致）
- 流式完成后（`EventComplete`）：在 `tea.Cmd` 中异步调用 `glamour.Render()`，完成后 `p.Send(MarkdownRenderedMsg{})` 替换原始文本
- 已完成的历史消息始终显示渲染后版本
- 每条消息缓存渲染结果，避免 `View()` 重复渲染（仅在窗口 resize 时重新渲染）

**边界处理：**
- 用户输入（`You:` 部分）不做 Markdown 渲染，保持原文
- Tool 调用展示（`$ kubectl logs ...`）保持当前的等宽样式，不经过 glamour
- 超宽表格自动截断到面板宽度
- 渲染失败时（malformed markdown）降级显示原始文本，不中断

**性能要求：**
- 单条消息渲染 < 50ms
- 渲染不阻塞 TUI 主循环（在 `tea.Cmd` 中异步执行，完成后 `p.Send` 更新）

---

## 4. Feature Specification — Enhanced Browser

### 4.1 Resource Browser 资源类型扩展

**目标：** Resource Browser 从仅支持 Deployment 扩展到支持 StatefulSet、DaemonSet、Job、CronJob，覆盖绝大多数生产工作负载类型。

**扩展后的层级导航：**

```
Cluster
  └── Namespaces (列表, 可搜索)
       ├── Deployments (列表, 显示 READY/UP-TO-DATE/AVAILABLE)
       ├── StatefulSets (列表, 显示 READY/AGE)
       ├── DaemonSets (列表, 显示 DESIRED/CURRENT/READY)
       ├── Jobs (列表, 显示 COMPLETIONS/DURATION)
       └── CronJobs (列表, 显示 SCHEDULE/LAST SCHEDULE/ACTIVE)
            └── Pods (列表, 显示 STATUS/RESTARTS/AGE)
                 └── Containers
                      └── Logs
```

**Namespace 下的资源类型切换：**
- 进入 Namespace 后，默认展示 Deployments（与 Phase 1 一致）
- 按 `1`-`5` 数字键切换资源类型：

| 键 | 资源类型 |
|----|---------|
| `1` | Deployments (默认) |
| `2` | StatefulSets |
| `3` | DaemonSets |
| `4` | Jobs |
| `5` | CronJobs |

- Header 显示当前资源类型和数量：`production > Deployments (23)`
- 按 `/` 搜索时匹配当前选中的资源类型
- `Esc` 返回上一层（Namespace 列表），资源类型选择重置为 Deployments

**列表展示字段：**

| 资源类型 | 列 |
|---------|-----|
| **Deployment** | NAME, READY (x/y), UP-TO-DATE, AVAILABLE, AGE |
| **StatefulSet** | NAME, READY (x/y), AGE |
| **DaemonSet** | NAME, DESIRED, CURRENT, READY, UP-TO-DATE, AVAILABLE, AGE |
| **Job** | NAME, COMPLETIONS (x/y), DURATION, AGE |
| **CronJob** | NAME, SCHEDULE, SUSPEND, ACTIVE, LAST SCHEDULE, AGE |

**K8s Client 扩展（借鉴 k9s DAO Registry 模式）：**

> **架构借鉴：** k9s 使用 `AccessorFor()` 注册器 + 泛型 fallback 来处理多资源类型。Korthex Phase 2 应采用类似的 **Resource Registry** 模式，而非在 UI/Agent 中硬编码 switch-case。每种资源类型注册一个 `ResourceAccessor`，UI 和 Agent 通过 registry 查询，新增资源类型无需改动调用方代码。

```go
// internal/k8s/registry.go — 资源类型注册器
type ResourceAccessor interface {
    List(namespace string, filter string) ([]ResourceItem, error)
    GetLabelSelector(namespace, name string) (string, error)
}

var registry = map[string]ResourceAccessor{
    "deployments":  &DeploymentAccessor{},
    "statefulsets": &StatefulSetAccessor{},
    "daemonsets":   &DaemonSetAccessor{},
    "jobs":         &JobAccessor{},
    "cronjobs":     &CronJobAccessor{},
}

func AccessorFor(resourceType string) (ResourceAccessor, bool)
```

- 每种资源类型实现 `ResourceAccessor` 接口，内部复用 Informer Cache
- 新增对应的 Informer：
  - `AppsV1().StatefulSets()`
  - `AppsV1().DaemonSets()`
  - `BatchV1().Jobs()`
  - `BatchV1().CronJobs()`
- Informer 按需启动：用户首次切换到某资源类型时才启动对应 Informer
- Job/CronJob 的 Pod 关联通过 `job-name` label 自动追踪
- StatefulSet 的 Pod 有序展示（按 ordinal index 排序：`<name>-0`, `<name>-1`, ...）
- DaemonSet 的 Pod 按 Node 名排序

**Job/CronJob 特殊处理：**
- Job 列表默认只显示最近 24 小时的 Job（过滤掉已完成的旧 Job，避免列表过长）
- CronJob → 选中后进入其管理的 Job 列表 → Job → Pod → Logs
- 已完成 Job 的 Pod 可能已被 GC，此时显示 "Pod terminated, use --previous for last logs"

### 4.2 Pod 详情面板

**目标：** 在 Pod 列表中提供结构化的 Pod 详情查看，替代 Phase 1 的纯 describe 文本输出。

**触发方式：** 在 Pod 列表中选中 Pod 后按 `d`（复用 Phase 1 describe 快捷键）

**面板布局（覆盖层）：**

```
┌─ Pod: order-svc-7d4f8-abc12 ─────────────────────────────┐
│                                                            │
│  Status: Running      Node: worker-03                      │
│  IP: 10.244.1.15      Started: 2h ago                      │
│  Restarts: 0          QoS: Burstable                       │
│                                                            │
│ ── Containers ──────────────────────────────────────────── │
│  NAME        STATUS    CPU          MEMORY                  │
│  order-api   Running   250m/500m    128Mi/256Mi             │
│  sidecar     Running   50m/100m     32Mi/64Mi               │
│                                                            │
│ ── Conditions ──────────────────────────────────────────── │
│  ✓ PodScheduled   ✓ Initialized   ✓ ContainersReady       │
│  ✓ Ready                                                   │
│                                                            │
│ ── Recent Events (last 10) ────────────────────────────── │
│  2h ago  Normal   Pulled     Successfully pulled image     │
│  2h ago  Normal   Created    Created container order-api   │
│  2h ago  Normal   Started    Started container order-api   │
│                                                            │
│ [Esc] back  [l] logs  [y] copy YAML  [j/k] scroll         │
└────────────────────────────────────────────────────────────┘
```

**数据来源：**

| 区域 | 数据来源 | 缓存策略 |
|------|---------|---------|
| Pod 基本信息 | Informer Cache | 实时（Watch 更新） |
| Container 状态 | Pod.Status.ContainerStatuses | 实时 |
| CPU/Memory 用量 | Metrics API (`metrics.k8s.io`) | 按需拉取，缓存 30s |
| Conditions | Pod.Status.Conditions | 实时 |
| Events | Core API Events | 按需拉取，缓存 60s |

**Metrics API 优雅降级：**
- 启动时查询 API Server 检查 `metrics.k8s.io` 是否可用：
  ```go
  _, err := discoveryClient.ServerResourcesForGroupVersion("metrics.k8s.io/v1beta1")
  ```
- 可用时：显示实际 CPU/Memory 用量 (`250m/500m`)
- 不可用时：CPU/Memory 列显示 `-/500m`（仅显示 limit）
- StatusBar 提示："Metrics API not available, resource usage not shown"

**面板交互：**
- `j`/`k` 或 `↑`/`↓`：在面板内滚动（内容超出面板高度时）
- `l`：直接查看该 Pod 的日志（跳转到 Log Viewer）
- `y`：复制 Pod YAML 到剪贴板
- `Esc`：关闭面板，返回 Pod 列表

### 4.3 AI Agent 对新资源类型的感知

**目标：** AI Agent 自动感知新增的资源类型，用户可以用自然语言查询所有工作负载类型的日志。

**新增 Agent Tools：**

| Tool | 功能 | 参数 |
|------|------|------|
| `kubectl_get_statefulsets` | 列出 StatefulSet | `namespace`, `filter` (可选) |
| `kubectl_get_daemonsets` | 列出 DaemonSet | `namespace`, `filter` (可选) |
| `kubectl_get_jobs` | 列出 Job | `namespace`, `filter` (可选), `activeOnly` (可选) |
| `kubectl_get_cronjobs` | 列出 CronJob | `namespace`, `filter` (可选) |

**System Prompt 更新 — 扩展的资源发现策略：**

```
当用户提到一个服务名时，按以下顺序搜索：
1. Deployment（最常见）
2. StatefulSet（数据库、消息队列等有状态服务）
3. DaemonSet（日志收集器、监控 agent 等）
4. Job/CronJob（批处理任务、定时任务）

找到匹配的资源后，提取 label selector 查询日志。
如果多种资源类型匹配，列出候选让用户选择。
```

**特殊场景处理：**

| 场景 | 用户输入 | AI 行为 |
|------|---------|---------|
| StatefulSet 日志 | "查 redis-cluster 的日志" | 识别为 StatefulSet → 提取 selector → 按 ordinal 顺序展示各 Pod 日志 |
| DaemonSet 日志 | "node-exporter 在 worker-03 上有报错吗" | 识别为 DaemonSet → 按 Node 名过滤 Pod → 查日志 |
| Job 日志 | "最近那个 migration job 成功了吗" | 查找最近的 Job → 检查 completions → 拉日志 |
| CronJob 日志 | "昨天凌晨的 backup 有没有报错" | 查找 CronJob → 找到对应时间的 Job → Pod → 日志 |
| 失败 Job | "为什么 data-sync job 失败了" | 查找 Job → 发现 Failed 状态 → 自动 `--previous` 拉日志 |

---

## 5. Feature Specification — Data Safety

### 5.1 正则脱敏规则

**目标：** 日志内容发送给 LLM 前自动遮盖敏感信息，降低数据泄露风险。Log Viewer 展示原始内容不受影响。

**脱敏边界：**
- **仅在发送给 LLM 前脱敏** — Log Viewer 展示、日志导出、用户复制均保持原文
- 脱敏管道位于 Agent 层：tool result → 脱敏引擎 → 注入 LLM 消息
- `send_logs: false` 时脱敏引擎不激活（日志根本不发给 LLM）

**内置默认规则：**

| 规则名 | 匹配目标 | 正则 | 替换为 |
|--------|---------|------|--------|
| `jwt` | JWT Token | `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}` | `[JWT_REDACTED]` |
| `api_key` | API Key / Token / Secret | `(?i)(api[_-]?key\|token\|secret\|password)[=:]\s*['"]?[A-Za-z0-9_-]{16,}` | `$1=[REDACTED]` |
| `email` | 电子邮件 | `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}` | `[EMAIL_REDACTED]` |
| `ipv4` | IPv4 地址 | `\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b` | `[IP_REDACTED]` |
| `credit_card` | 信用卡号 | `\b\d{4}[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}\b` | `[CC_REDACTED]` |
| `bearer_token` | Bearer Token | `(?i)Bearer\s+[A-Za-z0-9_-]{20,}` | `Bearer [TOKEN_REDACTED]` |

**用户自定义规则：**

```yaml
# config.yaml
privacy:
  redaction:
    enabled: true            # 全局开关，默认 true
    rules:                   # 自定义规则（追加到内置规则之后）
      - name: "internal-id"
        pattern: "internal-user-id=([A-Z0-9]{8})"
        replacement: "internal-user-id=[REDACTED]"
      - name: "ssn"
        pattern: "\\b\\d{3}-\\d{2}-\\d{4}\\b"
        replacement: "[SSN_REDACTED]"
    disable_builtin:         # 可选：禁用特定内置规则
      - ipv4                 # 内部 IP 不需要脱敏
```

**脱敏引擎接口设计：**

> **自建决策：** Go 生态无通用日志脱敏库。cockroachdb/redact 是遥测专用（safe/unsafe 标记模型），masq 是 slog 专用。Korthex 需自建 `pkg/redact` 模块，但可借鉴 cockroachdb/redact 的 safe/unsafe 标记思想 — 在脱敏统计中区分"确认脱敏"和"疑似误匹配"。

```go
// pkg/redact/redact.go (新模块，Layer 0)

type Rule struct {
    Name        string
    Pattern     *regexp.Regexp
    Replacement string
}

type Engine struct {
    rules []Rule
}

func NewEngine(cfg config.RedactionConfig) *Engine
func (e *Engine) Redact(input []byte) (output []byte, stats RedactStats)

type RedactStats struct {
    TotalMatches int
    ByRule       map[string]int  // rule name → match count
}
```

**AI 感知：**
- System Prompt 中注明："日志内容可能包含 `[REDACTED]`/`[JWT_REDACTED]` 等标记，表示敏感信息已脱敏。分析时请忽略被脱敏的值，不要尝试推测原始内容。"
- 脱敏统计信息附加在 tool result 末尾：`[Redaction: 3 JWT, 2 email, 1 API key masked]`
- AI 的分析不应因脱敏而降级 — 聚焦错误模式、时间线、severity 等非敏感维度

**性能要求：**
- 所有正则启动时一次性编译为 `*regexp.Regexp`
- 脱敏函数接受 `[]byte`，使用 `regexp.ReplaceAll` 链式处理
- 基准目标：1MB 日志文本脱敏 < 10ms
- 规则数量建议 < 20 条（超出时启动警告 "Too many redaction rules may impact performance"）

### 5.2 日志书签 (Bookmark)

**目标：** 用户在 Log Viewer 中标记重要日志行，方便在大量日志中快速定位关键位置。

**交互设计：**

| 快捷键 | 功能 | 上下文 |
|--------|------|--------|
| `m` | 标记/取消标记当前光标行 | Log Viewer |
| `'` (单引号) | 打开书签列表覆盖层 | Log Viewer |
| `n` | 跳转到下一个书签 | Log Viewer |
| `N` | 跳转到上一个书签 | Log Viewer |

**书签视觉效果：**
- 被标记的行在行号区域显示 `▸` 标记符号（替代行号数字）
- 书签行背景微弱高亮（Lipgloss `Background` 暗色偏移）
- Footer 显示书签数量：`[3 bookmarks]`

**书签列表覆盖层：**

```
┌─ Bookmarks (3) ────────────────────────────────────────┐
│                                                         │
│  #1  L.234   [14:23:05] ERROR NullPointerException...  │
│  #2  L.567   [14:25:12] ERROR ConnectionTimeout...     │
│  #3  L.891   [14:30:01] WARN  Memory usage > 80%       │
│                                                         │
│  [Enter] jump  [d] delete  [Esc] close                 │
└─────────────────────────────────────────────────────────┘
```

**实现细节：**
- 书签数据结构：`[]BookmarkEntry`
  ```go
  type BookmarkEntry struct {
      LineIndex int       // Ring Buffer 中的行索引
      Preview   string    // 行内容预览（截取前 80 字符）
      CreatedAt time.Time
  }
  ```
- 存储在 LogViewer Model 中，不持久化（与 Ring Buffer 生命周期一致）
- Ring Buffer 淘汰旧行时，`LineIndex` 前移，落入淘汰范围的书签自动删除
- 最多 50 个书签（超出时 StatusBar 提示 "Max bookmarks reached (50)"）

**与 AI 分析的联动：**
- AI 分析结果中标注关键日志行时，可自动为这些行添加书签
- 新增 agent tool：

| Tool | 功能 | 参数 |
|------|------|------|
| `bookmark_log_lines` | AI 分析后主动标记重要日志行 | `lineIndices` (int 数组) |

- Chat 面板提示："已为 3 条关键日志添加书签，按 `'` 查看"
- 用户可手动删除 AI 自动添加的书签（`'` → `d`）

---

## 6. Non-Functional Requirements (Phase 2 增量)

| 需求 | Phase 1 指标 | Phase 2 指标 | 说明 |
|------|-------------|-------------|------|
| **Markdown 渲染延迟** | N/A | < 50ms (单条消息) | glamour 渲染不能让流式输出产生可感知的卡顿 |
| **脱敏吞吐** | N/A | 1MB 日志 < 10ms | 正则引擎不能成为日志管道的瓶颈 |
| **对话历史查询** | N/A | FTS 搜索 < 200ms (10000 条消息) | modernc.org/sqlite FTS5 |
| **数据库大小** | N/A | 30 天历史 < 50MB | 自动清理 + tool result 压缩 |
| **新资源类型列表** | N/A | 与 Deployment 一致 (< 500ms) | 复用 Informer Cache |
| **Metrics API 降级** | N/A | 无 Metrics Server 时 Pod 详情面板 < 1s | 优雅降级不阻塞 |
| **跨 Service 追踪** | N/A | 5 namespace 内 grep < 10s | 并发 goroutine，信号量限制 |
| **书签操作** | N/A | 添加/删除/跳转 < 16ms | 纯内存操作，60fps |
| **内存占用** | < 300MB | < 350MB (含 SQLite + 脱敏 + glamour) | 增量 < 50MB |
| **启动时间** | < 2s | < 2.5s (含 SQLite init + 正则编译) | 增量 < 500ms |

---

## 7. Success Metrics (Phase 2)

| 指标 | 目标 | 度量方式 |
|------|------|---------|
| AI 分析深度 | 分析回复包含结构化"发现→原因→建议"（> 80%） | 20 条预设分析 query 评估，检查输出三段式结构 |
| 跨 Service 追踪成功率 | trace ID 关联到 ≥ 2 个 Service（> 70%） | 含 OpenTelemetry 的测试集群，10 个 trace ID 测试 |
| 脱敏覆盖率 | 内置规则覆盖 > 90% 常见敏感信息类型 | 包含已知敏感信息的测试日志集验证 |
| 历史搜索可用性 | 3 步内找到历史对话 | `/history` → 关键词 → 选择会话 |
| Markdown 渲染质量 | 标题/列表/代码块/表格正确渲染 > 95% | 人工审核 20 条不同格式的 AI 回复 |
| 资源类型覆盖率 | 新增 4 种资源类型全部可浏览和查日志 | 在测试集群中逐一验证 StatefulSet/DaemonSet/Job/CronJob |

---

## 8. Decisions (Phase 2 已确认决策)

| # | 决策 | 理由 |
|---|------|------|
| P2-D1 | Ollama 支持延后到 Phase 3/4 | PRD 已标注"往后移"，Phase 2 聚焦云端 LLM 的分析深度 |
| P2-D2 | 日志可视化（热力图/柱状图）不纳入 Phase 2 | 用户决策，推迟到 Phase 3 |
| P2-D3 | 对话持久化使用 modernc.org/sqlite | 纯 Go，无 CGO，保持单二进制零依赖原则 |
| P2-D4 | 脱敏仅在 LLM 发送前执行 | Log Viewer 展示原文，安全边界清晰 |
| P2-D5 | Markdown 渲染使用 charmbracelet/glamour | 与 Bubble Tea 同生态，风格统一 |
| P2-D6 | 跨 Service 关联采用 AI 智能方式 | AI 自动识别 trace ID 格式，推断关联 service |
| P2-D7 | 书签不持久化 | 与 Ring Buffer 生命周期一致，简化实现 |
| P2-D8 | Pod 详情面板 Metrics 优雅降级 | 不强依赖 Metrics Server，无则显示 `-` |

---

## 9. Configuration (Phase 2 新增配置项)

```yaml
# config.yaml Phase 2 新增项

privacy:
  redaction:
    enabled: true              # 脱敏全局开关，默认 true
    rules: []                  # 用户自定义脱敏规则
    disable_builtin: []        # 禁用的内置规则名列表

history:
  enabled: true                # 对话历史持久化开关
  retention_days: 30           # 历史保留天数
  db_path: "~/.korthex/history.db"  # 数据库路径

analysis:
  trace_id_patterns: []        # 用户自定义 trace ID 正则
```

**与 Phase 1 配置项的关系：**
- Phase 1 的所有配置项保持不变
- Phase 2 新增项均有合理默认值，Phase 1 → Phase 2 升级无需修改 config.yaml
- `privacy.redaction.enabled: true` 默认开启脱敏（安全优先）
- `history.enabled: true` 默认开启历史持久化

---

## 10. New Dependencies (Phase 2 新增依赖)

| 依赖 | License | 用途 | 纯 Go |
|------|---------|------|-------|
| modernc.org/sqlite | BSD-3-Clause | 对话历史持久化 (SQLite) | ✓ |
| charmbracelet/glamour | MIT | Markdown 终端渲染 | ✓ |

**License 兼容性：** BSD-3-Clause 和 MIT 均与 Apache-2.0 兼容。需更新 `NOTICE` 文件。

---

## 11. Keyboard Shortcuts (Phase 2 新增/变更)

| Key | Action | Context | Phase |
|-----|--------|---------|-------|
| `1`-`5` | 切换资源类型 (Deploy/SS/DS/Job/CJ) | Resource Browser (Namespace 层) | Phase 2 |
| `m` | 添加/删除书签 | Log Viewer | Phase 2 |
| `'` | 打开书签列表 | Log Viewer | Phase 2 |
| `n` | 跳转到下一个书签 | Log Viewer | Phase 2 |
| `N` | 跳转到上一个书签 | Log Viewer | Phase 2 |
| `/history` | 打开历史搜索 | AI Chat | Phase 2 |

**Phase 1 快捷键保持不变。**

---

## 12. Module Impact Analysis (模块影响分析)

Phase 2 功能对现有模块的影响评估：

| 模块 | 变更类型 | 具体变更 |
|------|---------|---------|
| **config** | 扩展 | 新增 `privacy`/`history`/`analysis` 配置段 |
| **pkg/logparse** | 不变 | 无需修改 |
| **pkg/redact** (新) | 新增 | 脱敏引擎，Layer 0 |
| **internal/k8s** | 扩展 | 新增 StatefulSet/DaemonSet/Job/CronJob Informer + Metrics API client |
| **internal/llm** | 不变 | Provider 层无需修改 |
| **internal/agent** | 扩展 | 新增 7 个 tools + system prompt 增强 + 脱敏管道集成 |
| **internal/history** (新) | 新增 | SQLite 持久化层，Layer 1 |
| **internal/ui** | 扩展 | Markdown 渲染 + 书签 + Pod 详情面板 + 资源类型切换 + 历史搜索覆盖层 |
| **internal/app** | 扩展 | 新模块依赖注入 |

**新增模块的层级定位：**

```
Layer 0 (Foundation):   config, pkg/logparse, pkg/redact (新)
Layer 1 (Core):         k8s, llm, internal/history (新)
Layer 2 (Intelligence): agent ← imports history, redact
Layer 3 (Presentation): ui ← imports history (历史搜索 UI)
Layer 4 (Integration):  app
```

---

## 13. Implementation Difficulty Assessment (实现难度评估)

基于对现有代码库的分析和外部依赖调研，各功能的实现难度评估：

| 功能 | 难度 | 代码库现有支撑 | 外部依赖风险 | 关键文件 |
|------|------|---------------|-------------|---------|
| **Resource Browser 扩展** | 低 | `resources.go` Deployment lister 可直接复制。Informer Factory 已支持所有资源类型 | 无 | `k8s/resources.go`, `k8s/types.go` |
| **新增 Agent Tools** (list 类) | 低 | `tools.go` switch dispatch，加 case 即可。`safety.go` 白名单追加 | 无 | `agent/tools.go:27-149` |
| **System Prompt 增强** | 低 | `prompt.go:8-127` 纯字符串拼接 | 无 | `agent/prompt.go` |
| **正则脱敏** | 低-中 | 插入点明确：tool result → 脱敏 → LLM 历史。Go 生态无现成库，需自建 | 无外部依赖 | 新建 `pkg/redact/` |
| **日志书签** | 中 | 覆盖层可复用 describe overlay 模式 (`resource.go:446-466`)。Ring Buffer 需扩展 LineIndex | 无 | `ui/logviewer.go`, `ui/ringbuffer.go` |
| **Pod 详情面板** | 中 | 覆盖层模式已有。Metrics API 需 discovery client 探测 + 降级 | metrics.k8s.io 可选 | `ui/resource.go`, `k8s/client.go` |
| **对话历史持久化** | 中 | 新增模块，Schema/CRUD/FTS 中等工作量 | modernc.org/sqlite FTS5 已确认可用 | 新建 `internal/history/` |
| **Markdown 渲染** | 中-高 | `chat.go:443-479` renderMessages 需重构。glamour 不支持流式渲染，需完成后一次性渲染 + 缓存 | glamour 成熟但有流式限制 | `ui/chat.go` |
| **跨 Service trace 关联** | 高 | 并发跨 namespace grep 需信号量 + 超时 + 错误聚合 + 归并排序 | 无，但实现复杂 | `agent/tools.go`, `k8s/logs.go` |
| **AI 分析增强** | 高 | 工具实现中等，核心难度在 Prompt Engineering 和输出质量调优 | LLM 输出质量不可控 | `agent/prompt.go`, `agent/tools.go` |

### 借鉴已有工作总结

| 借鉴来源 | 借鉴内容 | 应用于 |
|---------|---------|--------|
| **k9s** `internal/dao/` | DAO Registry + `AccessorFor()` 模式，资源类型注册器 + 泛型 fallback | Resource Browser 扩展架构 |
| **k9s** `internal/view/` | Pod 详情面板的信息分区设计（Status/Containers/Conditions/Events） | Pod 详情面板布局 |
| **cockroachdb/redact** | safe/unsafe 标记思想，区分确认脱敏和疑似误匹配 | 脱敏引擎 API 设计 |
| **Gonzo** | 验证 Bubble Tea + AI 日志分析的技术可行性；OTLP 数据源模式（Phase 4 参考） | 架构验证 |
| **Korthex Phase 1** | describe overlay 模式 → 书签/Pod 详情覆盖层；switch dispatch → 新 tools；Informer 模式 → 新资源类型 | 全面复用 |
| **glamour** GitHub issues | 确认不支持流式渲染，需"完成后一次性渲染"策略 | Markdown 渲染时机设计 |
| **modernc.org/sqlite** docs | 确认 FTS5 可用，纯 Go 无 CGO | 对话历史持久化引擎选型 |

---

## Appendix A: Agent Tools Summary (Phase 2 完整列表)

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

### Phase 2 新增 Tools

| Tool | 功能 | 能力域 |
|------|------|--------|
| `kubectl_get_statefulsets` | 列出 StatefulSets | Enhanced Browser |
| `kubectl_get_daemonsets` | 列出 DaemonSets | Enhanced Browser |
| `kubectl_get_jobs` | 列出 Jobs | Enhanced Browser |
| `kubectl_get_cronjobs` | 列出 CronJobs | Enhanced Browser |
| `severity_stats` | 统计 severity 分布 | Deep Analysis |
| `compare_logs` | 对比两个时间段日志 | Deep Analysis |
| `get_pod_metrics` | 获取 Pod 资源用量 | Deep Analysis |
| `trace_logs` | 跨 Service trace ID 搜索 | Deep Analysis |
| `bookmark_log_lines` | AI 标记重要日志行 | Data Safety |
