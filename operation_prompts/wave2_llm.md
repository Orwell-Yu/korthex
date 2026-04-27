# Wave 2D: internal/llm — Multi-Provider LLM Integration

> **Prerequisite:** Wave 1 (logparse + config) merged to main
> **Branch:** feat/llm
> **Parallel with:** wave2_k8s.md (Instance C)
> **Files owned:** `internal/llm/*.go` (nothing else)
> **Estimated time:** 4-5 hours

---

You are developing the `internal/llm` module for the Korthex project — a unified abstraction over OpenAI, Anthropic, and Gemini LLM APIs with Tool-Use / Function-Calling support.

## Step 1: Read Documentation

Read these files **in order** before writing any code:
1. `internal/llm/CLAUDE.md` — module rules (SDK types don't leak, adapters independent, errors standardized, Gemini map[string]any handling, ToolCall.Arguments constraints)
2. `internal/llm/README.md` — interfaces, adapter responsibilities, provider-specific notes, testing strategy
3. `SPEC.md` Section 3.4 — LLM interface contract (types already in skeleton)
4. `SPEC.md` Section 3.7 — Phase 1 Tool Definitions (7 tools, what parameters they have)

**Pay special attention** to `llm/CLAUDE.md`:
- "Provider-Specific Notes" table (Gemini `map[string]any` handling)
- "ToolCall.Arguments 类型约束" section

## Step 2: Implement

### File 1: `internal/llm/provider.go`

Already defined in skeleton. Verify all types are complete.

### File 2: `internal/llm/errors.go`

Already defined in skeleton. Verify sentinel errors exist. Add helper functions if needed:
```go
func IsRateLimit(err error) bool { return errors.Is(err, ErrRateLimit) }
func IsAuth(err error) bool      { return errors.Is(err, ErrAuth) }
```

### File 3: `internal/llm/registry.go`

Implement `Registry`:
- Internal map: `map[string]func(config.LLMConfig) (Provider, error)`
- `Create(cfg)` — look up constructor by `cfg.Provider`, call it
- `SupportedProviders()` — return registered provider names
- Register "openai", "anthropic", "gemini", "custom" on init
- "custom" reuses the OpenAI constructor (with custom BaseURL)

Constructor: `func NewRegistry() Registry`

### File 4: `internal/llm/openai.go`

Implement OpenAI adapter using `github.com/openai/openai-go` (v3, official):

**Constructor:** `func NewOpenAIProvider(cfg config.LLMConfig) (Provider, error)`
- If `cfg.BaseURL` is set (custom provider), use it; otherwise use default OpenAI URL
- Create client with API key

**Chat(ctx, messages, tools):**
1. Convert `[]ToolDefinition` → `[]ChatCompletionToolParam` (JSON Schema format)
2. Convert `[]Message` → OpenAI messages format
3. Call `client.Chat.Completions.New(ctx, params)`
4. Convert response → `Message` with `[]ToolCall` (if any)

**ChatStream(ctx, messages, tools, ch):**
1. Same conversion as Chat
2. Use streaming API
3. Send `StreamDelta` for each chunk
4. Set `Done=true` on final chunk

**Error mapping:**
- 429 → `ErrRateLimit`
- 401/403 → `ErrAuth`
- Timeout → `ErrTimeout`

### File 5: `internal/llm/anthropic.go`

Implement Anthropic adapter using `github.com/anthropics/anthropic-sdk-go`:

**Key difference:** System prompt is a separate parameter, NOT in the messages array.

**Constructor:** `func NewAnthropicProvider(cfg config.LLMConfig) (Provider, error)`

**Chat(ctx, messages, tools):**
1. Convert `[]ToolDefinition` → Anthropic `ToolParam` with `InputSchema`
2. Split messages: extract system message → `System` parameter; rest → `Messages`
3. Call `client.Messages.New(ctx, params)`
4. Parse ContentBlocks: type "text" → Content, type "tool_use" → ToolCall

**ChatStream:** similar, use streaming API

**Error mapping:** similar to OpenAI

### File 6: `internal/llm/gemini.go`

Implement Gemini adapter using `google.golang.org/genai` (new GA SDK, NOT legacy `generative-ai-go`):

**Key difference:** FunctionCall parameters are `map[string]any`, not JSON string.

**Constructor:** `func NewGeminiProvider(cfg config.LLMConfig) (Provider, error)`

**Chat(ctx, messages, tools):**
1. Convert `[]ToolDefinition` → `genai.FunctionDeclaration` with `genai.Schema`
2. Convert `[]Message` → Gemini Content format
3. Call API
4. Parse response Parts: `FunctionCall` → ToolCall
   - **Gemini-specific**: `FunctionCall.Args` is `map[string]any`
   - Convert to `ToolCall.Arguments map[string]string` via `fmt.Sprintf("%v", v)` per key
   - Store original as JSON in `ToolCall.RawArgs`

**ChatStream:** similar, use streaming API

### ~~File 7: prompt.go~~ — REMOVED

> System prompt building is the **agent module's** responsibility (`internal/agent/prompt.go`).
> It uses `agent.ClusterContext` (Layer 2 type) and `sendLogs` config — neither belongs in the LLM layer.
> See `wave3_agent.md` File 3 for the correct location.

### Files 7-9: Tests

**`openai_test.go`**, **`anthropic_test.go`**, **`gemini_test.go`**:
- Use `httptest.NewServer` to mock each provider's API
- Test: Chat with tool definitions → verify ToolCall parsing
- Test: Chat without tools → verify text response
- Test: ChatStream → verify StreamDelta sequence and Done flag
- Test: Error mapping (rate limit, auth failure, timeout)
- Test: Tool-call round-trip (send tools → parse call → send result → parse response)
- Test (Gemini only): `map[string]any` → `map[string]string` + RawArgs conversion

## Step 3: Verify

```bash
go test -v -race ./internal/llm/...
go vet ./internal/llm/...
```

## Critical Rules

- **Only import `internal/config`** — never import k8s, agent, or ui
- **SDK types don't leak**: public methods return only `provider.go` types (Message, ToolCall, etc.)
- **Adapters are independent**: `openai.go` does NOT import `anthropic.go` or `gemini.go`
- **Custom provider reuses OpenAI**: don't create a separate file, just use custom BaseURL in `openai.go`
- **All SDK errors → standard types**: use `errors.go` sentinel errors, use `errors.Is/As` not string matching
- **Gemini RawArgs**: always store original `map[string]any` as JSON string in `ToolCall.RawArgs`
- **Use `slog` for debug logging**: `slog.Debug("llm request", "provider", name, "tools", len(tools))`, `slog.Warn("rate limited", "provider", name, "error", err)`. Bubble Tea occupies stdout — slog writes to file (configured by app layer). See SPEC §6.8
- **Adapters do NOT retry**: classify errors via `mapXxxError` → sentinel errors (`ErrRateLimit`, `ErrAuth`, etc.). Retry is the agent layer's responsibility (see agent/CLAUDE.md Rule #8)
