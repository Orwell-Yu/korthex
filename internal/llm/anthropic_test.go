package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orwell-Yu/korthex/internal/config"
)

// newAnthropicTestServer creates a mock Anthropic API server.
func newAnthropicTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, config.LLMConfig) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.LLMConfig{
		Provider:  "anthropic",
		APIKey:    "test-key",
		Model:     "claude-sonnet-4-20250514",
		BaseURL:   server.URL,
		MaxTokens: 1024,
	}
	return server, cfg
}

func TestAnthropic_Chat_TextResponse(t *testing.T) {
	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "msg_test",
			"type":  "message",
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "Hello from Claude!"},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)

	msg, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, RoleAssistant, msg.Role)
	assert.Equal(t, "Hello from Claude!", msg.Content)
	assert.Empty(t, msg.ToolCalls)
}

func TestAnthropic_Chat_SystemPromptSeparation(t *testing.T) {
	var receivedBody map[string]any

	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)

		resp := map[string]any{
			"id":    "msg_test",
			"type":  "message",
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "OK"},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)

	_, err = provider.Chat(context.Background(), []Message{
		{Role: RoleSystem, Content: "You are a K8s assistant"},
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	require.NoError(t, err)

	// Verify system prompt is in the system field, not in messages
	systemField, ok := receivedBody["system"]
	require.True(t, ok, "system field should be present in request")
	systemBlocks, ok := systemField.([]any)
	require.True(t, ok)
	require.Len(t, systemBlocks, 1)
	firstBlock := systemBlocks[0].(map[string]any)
	assert.Equal(t, "You are a K8s assistant", firstBlock["text"])

	// Verify messages don't contain system
	msgs := receivedBody["messages"].([]any)
	for _, m := range msgs {
		msgMap := m.(map[string]any)
		assert.NotEqual(t, "system", msgMap["role"])
	}
}

func TestAnthropic_Chat_ToolCall(t *testing.T) {
	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "msg_test",
			"type":  "message",
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "Let me check the pods."},
				{
					"type":  "tool_use",
					"id":    "toolu_123",
					"name":  "kubectl_get_pods",
					"input": map[string]any{"namespace": "default"},
				},
			},
			"stop_reason": "tool_use",
			"usage":       map[string]any{"input_tokens": 20, "output_tokens": 15},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)

	tools := []ToolDefinition{
		{
			Name:        "kubectl_get_pods",
			Description: "Get pods in a namespace",
			Parameters: []ParameterDef{
				{Name: "namespace", Type: "string", Description: "Namespace", Required: true},
			},
		},
	}

	msg, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "show pods"},
	}, tools)

	require.NoError(t, err)
	assert.Contains(t, msg.Content, "check the pods")
	require.Len(t, msg.ToolCalls, 1)
	assert.Equal(t, "toolu_123", msg.ToolCalls[0].ID)
	assert.Equal(t, "kubectl_get_pods", msg.ToolCalls[0].Name)
	assert.Equal(t, "default", msg.ToolCalls[0].Arguments["namespace"])
	assert.NotEmpty(t, msg.ToolCalls[0].RawArgs)
}

func TestAnthropic_Chat_ToolCallRoundTrip(t *testing.T) {
	callCount := 0
	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]any
		if callCount == 1 {
			resp = map[string]any{
				"id":    "msg_1",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-sonnet-4-20250514",
				"content": []map[string]any{
					{
						"type":  "tool_use",
						"id":    "toolu_abc",
						"name":  "kubectl_get_namespaces",
						"input": map[string]any{},
					},
				},
				"stop_reason": "tool_use",
				"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
			}
		} else {
			resp = map[string]any{
				"id":    "msg_2",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-sonnet-4-20250514",
				"content": []map[string]any{
					{"type": "text", "text": "Found 3 namespaces"},
				},
				"stop_reason": "end_turn",
				"usage":       map[string]any{"input_tokens": 30, "output_tokens": 10},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)

	tools := []ToolDefinition{
		{Name: "kubectl_get_namespaces", Description: "Get namespaces"},
	}

	msg1, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "list namespaces"},
	}, tools)
	require.NoError(t, err)
	require.Len(t, msg1.ToolCalls, 1)

	msg2, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "list namespaces"},
		{Role: RoleAssistant, ToolCalls: msg1.ToolCalls},
		{Role: RoleTool, ToolCallID: msg1.ToolCalls[0].ID, Name: "kubectl_get_namespaces", Content: "default, kube-system, monitoring"},
	}, tools)
	require.NoError(t, err)
	assert.Contains(t, msg2.Content, "namespaces")
}

