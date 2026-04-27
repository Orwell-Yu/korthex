package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	"github.com/Orwell-Yu/korthex/internal/config"
)

// newGeminiTestProvider creates a geminiProvider with a mock server.
// The Gemini SDK requires creating the client via genai.NewClient, so we
// create a real client pointing at our test server.
func newGeminiTestProvider(t *testing.T, handler http.HandlerFunc) (*httptest.Server, Provider) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:     "test-key",
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: server.Client(),
		HTTPOptions: genai.HTTPOptions{
			BaseURL: server.URL,
		},
	})
	require.NoError(t, err)

	return server, &geminiProvider{
		client: client,
		model:  "gemini-2.5-flash",
	}
}

func TestGemini_Chat_TextResponse(t *testing.T) {
	_, provider := newGeminiTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "Hello from Gemini!"},
						},
						"role": "model",
					},
					"finishReason": "STOP",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	msg, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, RoleAssistant, msg.Role)
	assert.Equal(t, "Hello from Gemini!", msg.Content)
	assert.Empty(t, msg.ToolCalls)
}

func TestGemini_Chat_FunctionCall(t *testing.T) {
	_, provider := newGeminiTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{
								"functionCall": map[string]any{
									"name": "kubectl_get_pods",
									"args": map[string]any{
										"namespace":     "default",
										"labelSelector": "app=web",
									},
								},
							},
						},
						"role": "model",
					},
					"finishReason": "STOP",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

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
	assert.Equal(t, "kubectl_get_pods", msg.ToolCalls[0].Name)
	assert.Equal(t, "default", msg.ToolCalls[0].Arguments["namespace"])
	assert.Equal(t, "app=web", msg.ToolCalls[0].Arguments["labelSelector"])
	assert.NotEmpty(t, msg.ToolCalls[0].RawArgs)
}

func TestGemini_Chat_FunctionCall_MapStringAny_Conversion(t *testing.T) {
	// Gemini returns map[string]any with potentially non-string values.
	// Test that numeric, boolean values are converted to string correctly.
	_, provider := newGeminiTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{
								"functionCall": map[string]any{
									"name": "kubectl_get_pods",
									"args": map[string]any{
										"namespace":   "default",
										"tailLines":   float64(100), // JSON numbers become float64
										"previous":    true,
										"grepPattern": "error|warning",
									},
								},
							},
						},
						"role": "model",
					},
					"finishReason": "STOP",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	msg, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "show logs"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, msg.ToolCalls, 1)
	tc := msg.ToolCalls[0]

	// All values should be converted to strings
	assert.Equal(t, "default", tc.Arguments["namespace"])
	assert.Equal(t, "100", tc.Arguments["tailLines"])
	assert.Equal(t, "true", tc.Arguments["previous"])
	assert.Equal(t, "error|warning", tc.Arguments["grepPattern"])

	// RawArgs should be valid JSON preserving original types
	var rawArgs map[string]any
	err = json.Unmarshal([]byte(tc.RawArgs), &rawArgs)
	require.NoError(t, err)
	assert.Equal(t, float64(100), rawArgs["tailLines"])
	assert.Equal(t, true, rawArgs["previous"])
}

func TestGemini_Chat_ToolCallRoundTrip(t *testing.T) {
	callCount := 0
	_, provider := newGeminiTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]any
		if callCount == 1 {
			resp = map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]any{
								{
									"functionCall": map[string]any{
										"name": "kubectl_get_namespaces",
										"args": map[string]any{},
									},
								},
							},
							"role": "model",
						},
						"finishReason": "STOP",
					},
				},
			}
		} else {
			resp = map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]any{
								{"text": "Found 3 namespaces: default, kube-system, monitoring"},
							},
							"role": "model",
						},
						"finishReason": "STOP",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

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
		{Role: RoleTool, Name: "kubectl_get_namespaces", Content: "default, kube-system, monitoring"},
	}, tools)
	require.NoError(t, err)
	assert.Contains(t, msg2.Content, "namespaces")
}

func TestGemini_ChatStream(t *testing.T) {
	_, provider := newGeminiTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Gemini streaming returns JSON lines
		chunks := []map[string]any{
			{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]any{{"text": "Hello"}},
							"role":  "model",
						},
					},
				},
			},
			{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]any{{"text": " world"}},
							"role":  "model",
						},
						"finishReason": "STOP",
					},
				},
			},
		}

		// Gemini uses newline-delimited JSON for streaming
		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			w.Write([]byte("data: "))
			w.Write(data)
			w.Write([]byte("\r\n\r\n"))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	ch := make(chan StreamDelta, 10)
	err := provider.ChatStream(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil, ch)
	require.NoError(t, err)

	var deltas []StreamDelta
	for d := range ch {
		deltas = append(deltas, d)
	}

	require.NotEmpty(t, deltas)

	var content string
	for _, d := range deltas {
		content += d.Content
	}
	assert.Contains(t, content, "Hello")
}

func TestGemini_Error_RateLimit(t *testing.T) {
	err := mapGeminiError(genai.APIError{Code: 429, Message: "Rate limit exceeded"})
	require.Error(t, err)
	assert.True(t, IsRateLimit(err))
}

func TestGemini_Error_Auth(t *testing.T) {
	err := mapGeminiError(genai.APIError{Code: 401, Message: "Invalid API key"})
	require.Error(t, err)
	assert.True(t, IsAuth(err))

	err = mapGeminiError(genai.APIError{Code: 403, Message: "Forbidden"})
	require.Error(t, err)
	assert.True(t, IsAuth(err))
}

func TestGemini_Error_ModelUnavailable(t *testing.T) {
	err := mapGeminiError(genai.APIError{Code: 404, Message: "Model not found"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrModelUnavailable)
}

func TestGemini_Error_Timeout(t *testing.T) {
	err := mapGeminiError(context.DeadlineExceeded)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTimeout)
}

func TestGemini_ModelName_ProviderName(t *testing.T) {
	_, provider := newGeminiTestProvider(t, func(w http.ResponseWriter, r *http.Request) {})
	assert.Equal(t, "gemini-2.5-flash", provider.ModelName())
	assert.Equal(t, "gemini", provider.ProviderName())
}

func TestGemini_EmptyAPIKey(t *testing.T) {
	_, err := NewGeminiProvider(config.LLMConfig{
		Provider: "gemini",
		Model:    "gemini-2.5-flash",
	})
	require.Error(t, err)
	assert.True(t, IsAuth(err))
}

func TestGemini_ConvertGeminiFunctionCall(t *testing.T) {
	fc := &genai.FunctionCall{
		Name: "test_func",
		ID:   "call_1",
		Args: map[string]any{
			"str_param":  "hello",
			"int_param":  float64(42),
			"bool_param": true,
		},
	}

	tc := convertGeminiFunctionCall(fc)

	assert.Equal(t, "call_1", tc.ID)
	assert.Equal(t, "test_func", tc.Name)
	assert.Equal(t, "hello", tc.Arguments["str_param"])
	assert.Equal(t, "42", tc.Arguments["int_param"])
	assert.Equal(t, "true", tc.Arguments["bool_param"])

	// RawArgs should preserve original types
	var raw map[string]any
	err := json.Unmarshal([]byte(tc.RawArgs), &raw)
	require.NoError(t, err)
	assert.Equal(t, float64(42), raw["int_param"])
	assert.Equal(t, true, raw["bool_param"])
}
