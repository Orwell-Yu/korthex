# internal/app — CLAUDE.md

> Application orchestration: dependency injection, startup sequence, graceful shutdown. The composition root.

## Parent Reference

- **When confused about architecture:** Read [`../../CLAUDE.md`](../../CLAUDE.md) (root project rules)
- **When confused about startup sequence:** Read [`../../SPEC.md`](../../SPEC.md) Section 3.6 (App Module) — search "Startup Sequence"
- **When confused about wizard flow:** Read [`../config/CLAUDE.md`](../config/CLAUDE.md) (Wizard rules)
- **When confused about all module interfaces:** Read [`../../SPEC.md`](../../SPEC.md) Section 3 (Key Interface Contracts)

## Module Rules

| # | Rule | Reason |
|---|------|--------|
| 1 | **Layer 4: 唯一可以 import 所有模块的地方** | 这是 composition root, 做依赖注入 |
| 2 | **main.go 只做 wiring** — 解析 flags, 创建依赖, 启动 App. 不含业务逻辑 | 保持入口简洁, 所有逻辑在 internal/ |
| 3 | **Graceful shutdown** — 必须处理 SIGTERM, 通过 context cancellation 传播 (SIGINT 由 Bubble Tea 原生处理 Ctrl+C) | 防止日志流 goroutine 泄漏 |
| 4 | **Wizard 先于 TUI** — config.NeedsSetup() 时先运行 Wizard, 完成后再启动主 TUI | Wizard 是独立 tea.Program, 不是主 TUI 的一部分 |
| 5 | **ValidateConnection 在启动时** — LLM Provider 和 K8s 连接必须在 TUI 启动前验证 | 提前发现配置错误, 避免 TUI 启动后再报错 |
| 6 | **Phase 2 DI 顺序** — redact.NewEngine 在 agent.New 之前, history.NewStore 在 agent.New 和 ui.NewAppModel 之前。完整顺序: config → k8s → llm → redact → history(+cleanup+session) → agent(+redact,+history) → ui(+history) → tea.Program | history.Store 被 agent 和 ui 双消费; redact.Engine 被 agent 消费 |

## Startup Sequence

```
main.go:
  0. startSplash()               // ASCII logo + Braille art + spinner animation
  1. config.NewManager().Load()
     → NeedsSetup? → config.NewWizard().Run() → config.Save()
  2. k8s.NewClient().Connect(kubeconfig, context)
  3. llm.NewRegistry().Create(llmConfig)
     → llm.ValidateConnection(ctx)
  3.5. redactEngine = redact.NewEngine(cfg.Privacy.Redaction)
  3.7. historyStore = history.NewStore(cfg.History)
     → historyStore.CleanExpired(ctx, cfg.History.RetentionDays)
     → historyStore.CreateSession(ctx, k8sClient.CurrentContext())
  4. agent.New(llmProvider, k8sClient, logparse.NewParser(), redactEngine, historyStore, cfg.Agent)
  stopSplash()                    // stop spinner animation
  5. ui.NewAppModel(agent, k8sClient, cfg)
  6. tea.NewProgram(appModel, tea.WithAltScreen()).Run()
```

> Full startup sequence → [`../../SPEC.md`](../../SPEC.md) Section 3.6

## Files

| File | Do | Don't |
|------|-----|-------|
| `app.go` | App struct, Run() entry point, graceful shutdown | 不要在这里放 UI 渲染逻辑 |
| `cmd/korthex/main.go` | Flag parsing, DI wiring, App construction | 不要超过 100 行, 所有逻辑在 App.Run() |

## Cross-Module Dependencies

| This module | → | Dependency | Purpose |
|-------------|---|-----------|---------|
| app | imports | config | Load() + Wizard |
| app | imports | k8s | NewClient() + Connect() |
| app | imports | llm | NewRegistry() + Create() + ValidateConnection() |
| app | imports | agent | New() |
| app | imports | logparse | NewParser() |
| app | imports | redact | NewEngine() |
| app | imports | history | NewStore() + CleanExpired() + CreateSession() |
| app | imports | ui | NewAppModel() |
| app | imports | bubbletea | tea.NewProgram() |

## Testing Checklist

- [ ] Startup with valid config: all dependencies created successfully
- [ ] Startup with missing API key: proper error message
- [ ] Startup with unreachable K8s cluster: proper error message
- [ ] Startup with invalid LLM API key: ValidateConnection fails gracefully
- [ ] Graceful shutdown: SIGTERM → all goroutines exit (SIGINT handled by Bubble Tea)
- [ ] Wizard flow: NeedsSetup=true → Wizard runs → config saved → TUI starts
