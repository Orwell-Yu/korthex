package llm

import (
	"context"

	"github.com/Orwell-Yu/korthex/internal/config"
)

// Role represents the role of a message participant.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// TokenUsage holds token consumption data from an LLM response.
type TokenUsage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

// Message is the cross-provider unified message format.
type Message struct {
	Role       Role
	Content    string     // text content (may be empty when ToolCalls present)
	ToolCalls  []ToolCall // assistant requesting tool execution
	ToolCallID string     // RoleTool: which ToolCall this responds to
	Name       string     // RoleTool: tool name
	Usage      TokenUsage // token usage from this response (Phase 3)
}

// ToolCall represents a tool invocation requested by the LLM.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]string // Phase 1: all 7 tools have string-only params
	RawArgs   string            // provider raw JSON — reserved for Phase 2+ extension
}

// ToolDefinition describes a tool available to the LLM.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  []ParameterDef
}

// ParameterDef describes a single parameter of a tool.
type ParameterDef struct {
	Name        string
	Type        string // "string", "integer", "boolean"
	Description string
	Required    bool
	Enum        []string // optional
}

// StreamDelta represents an incremental chunk of a streaming response.
type StreamDelta struct {
	Content    string
	ToolCall   *ToolCall
	Done       bool
	StopReason string      // "end_turn", "tool_use", etc.
	Usage      *TokenUsage // token usage (typically in final chunk)
}

// Provider is the unified abstraction for LLM communication.
type Provider interface {
	Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error)
	ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, ch chan<- StreamDelta) error
	ModelName() string
	ProviderName() string
	ValidateConnection(ctx context.Context) error
}

// Registry manages LLM provider construction.
type Registry interface {
	Create(cfg config.LLMConfig) (Provider, error)
	SupportedProviders() []string
}
