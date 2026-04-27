package history

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	store, err := NewMemoryStore()
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCreateSessionAndAddMessage(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sid, err := store.CreateSession(ctx, "test-cluster")
	require.NoError(t, err)
	assert.NotEmpty(t, sid)

	err = store.AddMessage(ctx, sid, Message{
		Role:      "user",
		Content:   "why is my pod crashing?",
		Namespace: "production",
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	err = store.AddMessage(ctx, sid, Message{
		Role:      "assistant",
		Content:   "I found the issue: OOMKilled due to memory limit.",
		Namespace: "production",
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	// Verify via search.
	results, err := store.Search(ctx, SearchQuery{Keyword: "crashing"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, sid, results[0].SessionID)
	assert.Equal(t, "test-cluster", results[0].Cluster)
	assert.Len(t, results[0].Messages, 1)
	assert.Equal(t, "user", results[0].Messages[0].Role)
}

func TestEndSession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sid, err := store.CreateSession(ctx, "my-cluster")
	require.NoError(t, err)

	err = store.EndSession(ctx, sid, "Investigated OOMKill in production.")
	require.NoError(t, err)

	// EndSession should update but not fail if called again.
	err = store.EndSession(ctx, sid, "Updated summary.")
	require.NoError(t, err)
}

func TestFTS5KeywordSearch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sid, _ := store.CreateSession(ctx, "cluster-a")
	store.AddMessage(ctx, sid, Message{
		Role:    "user",
		Content: "show me timeout errors in order-service",
	})
	store.AddMessage(ctx, sid, Message{
		Role:    "assistant",
		Content: "Found 5 timeout errors in the last hour.",
	})
	store.AddMessage(ctx, sid, Message{
		Role:    "user",
		Content: "what about memory usage?",
	})

	tests := []struct {
		keyword string
		count   int
	}{
		{"timeout", 2}, // matches both "timeout" messages
		{"memory", 1},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.keyword, func(t *testing.T) {
			results, err := store.Search(ctx, SearchQuery{Keyword: tt.keyword})
			require.NoError(t, err)
			total := 0
			for _, r := range results {
				total += len(r.Messages)
			}
			assert.Equal(t, tt.count, total)
		})
	}
}

func TestFTS5NamespaceFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sid, _ := store.CreateSession(ctx, "cluster")

	store.AddMessage(ctx, sid, Message{
		Role:      "user",
		Content:   "check error logs",
		Namespace: "production",
	})
	store.AddMessage(ctx, sid, Message{
		Role:      "user",
		Content:   "check error logs",
		Namespace: "staging",
	})

	results, err := store.Search(ctx, SearchQuery{
		Keyword:   "error",
		Namespace: "production",
	})
	require.NoError(t, err)

	total := 0
	for _, r := range results {
		total += len(r.Messages)
	}
	assert.Equal(t, 1, total)
}

func TestFTS5TimeFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sid, _ := store.CreateSession(ctx, "cluster")

	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)

	store.AddMessage(ctx, sid, Message{
		Role: "user", Content: "old error", CreatedAt: old,
	})
	store.AddMessage(ctx, sid, Message{
		Role: "user", Content: "recent error", CreatedAt: recent,
	})

	results, err := store.Search(ctx, SearchQuery{
		Keyword: "error",
		After:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	total := 0
	for _, r := range results {
		total += len(r.Messages)
	}
	assert.Equal(t, 1, total)
}

func TestCleanExpired(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create an "old" session using the proper API, then backdate it.
	oldSid, err := store.CreateSession(ctx, "cluster")
	require.NoError(t, err)
	store.AddMessage(ctx, oldSid, Message{Role: "user", Content: "old message for cleanup"})

	// Backdate the session to 31 days ago.
	s := store.(*sqliteStore)
	oldTime := time.Now().UTC().AddDate(0, 0, -31).Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `UPDATE sessions SET started_at = ? WHERE id = ?`, oldTime, oldSid)
	require.NoError(t, err)

	// Create a recent session.
	sid, _ := store.CreateSession(ctx, "cluster")
	store.AddMessage(ctx, sid, Message{Role: "user", Content: "recent message"})

	err = store.CleanExpired(ctx, 30)
	require.NoError(t, err)

	// Old session messages should be gone.
	results, err := store.Search(ctx, SearchQuery{Keyword: "cleanup"})
	require.NoError(t, err)
	assert.Empty(t, results)

	// Recent session should still exist.
	results, err = store.Search(ctx, SearchQuery{Keyword: "recent"})
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestConcurrentWrites(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sid, _ := store.CreateSession(ctx, "cluster")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				store.AddMessage(ctx, sid, Message{
					Role:    "user",
					Content: "concurrent write test message",
				})
			}
		}(i)
	}
	wg.Wait()

	// All 100 messages should be searchable.
	results, err := store.Search(ctx, SearchQuery{Keyword: "concurrent", Limit: 200})
	require.NoError(t, err)
	total := 0
	for _, r := range results {
		total += len(r.Messages)
	}
	assert.Equal(t, 100, total)
}

func TestCompressToolResult(t *testing.T) {
	// Short content: no compression.
	short := "hello world"
	assert.Equal(t, short, CompressToolResult(short))

	// Long content: compressed.
	long := strings.Repeat("x", 3000)
	compressed := CompressToolResult(long)
	assert.True(t, len(compressed) < len(long))
	assert.Contains(t, compressed, "[compressed:")
	assert.True(t, strings.HasPrefix(compressed, strings.Repeat("x", 500)))
}

func TestToolResultCompression(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sid, _ := store.CreateSession(ctx, "cluster")

	longResult := strings.Repeat("error log line\n", 200)
	err := store.AddMessage(ctx, sid, Message{
		Role:    "tool_result",
		Content: longResult,
	})
	require.NoError(t, err)

	// Search for part of the truncated content.
	results, err := store.Search(ctx, SearchQuery{Keyword: "error"})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Contains(t, results[0].Messages[0].Content, "[compressed:")
}

func TestEmptyKeywordSearch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	results, err := store.Search(ctx, SearchQuery{Keyword: ""})
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSearchResultOrdering(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sid, _ := store.CreateSession(ctx, "cluster")

	t1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)

	store.AddMessage(ctx, sid, Message{
		Role: "user", Content: "first error", CreatedAt: t1,
	})
	store.AddMessage(ctx, sid, Message{
		Role: "user", Content: "second error", CreatedAt: t2,
	})

	results, err := store.Search(ctx, SearchQuery{Keyword: "error"})
	require.NoError(t, err)
	require.Len(t, results, 1) // same session
	require.Len(t, results[0].Messages, 2)
	// Most recent first (ORDER BY created_at DESC).
	assert.Contains(t, results[0].Messages[0].Content, "second")
	assert.Contains(t, results[0].Messages[1].Content, "first")
}
