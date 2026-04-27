package redact

import (
	"strings"
	"sync"
	"testing"
)

func TestBuiltinRules_JWT(t *testing.T) {
	e := NewEngine(RedactionConfig{Enabled: true})
	input := []byte(`token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U`)
	output, stats := e.Redact(input)
	if !strings.Contains(string(output), "[JWT_REDACTED]") {
		t.Errorf("expected JWT to be redacted, got: %s", output)
	}
	if stats.ByRule["jwt"] == 0 {
		t.Error("expected jwt rule to fire")
	}
}

func TestBuiltinRules_BearerToken(t *testing.T) {
	e := NewEngine(RedactionConfig{Enabled: true})
	input := []byte(`Authorization: Bearer sk-proj-abc123def456ghi789jkl0123456789`)
	output, stats := e.Redact(input)
	if !strings.Contains(string(output), "[TOKEN_REDACTED]") {
		t.Errorf("expected bearer token to be redacted, got: %s", output)
	}
	if stats.ByRule["bearer_token"] == 0 {
		t.Error("expected bearer_token rule to fire")
	}
}

func TestBuiltinRules_APIKey(t *testing.T) {
	e := NewEngine(RedactionConfig{Enabled: true})
	input := []byte(`api_key=sk_live_abcdef123456789012345`)
	output, stats := e.Redact(input)
	if !strings.Contains(string(output), "[REDACTED]") {
		t.Errorf("expected API key to be redacted, got: %s", output)
	}
	if stats.ByRule["api_key"] == 0 {
		t.Error("expected api_key rule to fire")
	}
}

func TestBuiltinRules_Email(t *testing.T) {
	e := NewEngine(RedactionConfig{Enabled: true})
	input := []byte(`user email: alice@example.com sent to bob@company.org`)
	output, stats := e.Redact(input)
	if strings.Contains(string(output), "alice@") {
		t.Errorf("expected email to be redacted, got: %s", output)
	}
	if stats.ByRule["email"] != 2 {
		t.Errorf("expected 2 email matches, got: %d", stats.ByRule["email"])
	}
}

func TestBuiltinRules_IPv4(t *testing.T) {
	e := NewEngine(RedactionConfig{Enabled: true})
	input := []byte(`connecting to 192.168.1.100:8080 from 10.0.0.1`)
	output, stats := e.Redact(input)
	if strings.Contains(string(output), "192.168") {
		t.Errorf("expected IP to be redacted, got: %s", output)
	}
	if stats.ByRule["ipv4"] != 2 {
		t.Errorf("expected 2 IPv4 matches, got: %d", stats.ByRule["ipv4"])
	}
}

func TestBuiltinRules_CreditCard(t *testing.T) {
	e := NewEngine(RedactionConfig{Enabled: true})
	input := []byte(`payment with card 4111-1111-1111-1111`)
	output, stats := e.Redact(input)
	if !strings.Contains(string(output), "[CC_REDACTED]") {
		t.Errorf("expected credit card to be redacted, got: %s", output)
	}
	if stats.ByRule["credit_card"] == 0 {
		t.Error("expected credit_card rule to fire")
	}
}

func TestDisableBuiltin(t *testing.T) {
	e := NewEngine(RedactionConfig{
		Enabled:        true,
		DisableBuiltin: []string{"ipv4"},
	})
	input := []byte(`connecting to 192.168.1.100`)
	output, _ := e.Redact(input)
	if strings.Contains(string(output), "[IP_REDACTED]") {
		t.Error("expected ipv4 rule to be disabled")
	}
}

func TestCustomRule(t *testing.T) {
	e := NewEngine(RedactionConfig{
		Enabled: true,
		Rules: []RuleConfig{
			{
				Name:        "ssn",
				Pattern:     `\b\d{3}-\d{2}-\d{4}\b`,
				Replacement: "[SSN_REDACTED]",
			},
		},
	})
	input := []byte(`SSN: 123-45-6789`)
	output, stats := e.Redact(input)
	if !strings.Contains(string(output), "[SSN_REDACTED]") {
		t.Errorf("expected SSN to be redacted, got: %s", output)
	}
	if stats.ByRule["ssn"] != 1 {
		t.Errorf("expected 1 ssn match, got: %d", stats.ByRule["ssn"])
	}
}

