package redact

import (
	"log/slog"
	"regexp"
)

// Rule is a compiled redaction rule.
type Rule struct {
	Name        string
	Pattern     *regexp.Regexp
	Replacement string
}

// RedactStats tracks redaction metrics for a single Redact call.
type RedactStats struct {
	TotalMatches int
	ByRule       map[string]int // rule name → match count
}

// RedactionConfig configures the engine at construction time.
type RedactionConfig struct {
	Enabled        bool
	Rules          []RuleConfig
	DisableBuiltin []string
}

// RuleConfig is an uncompiled rule from user configuration.
type RuleConfig struct {
	Name        string
	Pattern     string
	Replacement string
}

// Engine applies redaction rules to byte slices.
// It is immutable after construction and safe for concurrent use.
type Engine struct {
	rules   []Rule
	enabled bool
}

// NewEngine compiles all rules (builtin + custom) and returns an Engine.
// Invalid custom rule patterns are logged and skipped.
// A nil-safe no-op Engine is returned when cfg.Enabled is false.
func NewEngine(cfg RedactionConfig) *Engine {
	if !cfg.Enabled {
		return &Engine{enabled: false}
	}

	disableSet := make(map[string]bool, len(cfg.DisableBuiltin))
	for _, name := range cfg.DisableBuiltin {
		disableSet[name] = true
	}

	var rules []Rule

	// Add builtin rules (skip disabled ones)
	for _, r := range builtinRules() {
		if disableSet[r.Name] {
			slog.Debug("redact: builtin rule disabled", "rule", r.Name)
			continue
		}
		rules = append(rules, r)
	}

	// Add custom rules from config
	for _, rc := range cfg.Rules {
		if rc.Pattern == "" {
			slog.Warn("redact: skipping custom rule with empty pattern", "rule", rc.Name)
			continue
		}
		compiled, err := regexp.Compile(rc.Pattern)
		if err != nil {
			slog.Warn("redact: skipping custom rule with invalid pattern", "rule", rc.Name, "error", err)
			continue
		}
		rules = append(rules, Rule{
			Name:        rc.Name,
			Pattern:     compiled,
			Replacement: rc.Replacement,
		})
	}

	if len(rules) > 20 {
		slog.Warn("redact: too many rules may impact performance", "count", len(rules))
	}

	return &Engine{rules: rules, enabled: true}
}

// Redact applies all rules to input and returns the redacted output with stats.
// Returns input unchanged if the engine is disabled or has no rules.
func (e *Engine) Redact(input []byte) ([]byte, RedactStats) {
	stats := RedactStats{ByRule: make(map[string]int)}

	if e == nil || !e.enabled || len(e.rules) == 0 {
		return input, stats
	}

	output := make([]byte, len(input))
	copy(output, input)

	for _, r := range e.rules {
		count := 0
		output = r.Pattern.ReplaceAllFunc(output, func(match []byte) []byte {
			count++
			// Expand replacement with group references ($1, ${1}, etc.)
			return r.Pattern.ReplaceAll(match, []byte(r.Replacement))
		})
		if count > 0 {
			stats.ByRule[r.Name] = count
			stats.TotalMatches += count
		}
	}

	return output, stats
}

// FormatStats returns a human-readable summary of redaction stats.
// Returns empty string if nothing was redacted.
func FormatStats(stats RedactStats) string {
	if stats.TotalMatches == 0 {
		return ""
	}
	parts := make([]string, 0, len(stats.ByRule))
	for name, count := range stats.ByRule {
		parts = append(parts, name+":"+itoa(count))
	}
	return "[Redaction: " + join(parts, ", ") + " masked]"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 4)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	// reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}
