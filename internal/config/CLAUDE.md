# internal/config — CLAUDE.md

> Configuration management: load, validate, persist, and Setup Wizard.

## Parent Reference

- **When confused about architecture:** Read [`../../CLAUDE.md`](../../CLAUDE.md) (root project rules)
- **When confused about interface design:** Read [`../../SPEC.md`](../../SPEC.md) Section 3.1 (Config interfaces)
- **When confused about config fields:** Read [`../../configs/default.yaml`](../../configs/default.yaml)
- **When confused about product requirement:** Read [`../../PRD.md`](../../PRD.md) Section 5.0 (Setup Wizard)

## Module Rules

| # | Rule | Reason |
|---|------|--------|
| 1 | **Layer 0: 禁止 import 任何 internal/ 或 pkg/ 包** | Foundation 层，零内部依赖。只依赖 stdlib + Viper |
| 2 | **环境变量优先** — Load() 必须先检查 env var，再 fallback 到 config.yaml | `KORTHEX_LLM_API_KEY` > `OPENAI_API_KEY` > config.yaml `llm.api_key` |
| 3 | **Validate 必须全面** — 所有必填字段必须在 Validate() 中检查 | 避免运行时 nil panic |
| 4 | **Wizard 是独立 Bubble Tea program** — 不要尝试把 Wizard 做成主 TUI 的一个 mode | 清晰的生命周期: Wizard 完成 → 返回 Config → 主 TUI 启动 |
| 5 | **永远不在代码中硬编码 API Key** | 安全性底线 |
| 6 | **Phase 2 配置段必须有默认值** — `privacy`, `history`, `analysis` 段必须在 `applyDefaults()` 中填充合理默认值。用户现有 Phase 1 config.yaml 缺少这些段时不能引起 nil panic | Phase 1 → Phase 2 升级零配置变更 |

## Interfaces (defined in this module)

```go
type Manager interface {
    Load() (*Config, error)
    Save(cfg *Config) error
    Validate(cfg *Config) error
    ConfigPath() string
}

type Wizard interface {
    Run(detected KubeDetection) (*Config, error)
    NeedsSetup() bool
}
```

> Full type definitions → [`README.md`](./README.md) | SPEC Section 3.1 → [`../../SPEC.md`](../../SPEC.md)

## Files

| File | Do | Don't |
|------|-----|-------|
| `config.go` | Viper load/save, env override, Validate | 不要在这里做 K8s 连接测试 |
| `wizard.go` | Bubble Tea mini-program, 步骤状态机 | 不要启动主 TUI，Wizard 是独立 program |
| `config_test.go` | Table-driven: load/validate/env/defaults | 不要 mock Viper，直接用临时文件 |

## Who Depends on Me

- `internal/k8s` → 读取 `KubernetesConfig` (kubeconfig path, context)
- `internal/llm` → 读取 `LLMConfig` (provider, API key, model, base URL)
- `internal/agent` → 读取 `AgentConfig` (max iterations, max history turns)
- `internal/ui` → 读取 `UIConfig` (theme, log limits)
- `internal/history` → 读取 `HistoryConfig` (db_path, retention_days, enabled)
- `internal/app` → 调用 Manager.Load() 和 Wizard.Run()

## Testing Checklist

- [ ] Load from file + env override priority
- [ ] Validate catches missing required fields
- [ ] Save round-trip (Load → Save → Load = same)
- [ ] Provider-specific env vars (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`)
- [ ] Wizard state machine: happy path + Ctrl+C cancel
