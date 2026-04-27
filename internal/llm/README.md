# internal/llm - LLM Integration

> **Parent docs:** [CLAUDE.md (root)](../../CLAUDE.md) | [SPEC.md Section 3.4](../../SPEC.md) | [PRD Section 6.1](../../PRD.md)
>
> **Module rules:** [CLAUDE.md (this module)](./CLAUDE.md)
>
> **Depends on:** [config](../config/README.md) | **Depended by:** [agent](../agent/README.md)

## Responsibility

Abstract multi-provider LLM communication behind a unified interface. Handle tool definitions in each SDK's native format, streaming responses, and error normalization. Consumers never touch SDK-specific types.

## Public Interfaces

```go
// Provider is the unified LLM abstraction
type Provider interface {
    Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error)
    ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, ch chan<- StreamDelta) error
    ModelName() string
    ProviderName() string  // "openai", "anthropic", "gemini"
    ValidateConnection(ctx context.Context) error
}

// Registry creates Provider instances by name
type Registry interface {
    Create(cfg config.LLMConfig) (Provider, error)
    SupportedProviders() []string
}
```

## Key Types

| Type | Purpose |
|------|---------|
| `Message` | Unified message (Role, Content, ToolCalls, ToolCallID) |
| `ToolCall` | Tool execution request from LLM (ID, Name, Arguments) |
| `ToolDefinition` | Tool schema for LLM (Name, Description, Parameters) |
| `ParameterDef` | Tool parameter definition (Name, Type, Description, Required, Enum) |
| `StreamDelta` | Incremental streaming chunk (Content, ToolCall, Done, StopReason) |
| `Role` | system / user / assistant / tool |

## Files

| File | Responsibility |
|------|---------------|
| `provider.go` | Provider interface, Message, ToolCall, ToolDefinition, StreamDelta types |
| `registry.go` | Registry implementation: provider name -> constructor mapping |
| `openai.go` | OpenAI adapter (github.com/openai/openai-go). Also handles Custom endpoints (custom BaseURL) |
| `anthropic.go` | Anthropic adapter (github.com/anthropics/anthropic-sdk-go). Handles system prompt separation |
| `gemini.go` | Gemini adapter (google.golang.org/genai). Translates FunctionDeclaration/FunctionCall |
| `prompt.go` | System prompt templates with cluster context injection, safety rules, output format |
| `errors.go` | Normalized error types: ErrRateLimit, ErrAuth, ErrTimeout, ErrModelUnavailable |
| `openai_test.go` | Tests with mock HTTP server + golden file responses |
| `anthropic_test.go` | Tests with mock HTTP server |
| `gemini_test.go` | Tests with mock HTTP server |

## Dependencies

- **External**: `github.com/openai/openai-go`, `github.com/anthropics/anthropic-sdk-go`, `google.golang.org/genai`
- **Internal**: `config` (for LLMConfig)

## Provider Adapter Responsibilities

Each adapter file (`openai.go`, `anthropic.go`, `gemini.go`) handles:

1. **ToolDefinition -> Native Schema**: Convert `[]ParameterDef` to provider's schema format
   - OpenAI: `ChatCompletionToolParam{Function: FunctionDefinitionParam{Parameters: jsonSchema}}`
   - Anthropic: `ToolParam{InputSchema: jsonSchema}`
   - Gemini: `FunctionDeclaration{Parameters: &genai.Schema{...}}`

2. **Message -> Native Format**: Convert unified messages to provider's structure
   - Note: Anthropic separates system prompt from messages (system is a top-level parameter)

3. **Native Response -> Message**: Parse provider response back to unified ToolCall/Content
   - OpenAI: `response.Choices[0].Message.ToolCalls` -> `[]ToolCall`
   - Anthropic: `ContentBlock.Type == "tool_use"` -> `ToolCall`
   - Gemini: `Part.FunctionCall` -> `ToolCall`

4. **Error Normalization**: Map SDK-specific errors to `ErrRateLimit`, `ErrAuth`, etc.
   - **Note**: Adapters do NOT retry. They classify errors; `internal/agent/` decides retry policy (see agent/CLAUDE.md Rule #8)

## Custom Endpoints

To use a proxy or gateway, select the matching provider (OpenAI or Anthropic) in the wizard and fill in the Base URL. The `"custom"` provider value is deprecated — existing configs with `provider: "custom"` are automatically treated as OpenAI-compatible for backward compatibility.

## Testing Strategy

- **Mock HTTP server** (`httptest.NewServer`): simulate each provider's API responses
- **Golden files**: recorded real API responses for deterministic replay
- **Error tests**: rate limit, auth failure, timeout, malformed response
- **Tool-call round-trip**: send tool definitions -> verify tool call parsing -> send tool result -> verify response

## Phase 2+ Extension Points

- Add `ollama.go` implementing Provider interface (Phase 3/4, deferred per P2-D1)
- Add token counting utilities for context window management
- Add provider-specific prompt optimizations in `prompt.go`
- Add response caching for identical queries
- Phase 2 tool definitions pass through unchanged — `ParameterDef.Type` already supports `integer`, `boolean`, `array` types
