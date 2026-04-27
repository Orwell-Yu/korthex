# Wave 0: Scaffold — Interface Skeleton + go.mod

> **Prerequisite:** None
> **Branch:** main
> **Must complete before:** All other Waves
> **Estimated time:** 1-2 hours

---

You are the lead engineer for the Korthex project, responsible for Sprint 0 scaffolding.

Read the following docs **in order**:
1. `CLAUDE.md` — project rules and architecture constraints
2. `SPEC.md` Section 2 (Architecture & Module Design) and Section 3 (Key Interface Contracts) — all interface definitions
3. `configs/default.yaml` — configuration reference

Then execute these tasks:

## Task 1: Initialize Go Module

```bash
go mod init github.com/Orwell-Yu/korthex
```

Add all dependencies to go.mod (run `go get`):
- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/lipgloss`
- `github.com/charmbracelet/bubbles`
- `k8s.io/client-go` (pin to v0.30.x)
- `k8s.io/api`
- `k8s.io/apimachinery`
- `github.com/openai/openai-go`
- `github.com/anthropics/anthropic-sdk-go`
- `google.golang.org/genai`
- `github.com/spf13/viper`
- `github.com/cenkalti/backoff/v4`
- `github.com/sahilm/fuzzy`
- `github.com/stretchr/testify`

Run `go mod tidy` to resolve the dependency tree.

## Task 2: Create Interface Skeleton Files

Create **only type definitions and interfaces** — no implementation code:

### internal/config/config.go
- `Config`, `KubernetesConfig`, `LLMConfig`, `AgentConfig`, `UIConfig` structs
- `Manager` interface (Load, Save, Validate, ConfigPath)
- `Wizard` interface (Run, NeedsSetup)
- `KubeDetection` struct
- Exactly as defined in SPEC.md Section 3.1

### pkg/logparse/parser.go
- `Severity` type and constants (SeverityUnknown through SeverityFatal)
- `LogEntry` struct (Timestamp, Severity, PodName, Container, Raw, IsJSON, JSONPretty)
- `Parser` interface (Parse method)
- `Size()` method signature on LogEntry
- Exactly as defined in SPEC.md Section 3.2

### internal/k8s/types.go
- `Namespace`, `Deployment`, `Pod`, `Container`, `Event` structs
- `LogRequest`, `LogLine`, `SearchResult` structs
- Exactly as defined in SPEC.md Section 3.3

### internal/k8s/client.go
- `Client` interface (Connect, Disconnect, IsConnected, CurrentContext, Resources, Logs, Events, Describer)
- `ResourceLister` interface
- `LogStreamer` interface
- `EventLister` interface
- `ResourceDescriber` interface
- Exactly as defined in SPEC.md Section 3.3

### internal/llm/provider.go
- `Role` type and constants
- `Message`, `ToolCall`, `ToolDefinition`, `ParameterDef`, `StreamDelta` types
- `Provider` interface (Chat, ChatStream, ModelName, ProviderName, ValidateConnection)
- `Registry` interface (Create, SupportedProviders)
- Exactly as defined in SPEC.md Section 3.4

### internal/llm/errors.go
- Sentinel errors: `ErrRateLimit`, `ErrAuth`, `ErrTimeout`, `ErrModelUnavailable`
- Use `errors.New()` for sentinel definitions

### internal/agent/agent.go
- `Agent` interface (Execute, ClearHistory, SetClusterContext)
- `ClusterContext` struct
- `AgentEvent` struct and `AgentEventType` enum (7 event types)
- `ToolExecutor` interface (ExecuteTool, ToolDefinitions)
- `SafetyLevel` type with 4 levels: SafetyAllowed, SafetyDangerous, SafetyCritical, SafetyDenied
- `SafetyChecker` interface with `Check()` method returning `(SafetyLevel, string)`
- **Important:** SafetyChecker uses `Check()` returning `SafetyLevel`, NOT `IsAllowed()` returning `bool`
- Exactly as defined in SPEC.md Section 3.5

### internal/ui/messages.go
- Custom tea.Msg types: `AgentEventMsg`, `NavigateToLogsMsg`, `LogsLoadedMsg`, `LogLineMsg`, `InformerUpdateMsg`, `ClusterConnectedMsg`, `ErrorMsg`
- `PanelID` and `LayoutMode` enums
- As defined in SPEC.md Section 3.6 and Section 4.6

### internal/app/app.go
- Minimal `App` struct with placeholder
- `func New(cfg *config.Config) (*App, error)` — return nil, nil
- `func (a *App) Run(ctx context.Context) error` — return nil

### cmd/korthex/main.go
- Minimal main function: `fmt.Println("korthex - AI-native K8s TUI")`

## Task 3: Create Mock Implementations

Wave 3 instances (agent, ui) need mocks to write tests against dependencies they don't own.
Create mock structs using the **function-field pattern** — each interface method delegates to a `XxxFunc` field, enabling per-test behavior injection.

### pkg/logparse/mock_parser.go

```go
type MockParser struct {
    ParseFunc func(podName, container, rawLine string) LogEntry
}
func (m *MockParser) Parse(podName, container, rawLine string) LogEntry {
    if m.ParseFunc != nil { return m.ParseFunc(podName, container, rawLine) }
    return LogEntry{Raw: rawLine, PodName: podName, Container: container}
}
```

### internal/k8s/mock_client.go

Mock all 5 sub-interfaces: `MockResourceLister`, `MockLogStreamer`, `MockEventLister`, `MockResourceDescriber`, plus `MockClient` that returns them.

```go
type MockResourceLister struct {
    ListNamespacesFunc       func() ([]Namespace, error)
    ListDeploymentsFunc      func(ns string) ([]Deployment, error)
    ListPodsFunc             func(ns string) ([]Pod, error)
    ListPodsBySelectorFunc   func(ns, selector string) ([]Pod, error)
    FindDeploymentByNameFunc func(ns, name string) (*Deployment, error)
    SearchResourcesFunc      func(ns, query string) ([]SearchResult, error)
}
// Each method: if XxxFunc != nil call it, else return nil, nil
```

Same pattern for `MockLogStreamer`, `MockEventLister`, `MockResourceDescriber`.

`MockClient`: holds mock sub-interfaces as fields, `Resources()` / `Logs()` / `Events()` / `Describer()` return them.

### internal/llm/mock_provider.go

```go
type MockProvider struct {
    ChatFunc               func(ctx context.Context, msgs []Message, tools []ToolDefinition) (*Message, error)
    ChatStreamFunc         func(ctx context.Context, msgs []Message, tools []ToolDefinition, ch chan<- StreamDelta) error
    ModelNameFunc          func() string
    ProviderNameFunc       func() string
    ValidateConnectionFunc func(ctx context.Context) error
}
// Defaults: Chat returns empty Message, ModelName returns "mock-model", ProviderName returns "mock", etc.
```

### internal/agent/mock_agent.go

```go
type MockAgent struct {
    ExecuteFunc           func(ctx context.Context, userQuery string, ch chan<- AgentEvent) error
    ClearHistoryFunc      func()
    SetClusterContextFunc func(ctx ClusterContext)
}
// Each method delegates to XxxFunc
```

**Rules for mocks:**
- All mocks are exported (capital M) — usable from other packages' test files
- Default behavior (nil func): return zero values, no error
- Never import external SDKs (client-go, LLM SDKs) in mock files
- Mock files do NOT have `_test.go` suffix — they are importable from other packages

## Task 4: Verify

```bash
go build ./...      # must compile
make lint           # must pass
```

## Task 5: Commit

Commit everything to main with message:
```
feat: scaffold — interface skeletons, go.mod, all dependencies

All module interfaces defined per SPEC.md Section 3.
No implementation code — skeleton only for parallel development.
```

## Critical Rules

- Interface definitions must **exactly match** SPEC.md Section 3 code blocks
- Do NOT write any implementation code (no function bodies beyond `return nil`)
- Every file must have correct `package` declaration and necessary `import`
- `go build ./...` must pass — this means all types must be resolvable across packages
