package agent

import (
	"fmt"

	"github.com/Orwell-Yu/korthex/internal/llm"
)

// AgentMetrics tracks token consumption across agentic loop iterations.
type AgentMetrics struct {
	Iterations   int
	TotalInput   int
	TotalOutput  int
	TotalCache   int
	ContextUsage float64 // 0.0 ~ 1.0
	MaxContext   int
}

// Add accumulates token usage from a single LLM response.
func (m *AgentMetrics) Add(usage llm.TokenUsage) {
	m.TotalInput += usage.InputTokens
	m.TotalOutput += usage.OutputTokens
	m.TotalCache += usage.CacheReadTokens + usage.CacheWriteTokens
	if m.MaxContext > 0 {
		m.ContextUsage = float64(m.TotalInput) / float64(m.MaxContext)
	}
}

// Reset zeroes all counters but preserves MaxContext.
func (m *AgentMetrics) Reset() {
	maxCtx := m.MaxContext
	*m = AgentMetrics{MaxContext: maxCtx}
}

// FormatHeader returns a formatted metrics string for the Chat panel header.
// Format: "iter:3 │ in:2.1k out:847 cache:1.5k │ ctx:65%"
// Rules:
//   - ≥1000 → "Nk" with one decimal (e.g. 2100 → "2.1k")
//   - cache == 0 → hide entire cache field
//   - MaxContext == 0 → hide ctx field
func (m *AgentMetrics) FormatHeader() string {
	if m.Iterations == 0 {
		return ""
	}

	s := fmt.Sprintf("iter:%d │ in:%s out:%s", m.Iterations, fmtTokens(m.TotalInput), fmtTokens(m.TotalOutput))

	if m.TotalCache > 0 {
		s += fmt.Sprintf(" cache:%s", fmtTokens(m.TotalCache))
	}

	if m.MaxContext > 0 {
		s += fmt.Sprintf(" │ ctx:%d%%", int(m.ContextUsage*100))
	}

	return s
}

// FormatHeaderProgressive returns a metrics string fitting within maxWidth.
// Progressive hiding order:
//  1. Full: "iter:3 │ in:2.1k out:847 cache:1.5k │ ctx:65%"
//  2. Hide cache: "iter:3 │ in:2.1k out:847 │ ctx:65%"
//  3. Hide ctx: "iter:3 │ in:2.1k out:847"
//  4. Minimal: "iter:3"
func (m *AgentMetrics) FormatHeaderProgressive(maxWidth int) string {
	if m.Iterations == 0 {
		return ""
	}

	// Try full format
	full := m.FormatHeader()
	if len(full) <= maxWidth {
		return full
	}

	// Hide cache
	noCache := fmt.Sprintf("iter:%d │ in:%s out:%s", m.Iterations, fmtTokens(m.TotalInput), fmtTokens(m.TotalOutput))
	if m.MaxContext > 0 {
		noCache += fmt.Sprintf(" │ ctx:%d%%", int(m.ContextUsage*100))
	}
	if len(noCache) <= maxWidth {
		return noCache
	}

	// Hide ctx too
	basic := fmt.Sprintf("iter:%d │ in:%s out:%s", m.Iterations, fmtTokens(m.TotalInput), fmtTokens(m.TotalOutput))
	if len(basic) <= maxWidth {
		return basic
	}

	// Minimal
	minimal := fmt.Sprintf("iter:%d", m.Iterations)
	if len(minimal) <= maxWidth {
		return minimal
	}

	return ""
}

// fmtTokens formats a token count: ≥1000 → "N.Nk", <1000 → plain number.
func fmtTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// modelContextSizes maps model name prefixes to context window sizes.
var modelContextSizes = map[string]int{
	// OpenAI
	"gpt-4o":           128000,
	"gpt-4-turbo":      128000,
	"gpt-4":            8192,
	"gpt-3.5-turbo":    16385,
	"o1":               200000,
	"o3":               200000,
	"o4-mini":          200000,
	// Anthropic
	"claude-opus-4":    200000,
	"claude-sonnet-4":  200000,
	"claude-haiku-4":   200000,
	"claude-3-5":       200000,
	"claude-3-opus":    200000,
	"claude-3-sonnet":  200000,
	"claude-3-haiku":   200000,
	// Gemini
	"gemini-2.5-pro":   1048576,
	"gemini-2.5-flash": 1048576,
	"gemini-2.0-flash": 1048576,
	"gemini-1.5-pro":   2097152,
	"gemini-1.5-flash": 1048576,
}

// LookupContextSize returns the context window size for the given model name.
// Returns 0 if the model is not recognized.
func LookupContextSize(modelName string) int {
	// Try exact match first
	if size, ok := modelContextSizes[modelName]; ok {
		return size
	}
	// Try prefix match — longest prefix wins to avoid "gpt-4" matching "gpt-4o-..."
	bestPrefix := ""
	bestSize := 0
	for prefix, size := range modelContextSizes {
		if len(modelName) >= len(prefix) && modelName[:len(prefix)] == prefix {
			if len(prefix) > len(bestPrefix) {
				bestPrefix = prefix
				bestSize = size
			}
		}
	}
	return bestSize
}
