# Wave 3E: internal/agent — AI Agent & Agentic Loop

> **Prerequisite:** Wave 2 (k8s + llm) merged to main
> **Branch:** feat/agent
> **Parallel with:** wave3_ui_base.md (Instance F), optionally wave3_ui_panels.md (Instance G)
> **Files owned:** `internal/agent/*.go` (nothing else)
> **Estimated time:** 5-7 hours

---

You are developing the `internal/agent` module for the Korthex project — the AI orchestration engine that translates natural language into Kubernetes tool calls via an agentic loop.

## Step 1: Read Documentation

Read these files **in order** before writing any code:
1. `internal/agent/CLAUDE.md` — module rules (SafetyLevel enum, maxIterations, send_logs, Error as tool result, CommandDisplay)
2. `internal/agent/README.md` — interfaces, agentic loop algorithm, label selector discovery, conversation history management
3. `SPEC.md` Section 3.5 — Agent interface contract (types in skeleton)
4. `SPEC.md` Section 4.1 — **Agentic Loop complete pseudocode** (follow exactly)
5. `SPEC.md` Section 4.2 — Label Selector intelligent discovery
6. `SPEC.md` Section 3.7 — Phase 1 Tool Definitions (7 tools with parameters)
7. `PRD.md` Section 10.7 — AI Chat context strategy (FIFO, tool result compression)

## Step 2: Implement

### File 1: `internal/agent/safety.go`

Implement `SafetyChecker`:
- `SafetyLevel` enum already defined in skeleton: `SafetyAllowed`, `SafetyDangerous`, `SafetyCritical`, `SafetyDenied`
- `Check(toolName, args) (SafetyLevel, string)`:
  - Phase 1 whitelist (7 tools): `kubectl_get_namespaces`, `kubectl_get_deployments`, `kubectl_get_pods`, `kubectl_logs`, `kubectl_logs_selector`, `kubectl_describe`, `kubectl_get_events` → return `SafetyAllowed, ""`
  - Everything else → return `SafetyDenied, "Operation not supported in Phase 1. Read-only operations only."`
- Use a configurable `map[string]SafetyLevel` internally (not hardcoded switch)

Constructor: `func NewSafetyChecker() SafetyChecker`

### File 2: `internal/agent/history.go`

Implement conversation history management:
- Store `[]llm.Message`
- `AppendUserMessage(content string)`
- `AppendAssistantMessage(msg llm.Message)`
- `AppendToolResult(toolCallID, toolName, result string)`
- `GetMessages() []llm.Message` — return system prompt + recent turns
- **FIFO truncation**: keep last `maxHistoryTurns` turns (1 turn = user + assistant + tool results)
- **System prompt always retained**: never truncated, not counted toward turn limit
- **Tool result compression**: results > 2000 chars → truncate to first 500 chars + stats summary (total lines, ERROR/WARN count)
- `ClearHistory()` — reset to empty (keep system prompt)

Constructor: `func NewHistoryManager(maxTurns int, systemPrompt string) *HistoryManager`

### File 3: `internal/agent/prompt.go`

Build the system prompt:
```go
func BuildSystemPrompt(ctx ClusterContext, providerName string, sendLogs bool) string
```

Content:
- Role: "You are Korthex, an AI assistant for Kubernetes cluster management."
- Context: inject `ctx.ContextName`, `ctx.Namespace`, `ctx.Deployments`
- Safety: "Phase 1: you may ONLY use read-only tools. Never attempt write operations."
- Strategy: "When a user mentions a service name, use kubectl_get_deployments to find it first, then extract its label selector for log queries. Prefer label selectors over pod names."
- send_logs=false note: "Log content will not be provided to you. You will receive execution status only (success/failure/line count)."
- Output guidance: "Always explain what command you're executing and why."
- Provider-aware tweaks: check `providerName` for Anthropic (XML-friendly) vs others

### File 4: `internal/agent/tools.go`

Implement `ToolExecutor`:
- `ToolDefinitions() []llm.ToolDefinition` — return 7 tool definitions with parameter specs
- `ExecuteTool(ctx, name, args) (string, []k8s.LogLine, error)` — dispatch to handler

**Tool handlers** (each maps to k8s.Client methods):

