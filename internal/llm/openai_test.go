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

// newOpenAITestServer creates a mock OpenAI API server.
func newOpenAITestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, config.LLMConfig) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.LLMConfig{
		Provider: "openai",
		APIKey:   "test-key",
		Model:    "gpt-4",
		BaseURL:  server.URL,
	}
	return server, cfg
}

func TestOpenAI_Chat_TextResponse(t *testing.T) {
	_, cfg := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "chatcmpl-test",
			"model": "gpt-4",
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello, world!",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	provider, err := NewOpenAIProvider(cfg)
	require.NoError(t, err)

	msg, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, RoleAssistant, msg.Role)
	assert.Equal(t, "Hello, world!", msg.Content)
	assert.Empty(t, msg.ToolCalls)
}

func TestOpenAI_Chat_ToolCall(t *testing.T) {
	_, cfg := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "chatcmpl-test",
			"model": "gpt-4",
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "tool_calls",
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_123",
								"type": "function",
								"function": map[string]any{
									"name":      "kubectl_get_pods",
									"arguments": `{"namespace":"default","labelSelector":"app=web"}`,
								},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	provider, err := NewOpenAIProvider(cfg)
	require.NoError(t, err)

	tools := []ToolDefinition{
		{
			Name:        "kubectl_get_pods",
			Description: "Get pods in a namespace",
			Parameters: []ParameterDef{
				{Name: "namespace", Type: "string", Description: "Namespace", Required: true},
				{Name: "labelSelector", Type: "string", Description: "Label selector"},
			},
		},
	}

	msg, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "show pods"},
	}, tools)

	require.NoError(t, err)
	require.Len(t, msg.ToolCalls, 1)
	assert.Equal(t, "call_123", msg.ToolCalls[0].ID)
	assert.Equal(t, "kubectl_get_pods", msg.ToolCalls[0].Name)
	assert.Equal(t, "default", msg.ToolCalls[0].Arguments["namespace"])
	assert.Equal(t, "app=web", msg.ToolCalls[0].Arguments["labelSelector"])
	assert.NotEmpty(t, msg.ToolCalls[0].RawArgs)
}

func TestOpenAI_Chat_ToolCallRoundTrip(t *testing.T) {
	callCount := 0
	_, cfg := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]any
		if callCount == 1 {
			resp = map[string]any{
				"id":    "chatcmpl-1",
				"model": "gpt-4",
				"choices": []map[string]any{
					{
						"index":         0,
						"finish_reason": "tool_calls",
						"message": map[string]any{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]any{
								{
									"id":   "call_abc",
									"type": "function",
									"function": map[string]any{
										"name":      "kubectl_get_namespaces",
										"arguments": `{}`,
									},
								},
							},
						},
					},
				},
			}
		} else {
			resp = map[string]any{
				"id":    "chatcmpl-2",
				"model": "gpt-4",
				"choices": []map[string]any{
					{
						"index":         0,
						"finish_reason": "stop",
						"message": map[string]any{
							"role":    "assistant",
							"content": "Found 3 namespaces: default, kube-system, monitoring",
						},
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	provider, err := NewOpenAIProvider(cfg)
	require.NoError(t, err)

	tools := []ToolDefinition{
		{Name: "kubectl_get_namespaces", Description: "Get namespaces"},
	}

	// First call: get tool call
	msg1, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "list namespaces"},
	}, tools)
	require.NoError(t, err)
	require.Len(t, msg1.ToolCalls, 1)

	// Second call: send tool result
	msg2, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "list namespaces"},
		{Role: RoleAssistant, ToolCalls: msg1.ToolCalls},
		{Role: RoleTool, ToolCallID: msg1.ToolCalls[0].ID, Content: "default, kube-system, monitoring"},
	}, tools)
	require.NoError(t, err)
	assert.Contains(t, msg2.Content, "namespaces")
}

func TestOpenAI_ChatStream(t *testing.T) {
	_, cfg := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		chunks := []string{
			`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		}

		for _, chunk := range chunks {
			w.Write([]byte("data: " + chunk + "\n\n"))
		}
		w.Write([]byte("data: [DONE]\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	provider, err := NewOpenAIProvider(cfg)
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

	// Should have received content deltas and a final done delta
	require.NotEmpty(t, deltas)

	var content string
	var gotDone bool
	for _, d := range deltas {
		content += d.Content
		if d.Done {
			gotDone = true
		}
	}
	assert.Contains(t, content, "Hello")
	assert.True(t, gotDone)
}

func TestOpenAI_Error_RateLimit(t *testing.T) {
	_, cfg := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Rate limit exceeded",
				"type":    "rate_limit_error",
				"code":    "rate_limit_exceeded",
			},
		})
	})

	provider, err := NewOpenAIProvider(cfg)
	require.NoError(t, err)

	_, err = provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	require.Error(t, err)
	assert.True(t, IsRateLimit(err), "expected rate limit error, got: %v", err)
}

func TestOpenAI_Error_Auth(t *testing.T) {
	_, cfg := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Invalid API key",
				"type":    "invalid_request_error",
				"code":    "invalid_api_key",
			},
		})
	})

	provider, err := NewOpenAIProvider(cfg)
	require.NoError(t, err)

	_, err = provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	require.Error(t, err)
	assert.True(t, IsAuth(err), "expected auth error, got: %v", err)
}

func TestOpenAI_Error_Timeout(t *testing.T) {
	_, cfg := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Don't respond — the context will be cancelled
		<-r.Context().Done()
	})

	provider, err := NewOpenAIProvider(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = provider.Chat(ctx, []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	require.Error(t, err)
}

func TestOpenAI_ModelName_ProviderName(t *testing.T) {
	_, cfg := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {})

	provider, err := NewOpenAIProvider(cfg)
	require.NoError(t, err)
	assert.Equal(t, "gpt-4", provider.ModelName())
	assert.Equal(t, "openai", provider.ProviderName())
}

func TestOpenAI_CustomProvider(t *testing.T) {
	_, cfg := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "chatcmpl-test",
			"model": "local-model",
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]any{
						"role":    "assistant",
						"content": "custom response",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	cfg.Provider = "custom"
	cfg.Model = "local-model"

	provider, err := NewOpenAIProvider(cfg)
	require.NoError(t, err)
	assert.Equal(t, "custom", provider.ProviderName())

	msg, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "custom response", msg.Content)
}

func TestOpenAI_EmptyAPIKey(t *testing.T) {
	_, err := NewOpenAIProvider(config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4",
	})
	require.Error(t, err)
	assert.True(t, IsAuth(err))
}
