# internal/config - Configuration Management

> **Parent docs:** [CLAUDE.md (root)](../../CLAUDE.md) | [SPEC.md Section 3.1](../../SPEC.md) | [PRD Section 5.0](../../PRD.md)
>
> **Module rules:** [CLAUDE.md (this module)](./CLAUDE.md)

## Responsibility

Load, validate, persist, and provide typed access to all application configuration. Own the first-run Setup Wizard flow.

## Public Interfaces

```go
// Manager provides configuration load/save/validate capabilities
type Manager interface {
    Load() (*Config, error)
    Save(cfg *Config) error
    Validate(cfg *Config) error
    ConfigPath() string
}

// Wizard drives the first-run setup flow
type Wizard interface {
    Run(detected KubeDetection) (*Config, error)
    NeedsSetup() bool
}
```

## Key Types

| Type | Purpose |
|------|---------|
| `Config` | Top-level config struct (Kubernetes + LLM + Agent + UI) |
| `KubernetesConfig` | kubeconfig path, default context |
| `LLMConfig` | provider, API key, model, base URL, temperature, send_logs |
| `AgentConfig` | max iterations, max history turns |
| `UIConfig` | theme, log lines limit, log page size, default log since |
| `PrivacyConfig` | redaction.enabled, redaction.rules, redaction.disable_builtin |
| `HistoryConfig` | enabled, retention_days, db_path |
| `AnalysisConfig` | trace_id_patterns |
| `KubeDetection` | Wizard input: detected kubeconfig path, contexts |
| `KubeconfigEntry` | Discovered kubeconfig file (path, display name) |
| `ContextEntry` | Parsed context from a kubeconfig file (name, cluster, user) |

## Files

| File | Responsibility |
|------|---------------|
| `config.go` | Config structs, Manager implementation (Viper-based), env-var override logic, validation, kubeconfig discovery (`DiscoverKubeconfigs`, `ParseContexts`) |
| `wizard.go` | Bubble Tea mini-program for first-run setup (detect kubeconfig, prompt LLM config, privacy disclosure) |
| `config_test.go` | Table-driven tests: load/validate/env-override/defaults |

## Dependencies

- **External**: `spf13/viper` (config management)
- **Internal**: None (Layer 0)

## Environment Variable Priority (high to low)

1. `KORTHEX_LLM_API_KEY` or provider-specific (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`)
2. `~/.config/korthex/config.yaml` field `llm.api_key`
3. No key -> Setup Wizard prompts

## Testing Strategy

- **Unit tests**: Table-driven tests for Load (file + env override), Validate (missing fields, invalid values), Save (round-trip)
- **Env var tests**: `t.Setenv()` to test env override priority
- **Wizard tests**: Bubble Tea `teatest` for headless wizard flow verification

## Kubeconfig Discovery

Runtime kubeconfig discovery for the kubeconfig switching feature (UI overlay and agent tool):

```go
// DiscoverKubeconfigs scans ~/.kube/ and $KUBECONFIG for kubeconfig files.
// Returns a sorted list of KubeconfigEntry with path and display name.
func DiscoverKubeconfigs(kubeDir, envKubeconfig string) []KubeconfigEntry

// ParseContexts reads a kubeconfig file and returns all contexts defined in it.
func ParseContexts(kubeconfigPath string) ([]ContextEntry, error)
```

- `DiscoverKubeconfigs` scans the standard `~/.kube/` directory and parses the `$KUBECONFIG` environment variable (colon-separated paths) to find all available kubeconfig files.
- `ParseContexts` parses a single kubeconfig file to extract its contexts (name, cluster, user), used by the two-step kubeconfig switch overlay (select file, then select context).

## Phase 2+ Extension Points

- Add `Ollama` to provider enum (Phase 3/4, deferred per P2-D1)
- Add config migration tooling for cross-version upgrades
