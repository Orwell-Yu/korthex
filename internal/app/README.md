# internal/app - Application Orchestration

> **Parent docs:** [CLAUDE.md (root)](../../CLAUDE.md) | [SPEC.md Section 3.6](../../SPEC.md) | [PRD Section 4](../../PRD.md)
>
> **Module rules:** [CLAUDE.md (this module)](./CLAUDE.md)
>
> **Depends on:** ALL modules — [config](../config/README.md), [k8s](../k8s/README.md), [llm](../llm/README.md), [agent](../agent/README.md), [ui](../ui/README.md), [logparse](../../pkg/logparse/README.md)

## Responsibility

Wire all modules together via dependency injection. Manage application lifecycle including startup sequence, graceful shutdown, and signal handling. This is the only module that imports concrete implementations.

## Key Types

```go
// App holds all dependencies and provides the Run() entry point
type App struct {
    config    *config.Config
    k8sClient k8s.Client
    llm       llm.Provider
    agent     agent.Agent
    // ... internal state
}

func New(cfg *config.Config) (*App, error)
func (a *App) Run(ctx context.Context) error
func (a *App) Shutdown()
```

## Files

| File | Responsibility |
|------|---------------|
| `app.go` | App struct, dependency injection, Run() method, graceful shutdown (context cancellation on SIGTERM; SIGINT/Ctrl+C handled natively by Bubble Tea), klog redirect to log file |

The CLI entry point lives at `cmd/korthex/main.go`:

| File | Responsibility |
|------|---------------|
| `cmd/korthex/main.go` | Parse CLI flags, create config.Manager, run wizard if needed, print splash logo, construct App, call App.Run() |

## Startup Sequence

```
main():
    0. printSplash()             // ASCII logo + version + "Initializing..."

    1. cfg = config.NewManager().Load()
       if config.NeedsSetup():
           detected = detectKubeconfig()
           cfg = config.NewWizard().Run(detected)
           config.Save(cfg)

    2. k8sClient = k8s.NewClient()
       k8sClient.Connect(cfg.Kubernetes.Kubeconfig, cfg.Kubernetes.DefaultContext)

    3. llmProvider = llm.NewRegistry().Create(cfg.LLM)
       llmProvider.ValidateConnection(ctx)

    3.5. redactEngine = redact.NewEngine(cfg.Privacy.Redaction)

    3.7. historyStore = history.NewStore(cfg.History)
         historyStore.CleanExpired(ctx, cfg.History.RetentionDays)
         sessionID = historyStore.CreateSession(ctx, k8sClient.CurrentContext())

    4. agentInstance = agent.New(llmProvider, k8sClient, logparse.NewParser(), redactEngine, historyStore, cfg.Agent)

    5. appModel = ui.NewAppModel(agentInstance, k8sClient, historyStore, cfg)

    6. p = tea.NewProgram(appModel, tea.WithAltScreen())
       p.Run()
```

## Dependencies

- **External**: `charmbracelet/bubbletea` (for tea.NewProgram)
- **Internal**: ALL modules — this is the composition root (config, k8s, llm, redact, history, agent, logparse, ui)

## Design Notes

- `cmd/korthex/main.go` is minimal: only flag parsing and App construction
- All business logic lives in internal packages
- `App.Run()` is the single orchestration point
- Graceful shutdown: context cancellation propagates to all goroutines (log streams, agent operations)
- Signal handling: SIGTERM -> cancel context -> Bubble Tea exits cleanly (SIGINT/Ctrl+C handled natively by Bubble Tea)
- Shutdown: `historyStore.EndSession(ctx, sessionID, aiGeneratedSummary)` before context cancellation
- klog/v2 redirect: client-go logs redirected to `~/.config/korthex/korthex.log` to prevent TUI pollution

## Testing Strategy

- **Integration tests**: verify full startup sequence with mock K8s + mock LLM
- **Shutdown tests**: verify context cancellation propagates and all goroutines exit
- **CLI flag tests**: verify flag parsing and config override behavior

## Phase 2+ Extension Points

- Add `--context` flag to override K8s context without config change
- Add `--namespace` flag to start focused on a specific namespace
- Add `--export-mcp` flag for MCP Server mode (Phase 4)
