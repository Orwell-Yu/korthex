# Wave 4: Integration — App + Wizard + E2E

> **Prerequisite:** ALL Wave 3 branches merged to main
> **Branch:** feat/integration (or directly on main)
> **Files owned:** `internal/app/app.go`, `cmd/korthex/main.go` + E2E verification
> **Estimated time:** 3-4 hours

---

You are the integration engineer for the Korthex project. All modules have been implemented by separate developers. Your job is to wire everything together, implement the application lifecycle, and verify end-to-end functionality.

## Step 1: Read Documentation

Read these files:
1. `internal/app/CLAUDE.md` — module rules (Layer 4, wires everything, graceful shutdown, wizard-first)
2. `internal/app/README.md` — startup sequence, design notes
3. `SPEC.md` Section 6.8 — Logging Strategy (slog + file output)
4. `SPEC.md` Section 4.5 — Setup Wizard state machine (already implemented in config module)
5. `SPEC.md` Section 4.6 — Message Flow Diagram (verify all pipelines work)

Also browse the implemented modules to understand what's available:
- `internal/config/` — Manager.Load(), Wizard.Run(), Wizard.NeedsSetup()
- `internal/k8s/` — NewClient(), Client.Connect()
- `internal/llm/` — NewRegistry(), Registry.Create(), Provider.ValidateConnection()
- `internal/agent/` — New(), Agent.Execute()
- `internal/ui/` — NewAppModel()
- `pkg/logparse/` — NewParser()

## Step 2: Implement

### File 1: `internal/app/app.go`

```go
type App struct {
    config     *config.Config
    k8sClient  k8s.Client
    llmProvider llm.Provider
    agent      agent.Agent
    logFile    *os.File  // debug log file
}

func New(cfg *config.Config) (*App, error)
func (a *App) Run(ctx context.Context) error
func (a *App) Shutdown()
```

**New(cfg):**
1. Setup logging (per SPEC §6.8):
   ```go
   logDir := filepath.Join(os.TempDir(), "korthex")
   os.MkdirAll(logDir, 0755)
   logFile, _ := os.OpenFile(filepath.Join(logDir, "korthex.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
   level := slog.LevelWarn
   if os.Getenv("KORTHEX_LOG_LEVEL") == "debug" { level = slog.LevelDebug }
   slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: level})))
   ```
2. Create K8s client: `k8s.NewClient()`
3. Connect: `k8sClient.Connect(cfg.Kubernetes.Kubeconfig, cfg.Kubernetes.DefaultContext)`
4. Create LLM provider: `llm.NewRegistry().Create(cfg.LLM)`
5. Validate LLM: `llmProvider.ValidateConnection(ctx)` (with 10s timeout)
6. Create agent: `agent.New(llmProvider, k8sClient, logparse.NewParser(), cfg.Agent, cfg.LLM.SendLogs)`
7. Return `&App{...}`, nil

**Run(ctx):**
1. Create `AppModel`: `ui.NewAppModel(a.agent, a.k8sClient, a.config)`
2. Create `tea.Program`: `tea.NewProgram(appModel, tea.WithAltScreen(), tea.WithMouseAllMotion())`
3. `appModel.SetProgram(p)` — inject program reference for p.Send()
4. Setup signal handling: catch SIGINT/SIGTERM → cancel context
5. `p.Run()` — blocks until quit
6. Return nil

**Shutdown():**
1. Close log file
2. Disconnect K8s client
3. Log "Korthex shutdown complete"

### File 2: `cmd/korthex/main.go`

Minimal entry point (target: under 80 lines):

```go
func main() {
    // 1. Parse flags
    var configPath string
    flag.StringVar(&configPath, "config", "", "config file path")
    flag.Parse()

    // 2. Load config
    mgr := config.NewManager()
    cfg, err := mgr.Load()
    if err != nil || config.NewWizard().NeedsSetup() {
        // Run wizard
        detected := detectKubeconfig()  // helper: find ~/.kube/config, parse contexts
        wizard := config.NewWizard()
        cfg, err = wizard.Run(detected)
        if err != nil { fatal(err) }
        mgr.Save(cfg)
    }

    // 3. Validate
    if err := mgr.Validate(cfg); err != nil { fatal(err) }

    // 4. Create and run app
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    app, err := app.New(cfg)
    if err != nil { fatal(err) }
    defer app.Shutdown()

    if err := app.Run(ctx); err != nil { fatal(err) }
}
```

Helper function `detectKubeconfig()`:
- Check `KUBECONFIG` env var, fallback to `~/.kube/config`
- Parse contexts using `clientcmd.LoadFromFile`
- Return `config.KubeDetection{KubeconfigPath, Contexts, CurrentContext}`

### File 3: Version information

Add version variables to `cmd/korthex/main.go`:
```go
var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)
```
These are injected by GoReleaser via ldflags.

Add `--version` flag:
```go
if *showVersion {
    fmt.Printf("korthex %s (%s) built %s\n", version, commit, date)
    os.Exit(0)
}
```

## Step 3: End-to-End Verification

Run the full CI pipeline:
```bash
make ci   # lint + test + build
```

Then manually verify (requires real K8s cluster + LLM API key):

1. **Fresh start**: delete `~/.config/korthex/config.yaml` → run `./bin/korthex` → Wizard should launch
2. **Wizard flow**: select context → select provider → enter API key → select model → confirm privacy → TUI starts
3. **Resource browsing**: see namespaces → Enter → see deployments → Enter → see pods
4. **Manual logs**: select pod → press `l` → see colored log stream
5. **AI chat**: press `:` → type "show me error logs from [some-service]" → see tool calls + logs + summary
6. **Layout switching**: F1/F2/F3 → verify three layout modes
7. **Keyboard**: Tab cycling, `/` search, `?` help, `q` quit
8. **Graceful shutdown**: Ctrl+C during agent operation → agent cancels → quit cleanly

## Step 4: Fix Integration Issues

If any module doesn't wire together correctly:
- Check interface compatibility (types from skeleton must match implementations)
- Check import paths
- Fix any compilation errors
- Do NOT modify module interfaces — if there's a mismatch, the module implementation needs to be fixed

## Critical Rules

- **main.go under 80 lines**: all logic in app.go
- **Wizard runs BEFORE main TUI**: if NeedsSetup(), wizard is a separate tea.Program
- **ValidateConnection before TUI**: catch API key errors early, not after TUI renders
- **Graceful shutdown**: SIGINT/SIGTERM → context cancel → all goroutines exit → Bubble Tea exits
- **Logging initialized first**: slog must be set up before any other module logs
- **Do NOT modify other module files**: if something doesn't compile, flag it — don't change interfaces
