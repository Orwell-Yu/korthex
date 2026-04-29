# internal/agent — CLAUDE.md

> AI Agent: orchestrate the agentic loop — natural language → tool calls → K8s execution → LLM feedback → iterate.

## Parent Reference

- **When confused about architecture:** Read [`../../CLAUDE.md`](../../CLAUDE.md) (root project rules, especially Rule #5: Phase 1 只读, Rule #6: 接口优先)
- **When confused about agentic loop design:** Read [`../../SPEC.md`](../../SPEC.md) Section 4.1 (Agentic Loop 算法)
- **When confused about label selector discovery:** Read [`../../SPEC.md`](../../SPEC.md) Section 4.2
- **When confused about tool definitions:** Read [`../../SPEC.md`](../../SPEC.md) Section 3.7 (Phase 1 Tool Definitions)
- **When confused about AI 安全机制:** Read [`../../PRD.md`](../../PRD.md) Section 5.3 (AI 安全机制) 和 Section 10.1 (命令安全分级)
- **When confused about conversation context:** Read [`../../PRD.md`](../../PRD.md) Section 10.7 (AI Chat 上下文策略)
- **When confused about LLM types:** Read [`../llm/CLAUDE.md`](../llm/CLAUDE.md) 和 [`../llm/README.md`](../llm/README.md)
- **When confused about K8s operations:** Read [`../k8s/CLAUDE.md`](../k8s/CLAUDE.md) 和 [`../k8s/README.md`](../k8s/README.md)

## Module Rules

| # | Rule | Reason |
|---|------|--------|
| 1 | **Layer 2: 可以 import config, k8s, llm, logparse, history, redact** | 依赖 Layer 0 和 Layer 1，不 import ui |
| 2 | **Phase 1 安全白名单** — SafetyChecker.Check() 只返回 `SafetyAllowed` (7 个只读 tool) 或 `SafetyDenied`。Phase 3 增加 `SafetyDangerous` / `SafetyCritical` 无需改接口 | Root CLAUDE.md Rule #5 |
| 3 | **maxIterations 限制** — agentic loop 最多迭代 `config.Agent.MaxIterations` 次 (-1 = 无限, default -1) | 用户可配置正整数限制迭代次数 |
| 4 | **Tool result 压缩** — 超过 2000 字符的 tool result 在历史中截断为 500 + 统计摘要 | 避免历史对话吃掉 context window |
| 5 | **send_logs 配置尊重** — 当 `send_logs=false` 时，tool result 只包含执行状态 (成功/失败/行数)，不包含日志原文 | 数据安全: 用户可能不想发日志给 LLM Provider |
| 6 | **Error 作为 tool result** — 执行错误以 "ERROR: ..." 格式作为 tool result 发回 LLM，让 LLM 自纠 | 标准 agentic 模式，LLM 能根据错误信息修正下一次调用 |
| 7 | **CommandDisplay 必须生成** — 每个 tool call 必须生成人类可读的 kubectl 等价命令字符串 | UI 需要展示给用户看 "AI 做了什么" |
| 8 | **LLM 调用重试** — `ErrRateLimit`/`ErrTimeout` 时自动重试 1 次（指数退避），`ErrAuth`/`ErrModelUnavailable` 不重试 | PRD §6.7 承诺"自动重试 1 次"。llm adapter 只分类错误不重试，agent 层兑现此承诺 |
| 9 | **脱敏管道** — 所有含日志内容的 tool result 在追加到 LLM 消息历史前必须经过 `pkg/redact` Engine。脱敏统计以 `[Redaction: N items masked]` 格式附加。仅 `privacy.redaction.enabled=true` 时激活 | PRD §5.1: 日志发给 LLM 前必须脱敏 |
| 10 | **历史持久化** — 每条用户消息和 AI 回复通过 `history.Store.AddMessage()` 实时写入。Tool result 超过 2000 字符存储压缩版本 (500 + 统计) | PRD §3.2: 对话历史持久化 |
| 11 | **Phase 2 安全白名单** — SafetyChecker 允许 20 个 tools (10 Phase 1 + 10 Phase 2)。新增: kubectl_get_statefulsets, kubectl_get_daemonsets, kubectl_get_jobs, kubectl_get_cronjobs, severity_stats, compare_logs, get_pod_metrics, trace_logs, bookmark_log_lines, switch_kubeconfig | Phase 2 新增工具均为只读 (switch_kubeconfig 修改连接但不修改集群状态) |

## Interfaces (defined in this module)

```go
type Agent interface {
    Execute(ctx context.Context, userQuery string, ch chan<- AgentEvent) error
    ClearHistory()
    SetClusterContext(ctx ClusterContext)
}

type ToolExecutor interface {
    ExecuteTool(ctx context.Context, name string, args map[string]string) (result string, logs []k8s.LogLine, err error)
    ToolDefinitions() []llm.ToolDefinition
}

type SafetyChecker interface {
    Check(toolName string, args map[string]string) (level SafetyLevel, reason string)
}
```

> Full type definitions (AgentEvent, AgentEventType, ClusterContext) → [`README.md`](./README.md) | SPEC Section 3.5 → [`../../SPEC.md`](../../SPEC.md)

## Files

| File | Do | Don't |
|------|-----|-------|
| `agent.go` | Agentic loop: 构建 messages → 调 LLM → 执行 tool → 反馈 → 迭代 | 不要在这里直接调 k8s API, 通过 ToolExecutor |
| `tools.go` | Tool name → k8s 操作映射 (20 tools: 10 Phase 1 + 10 Phase 2), label selector 发现, kubectl 命令字符串生成, `filter` 参数名称子串匹配, Resource Registry 集成 (StatefulSet/DaemonSet/Job/CronJob list tools), 分析工具 (severity_stats, compare_logs, get_pod_metrics, trace_logs), bookmark_log_lines, `navigate_resource_browser` UI 导航工具, `get_log_viewer_state`/`search_visible_logs` Log Viewer buffer 读取工具, `switch_kubeconfig` 集群上下文切换工具 | 不要绕过 SafetyChecker |
| `safety.go` | Phase 2 白名单: 20 个只读工具 (10 Phase 1 + 10 Phase 2). 返回 denial reason | 不要硬编码, 用可配置的白名单 map |
| `history.go` | FIFO 截断 (max 10 turns), tool result 压缩 (>2000 → 500 + stats), system prompt 始终保留 | 不要删除 system prompt |
| `prompt.go` | System prompt: 角色定义 + 安全规则 + cluster context + tool 使用指南 + 大集群策略 (filter 使用指导) + Log Viewer TUI 联动指令 + Resource Browser 导航指令 + analysis_mode 指令块 (结构化思维链: 现象→数据→假设→验证→结论) + 扩展资源发现策略 (Deployment→StatefulSet→DaemonSet→Job/CronJob 搜索顺序) + 脱敏感知指令 ([REDACTED] 标记处理) | 可以有 provider-aware 片段 (检查 llm.ProviderName()) |

## Agentic Loop Summary

```
User Query → [Build Messages] → [Call LLM] → Tool Calls?
  No  → Emit Summary + Complete
  Yes → For each ToolCall:
           SafetyCheck → Execute → Emit Events → Append Result
        → Loop back to [Call LLM] (iteration++)
        → Max iterations? → Emit Error + Complete
```

> Full pseudocode → [`../../SPEC.md`](../../SPEC.md) Section 4.1

## AgentEvent Types

| Event | When | UI Action |
|-------|------|-----------|
| `EventStreamDelta` | LLM 输出增量文本 | Chat 面板渐进渲染 |
| `EventToolCall` | Agent 调用 tool | Chat 面板显示 kubectl 命令 |
| `EventToolResult` | Tool 返回结果 | Chat 面板显示结果摘要 |
| `EventLogsReady` | 日志行就绪 | Log Viewer 写入 Ring Buffer |
| `EventSummary` | LLM 完成分析 | Chat 面板显示最终摘要 |
| `EventError` | 错误 (可能重试) | Chat 面板显示错误 |
| `EventComplete` | Loop 完成 | Chat 面板恢复输入状态 |
| `EventKubeSwitch` | Agent 请求切换 kubeconfig/context | App 执行 k8s.Reconnect, 重置 Resource/LogViewer, 持久化到 config |

## Cross-Module Dependencies

| This module | → | Dependency | Via |
|-------------|---|-----------|-----|
| agent | imports | llm | `llm.Provider`, `llm.Message`, `llm.ToolDefinition` |
| agent | imports | k8s | `k8s.Client` (via ToolExecutor 内部调用) |
| agent | imports | logparse | `logparse.Parser` (解析 tool 返回的日志行) |
| agent | imports | config | `config.AgentConfig` |
| agent | imports | history | `history.Store` (消息持久化) |
| agent | imports | redact | `redact.Engine` (脱敏管道) |
| ui | imports | agent | `agent.Agent` interface, `agent.AgentEvent` |

## Testing Checklist

- [ ] Mock LLM 返回单个 tool call → 验证 tool 执行 + result 反馈
- [ ] Mock LLM 返回多个 tool calls → 验证全部执行
- [ ] Mock LLM 第一次 tool call 失败 → 第二次迭代修正 → 成功
- [ ] Max iterations 达到 → 验证 EventError 触发
- [ ] SafetyChecker returns SafetyDenied for non-whitelisted tools → denial as tool result
- [ ] SafetyChecker returns SafetyAllowed for 7 read-only tools
- [ ] send_logs=false → 验证 tool result 只有状态不含日志原文
- [ ] History FIFO: 超过 10 turns 后旧对话被截断
- [ ] Tool result 压缩: >2000 字符截断为 500 + 统计
- [ ] Label selector discovery: "order-service" → 找到 Deployment → 提取 selector
