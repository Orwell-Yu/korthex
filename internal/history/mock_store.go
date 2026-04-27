package history

import "context"

// MockStore is a test double for the Store interface.
type MockStore struct {
	CreateSessionFunc func(ctx context.Context, cluster string) (string, error)
	EndSessionFunc    func(ctx context.Context, sessionID, summary string) error
	AddMessageFunc    func(ctx context.Context, sessionID string, msg Message) error
	LoadSessionFunc   func(ctx context.Context, sessionID string) ([]Message, error)
	SearchFunc        func(ctx context.Context, query SearchQuery) ([]SearchResult, error)
	CleanExpiredFunc  func(ctx context.Context, retentionDays int) error
	CloseFunc         func() error
}

func (m *MockStore) CreateSession(ctx context.Context, cluster string) (string, error) {
	if m.CreateSessionFunc != nil {
		return m.CreateSessionFunc(ctx, cluster)
	}
	return "mock-session-id", nil
}

func (m *MockStore) EndSession(ctx context.Context, sessionID, summary string) error {
	if m.EndSessionFunc != nil {
		return m.EndSessionFunc(ctx, sessionID, summary)
	}
	return nil
}

func (m *MockStore) AddMessage(ctx context.Context, sessionID string, msg Message) error {
	if m.AddMessageFunc != nil {
		return m.AddMessageFunc(ctx, sessionID, msg)
	}
	return nil
}

func (m *MockStore) LoadSession(ctx context.Context, sessionID string) ([]Message, error) {
	if m.LoadSessionFunc != nil {
		return m.LoadSessionFunc(ctx, sessionID)
	}
	return nil, nil
}

func (m *MockStore) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	if m.SearchFunc != nil {
		return m.SearchFunc(ctx, query)
	}
	return nil, nil
}

func (m *MockStore) CleanExpired(ctx context.Context, retentionDays int) error {
	if m.CleanExpiredFunc != nil {
		return m.CleanExpiredFunc(ctx, retentionDays)
	}
	return nil
}

func (m *MockStore) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}
