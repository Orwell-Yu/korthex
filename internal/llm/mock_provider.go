package llm

import "context"

// MockProvider is a test double for the Provider interface.
type MockProvider struct {
	ChatFunc               func(ctx context.Context, msgs []Message, tools []ToolDefinition) (*Message, error)
	ChatStreamFunc         func(ctx context.Context, msgs []Message, tools []ToolDefinition, ch chan<- StreamDelta) error
	ModelNameFunc          func() string
	ProviderNameFunc       func() string
	ValidateConnectionFunc func(ctx context.Context) error
}

func (m *MockProvider) Chat(ctx context.Context, msgs []Message, tools []ToolDefinition) (*Message, error) {
	if m.ChatFunc != nil {
		return m.ChatFunc(ctx, msgs, tools)
	}
	return &Message{Role: RoleAssistant, Content: ""}, nil
}

func (m *MockProvider) ChatStream(ctx context.Context, msgs []Message, tools []ToolDefinition, ch chan<- StreamDelta) error {
	if m.ChatStreamFunc != nil {
		return m.ChatStreamFunc(ctx, msgs, tools, ch)
	}
	return nil
}

func (m *MockProvider) ModelName() string {
	if m.ModelNameFunc != nil {
		return m.ModelNameFunc()
	}
	return "mock-model"
}

func (m *MockProvider) ProviderName() string {
	if m.ProviderNameFunc != nil {
		return m.ProviderNameFunc()
	}
	return "mock"
}

func (m *MockProvider) ValidateConnection(ctx context.Context) error {
	if m.ValidateConnectionFunc != nil {
		return m.ValidateConnectionFunc(ctx)
	}
	return nil
}
