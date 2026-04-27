# pkg/redact - Log Redaction Engine

> **Parent docs:** [CLAUDE.md (root)](../../CLAUDE.md) | [SPEC.md Section 9.3](../../SPEC.md) | [Phase 2 PRD Section 5.1](../../docs/superpowers/specs/2026-04-24-phase2-prd-design.md)
>
> **Module rules:** [CLAUDE.md (this module)](./CLAUDE.md)

## Responsibility

Regex-based redaction of sensitive data (JWT tokens, API keys, emails, IPs, credit cards, bearer tokens) from log content before LLM transmission. Engine is stateless after construction, concurrent-safe. Log Viewer and log export display original unredacted content.

## Public Interfaces

```go
// Engine is immutable after construction and safe for concurrent use
type Engine struct { /* unexported */ }

func NewEngine(cfg RedactionConfig) *Engine
func (e *Engine) Redact(input []byte) (output []byte, stats RedactStats)
```

## Key Types

```go
type Rule struct {
    Name        string
    Pattern     *regexp.Regexp
    Replacement string
}

type RedactStats struct {
    TotalMatches int
    ByRule       map[string]int  // rule name → match count
}

type RedactionConfig struct {
    Enabled        bool
    Rules          []RuleConfig    // user-defined custom rules
    DisableBuiltin []string        // builtin rule names to disable
}

type RuleConfig struct {
    Name        string
    Pattern     string
    Replacement string
}
```

## Files

| File | Responsibility |
|------|---------------|
| `redact.go` | Engine struct, Rule type, RedactStats, RedactionConfig, `NewEngine()` (compiles all regexes), `Redact()` (chain `ReplaceAll`) |
| `builtin.go` | 6 built-in rule definitions: jwt, api_key, email, ipv4, credit_card, bearer_token. Regex patterns from PRD §5.1 |
| `redact_test.go` | Table-driven tests for each rule + custom rules + disable_builtin + benchmark |

## Dependencies

- **External**: None (pure stdlib + regexp)
- **Internal**: None (Layer 0)

## Design Notes

- Self-built because Go has no suitable general-purpose log redaction library. cockroachdb/redact is telemetry-specific (safe/unsafe marking), masq is slog-specific.
- Inspired by cockroachdb/redact's safe/unsafe concept for `RedactStats` — track match counts per rule for audit visibility.
- `NewEngine()` compiles all regexes once. Invalid regex in custom rules → skip with warning log, don't panic.
- `Redact()` applies rules in order: built-in rules first, then custom rules. Uses `regexp.ReplaceAll` chain on `[]byte`.
- Benchmark target: 1MB log text redacted in < 10ms. Recommended max 20 rules (startup warning if exceeded).

## Testing Strategy

- **Table-driven tests** for each built-in rule:
  - Positive: known sensitive patterns (real-world JWT, API keys, emails)
  - Negative: similar but non-sensitive strings (version numbers for ipv4, UUIDs for credit_card)
- **Custom rule tests**: user-defined patterns from config integration
- **disable_builtin tests**: verify specific rules excluded
- **Stats tests**: verify `ByRule` map counts match actual replacements
- **Benchmark tests**: 1MB mixed log content with embedded sensitive data
- **Concurrency tests**: parallel `Redact()` calls with race detector

## Phase 3+ Extension Points

- Structured redaction audit logging (which lines were redacted, for compliance)
- Per-tool redaction rule overrides (different rules for different log sources)
- Safe/unsafe annotation mode (mark regions instead of replacing)