func TestAnthropic_ChatStream(t *testing.T) {
	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
			`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			`event: message_stop
data: {"type":"message_stop"}`,
		}

		for _, event := range events {
			w.Write([]byte(event + "\n\n"))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)

	ch := make(chan StreamDelta, 10)
	err = provider.ChatStream(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil, ch)
	require.NoError(t, err)

	var deltas []StreamDelta
	for d := range ch {
		deltas = append(deltas, d)
	}

	require.NotEmpty(t, deltas)

	var content string
	var gotDone bool
	for _, d := range deltas {
		content += d.Content
		if d.Done {
			gotDone = true
			assert.Equal(t, "end_turn", d.StopReason)
		}
	}
	assert.Equal(t, "Hello world", content)
	assert.True(t, gotDone)
}

func TestAnthropic_ChatStream_ToolUse(t *testing.T) {
	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_stream","name":"kubectl_get_pods"}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"namespace\""}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":":\"default\"}"}}`,
			`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":15}}`,
			`event: message_stop
data: {"type":"message_stop"}`,
		}

		for _, event := range events {
			w.Write([]byte(event + "\n\n"))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)

	ch := make(chan StreamDelta, 10)
	err = provider.ChatStream(context.Background(), []Message{
		{Role: RoleUser, Content: "show pods"},
	}, nil, ch)
	require.NoError(t, err)

	var deltas []StreamDelta
	for d := range ch {
		deltas = append(deltas, d)
	}

	// Should have a tool call and a done delta
	var gotToolCall bool
	var gotDone bool
	for _, d := range deltas {
		if d.ToolCall != nil {
			gotToolCall = true
			assert.Equal(t, "toolu_stream", d.ToolCall.ID)
			assert.Equal(t, "kubectl_get_pods", d.ToolCall.Name)
			assert.Equal(t, "default", d.ToolCall.Arguments["namespace"])
		}
		if d.Done {
			gotDone = true
			assert.Equal(t, "tool_use", d.StopReason)
		}
	}
	assert.True(t, gotToolCall, "should have received a tool call")
	assert.True(t, gotDone, "should have received done signal")
}

func TestAnthropic_Error_RateLimit(t *testing.T) {
	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "rate_limit_error",
				"message": "Rate limit exceeded",
			},
		})
	})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)

	_, err = provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	require.Error(t, err)
	assert.True(t, IsRateLimit(err), "expected rate limit error, got: %v", err)
}

func TestAnthropic_Error_Auth(t *testing.T) {
	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "authentication_error",
				"message": "Invalid API key",
			},
		})
	})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)

	_, err = provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	require.Error(t, err)
	assert.True(t, IsAuth(err), "expected auth error, got: %v", err)
}

func TestAnthropic_ModelName_ProviderName(t *testing.T) {
	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-20250514", provider.ModelName())
	assert.Equal(t, "anthropic", provider.ProviderName())
}

func TestAnthropic_EmptyAPIKey(t *testing.T) {
	_, err := NewAnthropicProvider(config.LLMConfig{
		Provider: "anthropic",
		Model:    "claude-sonnet-4-20250514",
	})
	require.Error(t, err)
	assert.True(t, IsAuth(err))
}

func TestAnthropic_Chat_TokenUsage(t *testing.T) {
	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "msg_test",
			"type":  "message",
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "Hello!"},
			},
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":                200,
				"output_tokens":               50,
				"cache_read_input_tokens":      120,
				"cache_creation_input_tokens":  80,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)

	msg, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, 200, msg.Usage.InputTokens)
	assert.Equal(t, 50, msg.Usage.OutputTokens)
	assert.Equal(t, 120, msg.Usage.CacheReadTokens)
	assert.Equal(t, 80, msg.Usage.CacheWriteTokens)
}

func TestAnthropic_ChatStream_TokenUsage(t *testing.T) {
	_, cfg := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":100,"output_tokens":0}}}`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
			`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":100,"output_tokens":10,"cache_read_input_tokens":60,"cache_creation_input_tokens":40}}`,
			`event: message_stop
data: {"type":"message_stop"}`,
		}

		for _, event := range events {
			w.Write([]byte(event + "\n\n"))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	provider, err := NewAnthropicProvider(cfg)
	require.NoError(t, err)

	ch := make(chan StreamDelta, 10)
	err = provider.ChatStream(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil, ch)
	require.NoError(t, err)

	var usage *TokenUsage
	for d := range ch {
		if d.Usage != nil {
			usage = d.Usage
		}
	}

	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.InputTokens)
	assert.Equal(t, 10, usage.OutputTokens)
	assert.Equal(t, 60, usage.CacheReadTokens)
	assert.Equal(t, 40, usage.CacheWriteTokens)
}
