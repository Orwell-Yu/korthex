# Wave 1B: internal/config — Configuration Management

> **Prerequisite:** Wave 0 (scaffold) merged to main
> **Branch:** feat/config
> **Parallel with:** wave1_logparse.md (Instance A)
> **Files owned:** `internal/config/*.go` (nothing else)
> **Estimated time:** 3-4 hours

---

You are developing the `internal/config` module for the Korthex project — configuration loading, validation, persistence, and the first-run Setup Wizard.

## Step 1: Read Documentation

Read these files **in order** before writing any code:
1. `internal/config/CLAUDE.md` — module rules (Layer 0, Viper only, env priority, Wizard is independent program)
2. `internal/config/README.md` — interfaces, file responsibilities, env var priority, testing strategy
3. `SPEC.md` Section 3.1 — Config interface contract (types already in skeleton)
4. `SPEC.md` Section 4.5 — Setup Wizard state machine
5. `configs/default.yaml` — default configuration reference
6. `PRD.md` Section 5.0 — Setup Wizard user-facing spec

## Step 2: Implement

The skeleton file `internal/config/config.go` already has type definitions (Config, Manager, Wizard interfaces). You need to implement the actual logic.

### File 1: `internal/config/config.go` (implementation)

Implement `Manager` using Viper:

**Load():**
- Config path: `~/.config/korthex/config.yaml`
- Use Viper to read YAML
- Environment variable override (highest priority):
  1. `KORTHEX_LLM_API_KEY` → `LLMConfig.APIKey`
  2. If empty, check provider-specific: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY` (based on `LLMConfig.Provider`)
  3. If still empty, use value from config.yaml
- Apply defaults from `configs/default.yaml` values for any missing fields

**Save(cfg):**
- Write to `~/.config/korthex/config.yaml`
- Create parent directories if needed
- Set file permissions to 0600 (API key in file)

**Validate(cfg):**
- Required: `Kubernetes.Kubeconfig` must be non-empty and file must exist
- Required: `LLM.Provider` must be one of: openai, anthropic, gemini, custom
- Required: `LLM.APIKey` must be non-empty
- Required if custom: `LLM.BaseURL` must be non-empty
- `Agent.MaxIterations` must be > 0 (default 20)
- `Agent.MaxHistoryTurns` must be > 0 (default 20)
- `UI.LogLinesLimit` must be > 0 (default 10000)
- Return all validation errors (not just the first one)

**ConfigPath():**
- Return `~/.config/korthex/config.yaml`

Constructor: `func NewManager() Manager`

### File 2: `internal/config/wizard.go`

Implement the Setup Wizard as an **independent Bubble Tea program** (NOT a mode of the main TUI):

**State machine** (per SPEC §4.5):
```
SelectContext → SelectProvider → EnterAPIKey → SelectModel → PrivacyDisclosure → Complete
```

- `NeedsSetup() bool` — return true if config file does not exist
- `Run(detected KubeDetection) (*Config, error)` — run the Bubble Tea wizard, return completed config
- Use `bubbles` components: `textinput` for API key (EchoMode=Password), `list` for selections
- `SelectContext`: list available K8s contexts from `detected.Contexts` (passed in, NOT parsed here)
- `SelectProvider`: OpenAI / Anthropic / Gemini / Custom
- `EnterAPIKey`: masked input
- `SelectModel`: show provider-specific default (OpenAI: gpt-4o, Anthropic: claude-sonnet-4-20250514, Gemini: gemini-2.5-flash), allow override
- `PrivacyDisclosure`: display privacy warning (logs sent to LLM provider), require explicit confirmation
- `Complete`: build and return Config

**Important:** The Wizard does NOT detect kubeconfig or parse contexts itself. The caller (`cmd/korthex/main.go`, see `wave4_integration.md`) detects kubeconfig using client-go's `clientcmd` and passes `KubeDetection` to `Run()`. This keeps config at Layer 0 with zero client-go dependency.

Constructor: `func NewWizard() Wizard`

### File 3: `internal/config/config_test.go`

Table-driven tests:

- **Load tests:**
  - Load from valid config file
  - Load with env var override (`KORTHEX_LLM_API_KEY` beats config file)
  - Load with provider-specific env var (`OPENAI_API_KEY` when provider=openai)
  - Load with missing file → returns defaults
  - Use `t.TempDir()` for test config files, `t.Setenv()` for env vars

- **Validate tests:**
  - Missing kubeconfig path → error
  - Invalid provider → error
  - Missing API key → error
  - Custom provider without base_url → error
  - Valid config → no error

- **Save tests:**
  - Save → Load round-trip produces same config
  - Creates parent directory if needed
  - File permissions are 0600

- **NeedsSetup tests:**
  - Config file exists → false
  - Config file missing → true

## Step 3: Verify

```bash
go test -v -race ./internal/config/...
go vet ./internal/config/...
```

All tests must pass.

## Critical Rules

- **ZERO internal imports**: do not import any `internal/` or `pkg/` package — only stdlib + Viper + Bubble Tea/Bubbles
- **ZERO client-go imports**: kubeconfig detection is done by the caller (`main.go`), not by config. Wizard receives `KubeDetection` as input.
- **Wizard is an independent `tea.NewProgram()`**: do not make it a mode of the main TUI
- **Never hardcode API keys** in code
- **Env vars always win**: `KORTHEX_LLM_API_KEY` > provider-specific env > config.yaml
- **Validate must be comprehensive**: catch all missing required fields, return descriptive error messages
- **Do not mock Viper**: use real temp files with `t.TempDir()`