func TestCustomRule_InvalidPattern(t *testing.T) {
	// Should not panic; invalid pattern is logged and skipped.
	e := NewEngine(RedactionConfig{
		Enabled: true,
		Rules: []RuleConfig{
			{Name: "bad", Pattern: `[invalid`, Replacement: "X"},
		},
	})
	input := []byte(`some text`)
	output, stats := e.Redact(input)
	if string(output) != "some text" {
		t.Errorf("expected unchanged output, got: %s", output)
	}
	if stats.TotalMatches != 0 {
		t.Errorf("expected 0 matches, got: %d", stats.TotalMatches)
	}
}

func TestDisabled(t *testing.T) {
	e := NewEngine(RedactionConfig{Enabled: false})
	input := []byte(`api_key=sk_live_abcdef123456789012345`)
	output, stats := e.Redact(input)
	if string(output) != string(input) {
		t.Error("expected disabled engine to return input unchanged")
	}
	if stats.TotalMatches != 0 {
		t.Errorf("expected 0 matches when disabled, got: %d", stats.TotalMatches)
	}
}

func TestNilEngine(t *testing.T) {
	var e *Engine
	input := []byte(`api_key=sk_live_abcdef123456789012345`)
	output, stats := e.Redact(input)
	if string(output) != string(input) {
		t.Error("expected nil engine to return input unchanged")
	}
	if stats.TotalMatches != 0 {
		t.Errorf("expected 0 matches for nil engine, got: %d", stats.TotalMatches)
	}
}

func TestEmptyInput(t *testing.T) {
	e := NewEngine(RedactionConfig{Enabled: true})
	output, stats := e.Redact([]byte{})
	if len(output) != 0 {
		t.Errorf("expected empty output, got: %s", output)
	}
	if stats.TotalMatches != 0 {
		t.Errorf("expected 0 matches for empty input, got: %d", stats.TotalMatches)
	}
}

func TestFormatStats(t *testing.T) {
	stats := RedactStats{TotalMatches: 0, ByRule: map[string]int{}}
	if FormatStats(stats) != "" {
		t.Error("expected empty string for zero matches")
	}

	stats = RedactStats{TotalMatches: 3, ByRule: map[string]int{"jwt": 2, "email": 1}}
	result := FormatStats(stats)
	if !strings.HasPrefix(result, "[Redaction:") {
		t.Errorf("expected redaction prefix, got: %s", result)
	}
	if !strings.Contains(result, "jwt:2") {
		t.Errorf("expected jwt:2, got: %s", result)
	}
}

func TestConcurrency(t *testing.T) {
	e := NewEngine(RedactionConfig{Enabled: true})
	input := []byte(`email: test@example.com, ip: 10.0.0.1, token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U`)

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				output, stats := e.Redact(input)
				if stats.TotalMatches == 0 {
					t.Error("expected matches in concurrent run")
				}
				if strings.Contains(string(output), "test@example.com") {
					t.Error("expected email to be redacted in concurrent run")
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkRedact_1MB(b *testing.B) {
	e := NewEngine(RedactionConfig{Enabled: true})

	// Generate ~1MB of log text with embedded sensitive data
	line := `2024-01-01T14:23:05Z INFO  order-service-pod-abc12 Processing request from 192.168.1.100 user=alice@example.com token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U` + "\n"
	var builder strings.Builder
	for builder.Len() < 1<<20 { // 1MB
		builder.WriteString(line)
	}
	input := []byte(builder.String())

	b.ResetTimer()
	b.SetBytes(int64(len(input)))
	for range b.N {
		e.Redact(input)
	}
}
