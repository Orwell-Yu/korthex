package agent

import "context"

// MockAgent is a test double for the Agent interface.
type MockAgent struct {
	ExecuteFunc            func(ctx context.Context, userQuery string, ch chan<- AgentEvent) error
	ClearHistoryFunc       func()
	SetClusterContextFunc  func(ctx ClusterContext)
	SetLogBufferReaderFunc func(reader LogBufferReader)
}

func (m *MockAgent) Execute(ctx context.Context, userQuery string, ch chan<- AgentEvent) error {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, userQuery, ch)
	}
	return nil
}

func (m *MockAgent) ClearHistory() {
	if m.ClearHistoryFunc != nil {
		m.ClearHistoryFunc()
	}
}

func (m *MockAgent) SetClusterContext(ctx ClusterContext) {
	if m.SetClusterContextFunc != nil {
		m.SetClusterContextFunc(ctx)
	}
}

func (m *MockAgent) SetLogBufferReader(reader LogBufferReader) {
	if m.SetLogBufferReaderFunc != nil {
		m.SetLogBufferReaderFunc(reader)
	}
}
