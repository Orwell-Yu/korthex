package agent

import (
	"testing"

	"github.com/Orwell-Yu/korthex/internal/llm"
	"github.com/stretchr/testify/assert"
)

func TestAgentMetrics_Add(t *testing.T) {
	m := &AgentMetrics{MaxContext: 100000}

	m.Add(llm.TokenUsage{
		InputTokens:      1000,
		OutputTokens:     500,
		CacheReadTokens:  200,
		CacheWriteTokens: 100,
	})

	assert.Equal(t, 1000, m.TotalInput)
	assert.Equal(t, 500, m.TotalOutput)
	assert.Equal(t, 300, m.TotalCache)
	assert.InDelta(t, 0.01, m.ContextUsage, 0.001)

	// Add more
	m.Add(llm.TokenUsage{
		InputTokens:  2000,
		OutputTokens: 300,
	})

	assert.Equal(t, 3000, m.TotalInput)
	assert.Equal(t, 800, m.TotalOutput)
	assert.Equal(t, 300, m.TotalCache) // unchanged
	assert.InDelta(t, 0.03, m.ContextUsage, 0.001)
}

func TestAgentMetrics_Reset(t *testing.T) {
	m := &AgentMetrics{
		Iterations:   5,
		TotalInput:   10000,
		TotalOutput:  3000,
		TotalCache:   2000,
		ContextUsage: 0.5,
		MaxContext:    200000,
	}

	m.Reset()

	assert.Equal(t, 0, m.Iterations)
	assert.Equal(t, 0, m.TotalInput)
	assert.Equal(t, 0, m.TotalOutput)
	assert.Equal(t, 0, m.TotalCache)
	assert.Equal(t, 0.0, m.ContextUsage)
	assert.Equal(t, 200000, m.MaxContext) // preserved
}

func TestAgentMetrics_FormatHeader(t *testing.T) {
	tests := []struct {
		name     string
		metrics  AgentMetrics
		expected string
	}{
		{
			name:     "zero iterations",
			metrics:  AgentMetrics{},
			expected: "",
		},
		{
			name: "small numbers no cache",
			metrics: AgentMetrics{
				Iterations:  1,
				TotalInput:  500,
				TotalOutput: 200,
			},
			expected: "iter:1 │ in:500 out:200",
		},
		{
			name: "large numbers with cache and context",
			metrics: AgentMetrics{
				Iterations:   3,
				TotalInput:   2100,
				TotalOutput:  847,
				TotalCache:   1500,
				ContextUsage: 0.65,
				MaxContext:    128000,
			},
			expected: "iter:3 │ in:2.1k out:847 cache:1.5k │ ctx:65%",
		},
		{
			name: "no context size",
			metrics: AgentMetrics{
				Iterations:  2,
				TotalInput:  5000,
				TotalOutput: 1200,
				TotalCache:  3000,
			},
			expected: "iter:2 │ in:5.0k out:1.2k cache:3.0k",
		},
		{
			name: "cache zero hidden",
			metrics: AgentMetrics{
				Iterations:   1,
				TotalInput:   1500,
				TotalOutput:  300,
				TotalCache:   0,
				ContextUsage: 0.01,
				MaxContext:    128000,
			},
			expected: "iter:1 │ in:1.5k out:300 │ ctx:1%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.metrics.FormatHeader())
		})
	}
}

func TestAgentMetrics_FormatHeaderProgressive(t *testing.T) {
	m := AgentMetrics{
		Iterations:   3,
		TotalInput:   2100,
		TotalOutput:  847,
		TotalCache:   1500,
		ContextUsage: 0.65,
		MaxContext:    128000,
	}

	// Full format
	full := m.FormatHeaderProgressive(100)
	assert.Equal(t, "iter:3 │ in:2.1k out:847 cache:1.5k │ ctx:65%", full)

	// Too narrow for full → hide cache
	noCache := m.FormatHeaderProgressive(40)
	assert.Equal(t, "iter:3 │ in:2.1k out:847 │ ctx:65%", noCache)

	// Even narrower → hide ctx
	basic := m.FormatHeaderProgressive(30)
	assert.Equal(t, "iter:3 │ in:2.1k out:847", basic)

	// Very narrow → minimal
	minimal := m.FormatHeaderProgressive(8)
	assert.Equal(t, "iter:3", minimal)

	// Too narrow for anything
	empty := m.FormatHeaderProgressive(3)
	assert.Equal(t, "", empty)
}

func TestFmtTokens(t *testing.T) {
	tests := []struct {
		n        int
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{10000, "10.0k"},
		{100000, "100.0k"},
		{2100, "2.1k"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, fmtTokens(tt.n))
	}
}

func TestLookupContextSize(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{"gpt-4o", 128000},
		{"gpt-4o-2024-05-13", 128000},
		{"claude-sonnet-4-20250514", 200000},
		{"gemini-2.5-flash", 1048576},
		{"unknown-model", 0},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.expected, LookupContextSize(tt.model))
		})
	}
}
