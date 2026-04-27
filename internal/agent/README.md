# internal/agent - AI Agent

> **Parent docs:** [CLAUDE.md (root)](../../CLAUDE.md) | [SPEC.md Section 3.5](../../SPEC.md) | [PRD Section 6.4](../../PRD.md)
>
> **Module rules:** [CLAUDE.md (this module)](./CLAUDE.md)
>
> **Depends on:** [k8s](../k8s/README.md), [llm](../llm/README.md), [logparse](../../pkg/logparse/README.md), [config](../config/README.md) | **Depended by:** [ui](../ui/README.md)

## Responsibility

Orchestrate the agentic loop: translate user natural-language intent into tool calls, execute them against K8s, feed results back to the LLM, and iterate up to `max_iterations`. Manage conversation history and enforce safety rules.

## Public Interfaces

```go
// Agent is the core AI conversation orchestrator
type Agent interface {
    Execute(ctx context.Context, userQuery string, ch chan<- AgentEvent) error
    ClearHistory()
    SetClusterContext(ctx ClusterContext)
}

// ToolExecutor maps tool names to K8s operations
type ToolExecutor interface {
    ExecuteTool(ctx context.Context, name string, args map[string]string) (result string, logs []k8s.LogLine, err error)
    ToolDefinitions() []llm.ToolDefinition
}

// SafetyChecker enforces command safety levels
type SafetyChecker interface {
    Check(toolName string, args map[string]string) (level SafetyLevel, reason string)
}
```

## Key Types

| Type | Purpose |
|------|---------|
| `AgentEvent` | Event stream for UI consumption (tool calls, results, logs, summaries, errors) |
| `AgentEventType` | Enum: StreamDelta, ToolCall, ToolResult, LogsReady, Summary, Error, Complete |
| `ClusterContext` | Current context name, namespace, known deployments |
| `SafetyLevel` | Enum: Allowed, Dangerous, Critical, Denied — Phase 1 only uses Allowed/Denied |

## Files

| File | Responsibility |
|------|---------------|
| `agent.go` | Core agentic loop: build messages -> call LLM -> if tool calls: execute -> feed results -> iterate. Max N iterations |
| `tools.go` | ToolExecutor: map 19 tool names (10 Phase 1 + 9 Phase 2) to k8s.Client methods and analysis logic. Generate human-readable kubectl command strings. Label selector discovery strategy. `filter` parameter for large cluster support. Resource Registry integration for new resource types |
| `safety.go` | SafetyChecker: Phase 2 whitelist (19 read-only tools). Returns denial reason for blocked operations |
| `history.go` | Conversation history: []Message FIFO (configurable max turns), tool result compression (>2000 chars -> truncate + stats), system prompt always retained |
| `prompt.go` | System prompt builder: inject ClusterContext, safety rules, tool efficiency guidance. Key directives: "prefer label selectors", "use filter for large clusters", "be efficient — skip redundant steps, summarize once you have enough info", "kubectl_logs auto-pushes to Log Viewer panel — never paste logs in chat" |
| `agent_test.go` | Agentic loop tests with mock LLM + mock K8s |
| `tools_test.go` | Tool execution and label selector discovery tests |

## Agentic Loop Algorithm

```
Execute(ctx, query, ch):
    append user message to history

    for iteration = 1 to maxIterations:
        response = llm.Chat(ctx, history, toolDefinitions)

        if no tool calls in response:
            emit EventSummary(response.Content)
            emit EventComplete
            return

        for each tool call:
            if not allowed by SafetyChecker:
                emit EventError, append denial as tool result
                continue

            emit EventToolCall (with kubectl display string)
            execute tool via ToolExecutor
            if logs returned: emit EventLogsReady
            append tool result to history (respecting send_logs config)

        // LLM sees tool results on next iteration

    emit EventError("max iterations reached")
    emit EventComplete
```

## Label Selector Discovery (tools.go)

When user says "order-service" instead of a label selector:

1. `FindDeploymentByName(ns, "order-service")`
2. If found: extract `Deployment.Selector` -> use for `StreamMultiPodLogs`
3. If not found: `SearchResources(ns, "order-service")` (fuzzy)
4. If candidates found: return list for LLM to present to user
5. If nothing: return "No matching resources" for LLM to suggest corrections

## Dependencies

- **External**: None
- **Internal**: `llm` (Provider), `k8s` (Client), `logparse` (Parser), `config` (AgentConfig), `history` (Store), `redact` (Engine)

## Conversation History Management

- Retain last `max_history_turns` (default 20) turns (1 turn = user + assistant + tool calls/results)
- FIFO eviction: oldest turns dropped first
- System prompt + cluster metadata always retained (not counted in turns)
- Tool results >2000 chars: truncate to 500 + stats summary (line count, ERROR/WARN count)
- `send_logs=false`: tool results show execution status only, not log content

## Testing Strategy

- **Mock LLM**: return predetermined tool calls, verify iteration logic
- **Mock K8s**: return predetermined resources/logs, verify tool execution
- **End-to-end mock**: user query -> agentic loop -> verify emitted AgentEvents
- **Safety tests**: verify blocked operations return proper denial messages
- **History tests**: verify FIFO truncation, compression, system prompt retention

## Phase 2+ Extension Points

- Add Phase 3 write-operation tools (kubectl_scale, kubectl_restart, kubectl_delete)
- Extend SafetyChecker with "Dangerous" and "Critical" security levels
- Add tool result smart summarization via LLM (context optimization)

## Phase 2 Tools (9 new)

| Tool | Function | Parameters | Domain |
|------|----------|-----------|--------|
| `kubectl_get_statefulsets` | 列出 StatefulSet | namespace, filter (可选) | Enhanced Browser |
| `kubectl_get_daemonsets` | 列出 DaemonSet | namespace, filter (可选) | Enhanced Browser |
| `kubectl_get_jobs` | 列出 Job | namespace, filter (可选), activeOnly (可选) | Enhanced Browser |
| `kubectl_get_cronjobs` | 列出 CronJob | namespace, filter (可选) | Enhanced Browser |
| `severity_stats` | 统计 Ring Buffer 中 severity 分布 | sinceMinutes (可选, int, 默认全部) | Deep Analysis |
| `compare_logs` | 对比两个时间段的日志统计 | namespace, selector, since1, since2, duration | Deep Analysis |
| `get_pod_metrics` | 获取 Pod CPU/Memory 用量 | namespace, podName | Deep Analysis |
| `trace_logs` | 跨 namespace trace/request ID 搜索 | traceId, namespaces (可选), timeRange (可选) | Deep Analysis |
| `bookmark_log_lines` | AI 标记重要日志行 | lineIndices (int 数组) | Data Safety |

## Redaction Integration

Pipeline position: tool result → `redact.Engine.Redact()` → stats annotation → append to LLM history.

- Active only when `privacy.redaction.enabled=true` AND `send_logs=true`
- When `send_logs=false`, redaction is moot (logs not sent to LLM)
- Stats format: `[Redaction: 3 JWT, 2 email, 1 API key masked]`
- System prompt instructs AI: "[REDACTED] markers indicate redacted data — analyze error patterns, timelines, severity; don't guess redacted values"

## History Integration

Real-time message persistence via `history.Store`:

- Each user input → `AddMessage(sessionID, {role: "user", content, namespace})`
- Each AI response → `AddMessage(sessionID, {role: "assistant", content})`
- Tool results > 2000 chars → stored compressed (500 chars + stats, marked `[compressed]`)
- Shutdown → `EndSession(sessionID, aiGeneratedSummary)`