| Tool | Handler | Returns |
|------|---------|---------|
| `kubectl_get_namespaces` | `k8s.Resources().ListNamespaces()` | formatted text |
| `kubectl_get_deployments` | `k8s.Resources().ListDeployments(ns)` | formatted text |
| `kubectl_get_pods` | `k8s.Resources().ListPods(ns)` or `ListPodsBySelector` | formatted text |
| `kubectl_logs` | `k8s.Logs().GetLogs(ctx, req)` | text + `[]LogLine` |
| `kubectl_logs_selector` | label discovery → `k8s.Logs().GetLogs` for each pod | text + `[]LogLine` |
| `kubectl_describe` | `k8s.Describer().Describe(ns, kind, name)` | text |
| `kubectl_get_events` | `k8s.Events().ListEvents(ns)` | formatted text |

**Label Selector Discovery** (for `kubectl_logs_selector`):
1. `FindDeploymentByName(ns, serviceName)` → extract `Deployment.Selector`
2. If not found: `SearchResources(ns, serviceName)` (fuzzy) → return candidates
3. If nothing: return "No matching resources found"

**CommandDisplay generation**: each tool must produce a human-readable kubectl equivalent string:
- e.g., `kubectl_logs{ns:"prod", podName:"order-svc-abc"}` → `"kubectl logs order-svc-abc -n prod"`
- e.g., `kubectl_logs_selector{ns:"prod", labelSelector:"app=order-service"}` → `"kubectl logs -l app=order-service -n prod --all-containers"`

### File 5: `internal/agent/agent.go`

Implement the **Agentic Loop** — follow SPEC §4.1 pseudocode exactly:

```go
func (a *agentImpl) Execute(ctx context.Context, userQuery string, ch chan<- AgentEvent) error
```

Algorithm:
1. Append user message to history
2. For iteration = 1 to maxIterations:
   a. Call `llm.Chat(ctx, history.GetMessages(), tools.ToolDefinitions())`
   b. If no ToolCalls in response → emit EventSummary + EventComplete, return
   c. Append assistant message to history
   d. For each ToolCall:
      - `safety.Check(tc.Name, tc.Args)` → if SafetyDenied: emit EventError, append denial as tool result, continue
      - Emit `EventToolCall` (with CommandDisplay)
      - `tools.ExecuteTool(ctx, tc.Name, tc.Args)`
      - If error: emit EventError, append "ERROR: ..." as tool result
      - If logs returned: emit `EventLogsReady`
      - If send_logs=false and logs returned: tool result = "Success: N log lines returned" (not actual content)
      - Emit `EventToolResult`
      - Append tool result to history
   e. Continue loop (LLM sees tool results next iteration)
3. If max iterations reached: emit EventError("Max iterations reached") + EventComplete

Constructor: `func New(provider llm.Provider, k8sClient k8s.Client, parser logparse.Parser, cfg config.AgentConfig, sendLogs bool) Agent`

### File 6: `internal/agent/agent_test.go`

Tests with mock LLM + mock K8s:
- Single tool call → successful execution → summary
- Multiple tool calls in one response → all executed
- First iteration fails → second iteration succeeds (LLM self-corrects)
- Max iterations reached → EventError
- SafetyChecker denies non-whitelisted tool → denial as tool result
- SafetyChecker allows all 7 read-only tools
- send_logs=false → tool result contains only status, not log content
- History FIFO: > 10 turns → oldest truncated
- Tool result compression: > 2000 chars → truncated to 500 + stats

### File 7: `internal/agent/tools_test.go`

- Tool execution: each of the 7 tools with mock k8s.Client
- Label selector discovery: "order-service" → FindDeploymentByName → extract selector
- Label selector fallback: fuzzy search when exact match fails
- CommandDisplay generation: verify kubectl equivalent strings

## Step 3: Verify

```bash
go test -v -race ./internal/agent/...
go vet ./internal/agent/...
```

## Critical Rules

- **SafetyChecker.Check() returns SafetyLevel, not bool**
- **send_logs=false**: tool results show only execution status, never log content
- **Errors as tool results**: execution errors are "ERROR: ..." strings sent back to LLM for self-correction
- **maxIterations is a hard limit**: always emit EventComplete at the end
- **CommandDisplay is mandatory**: every EventToolCall must include human-readable kubectl equivalent
- **Do not directly call k8s APIs**: all K8s interaction goes through ToolExecutor (enables mock testing)
- **Do not import ui**: agent is Layer 2, ui is Layer 3
- **Use `slog` for debug logging**: `slog.Debug("agentic loop iteration", "iteration", i, "toolCalls", len(tcs))`, `slog.Warn("safety denied", "tool", name, "reason", reason)`. Bubble Tea occupies stdout — slog writes to file (configured by app layer). See SPEC §6.8
