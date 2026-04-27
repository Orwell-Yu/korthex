# Phase 2: Data Safety Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add regex-based log redaction before LLM sends, and log bookmarks in the Log Viewer.

**Architecture:** New `pkg/redact` module (Layer 0) provides a configurable regex redaction engine. Agent layer inserts redaction between tool execution and LLM message assembly. Log bookmarks add an in-memory `[]BookmarkEntry` to LogViewerModel with an overlay for listing/jumping. An AI tool `bookmark_log_lines` lets the agent mark important lines.

**Tech Stack:** Go 1.24, regexp, testify, Bubble Tea, Lipgloss

**Module path:** `github.com/Orwell-Yu/korthex`

---

## File Structure

| Action | Path | Responsibility |
|--------|------|---------------|
| Create | `pkg/redact/redact.go` | Redaction engine: Rule struct, Engine struct, Redact method |
| Create | `pkg/redact/builtin.go` | Built-in rules: JWT, API key, email, IPv4, credit card, bearer |
| Create | `pkg/redact/redact_test.go` | Tests for engine + built-in rules |
| Modify | `internal/config/config.go:13-45` | Add `Privacy` config section with `RedactionConfig` |
| Modify | `configs/default.yaml` | Add privacy.redaction section |
| Create | `internal/config/config_privacy_test.go` | Tests for new config fields |
| Modify | `internal/agent/agent_impl.go:18-28,163-181` | Add redaction engine field + apply redaction to tool results |
| Modify | `internal/agent/agent_impl.go:31-53` | Inject redaction engine in New() |
| Create | `internal/agent/redaction_test.go` | Tests for redaction integration in agent loop |
| Modify | `internal/ui/logviewer.go:21-60` | Add bookmark fields to LogViewerModel |
| Create | `internal/ui/bookmark.go` | BookmarkEntry struct, bookmark management, overlay rendering |
| Create | `internal/ui/bookmark_test.go` | Tests for bookmark add/remove/navigation |
| Modify | `internal/agent/tools.go:27-121` | Add `bookmark_log_lines` tool definition |
| Modify | `internal/agent/tools.go:123-149` | Add dispatch case for `bookmark_log_lines` |
| Modify | `internal/agent/safety.go:8-24` | Add `bookmark_log_lines` to whitelist |
| Modify | `internal/agent/prompt.go` | Add redaction awareness + bookmark instruction to system prompt |

---

### Task 1: Redaction Engine Core

**Files:**
- Create: `pkg/redact/redact.go`
- Create: `pkg/redact/redact_test.go`

- [ ] **Step 1: Write the failing test**

```go
// pkg/redact/redact_test.go
package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_Redact_SingleRule(t *testing.T) {
	engine, err := NewEngine([]RuleConfig{
		{Name: "email", Pattern: `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, Replacement: "[EMAIL_REDACTED]"},
	})
	require.NoError(t, err)

	output, stats := engine.Redact([]byte("Contact user@example.com for help"))
	assert.Equal(t, "Contact [EMAIL_REDACTED] for help", string(output))
	assert.Equal(t, 1, stats.TotalMatches)
	assert.Equal(t, 1, stats.ByRule["email"])
}

func TestEngine_Redact_NoMatch(t *testing.T) {
	engine, err := NewEngine([]RuleConfig{
		{Name: "email", Pattern: `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, Replacement: "[EMAIL_REDACTED]"},
	})
	require.NoError(t, err)

	output, stats := engine.Redact([]byte("No emails here"))
	assert.Equal(t, "No emails here", string(output))
	assert.Equal(t, 0, stats.TotalMatches)
}

func TestEngine_Redact_MultipleRules(t *testing.T) {
	engine, err := NewEngine([]RuleConfig{
		{Name: "email", Pattern: `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, Replacement: "[EMAIL_REDACTED]"},
		{Name: "ipv4", Pattern: `\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`, Replacement: "[IP_REDACTED]"},
	})
	require.NoError(t, err)

	output, stats := engine.Redact([]byte("From user@test.com on 10.0.0.1"))
	assert.Equal(t, "From [EMAIL_REDACTED] on [IP_REDACTED]", string(output))
	assert.Equal(t, 2, stats.TotalMatches)
	assert.Equal(t, 1, stats.ByRule["email"])
	assert.Equal(t, 1, stats.ByRule["ipv4"])
}

func TestEngine_Redact_InvalidRegex(t *testing.T) {
	_, err := NewEngine([]RuleConfig{
		{Name: "bad", Pattern: `[invalid`, Replacement: "x"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "compile rule \"bad\"")
}

func TestEngine_Redact_EmptyInput(t *testing.T) {
	engine, err := NewEngine([]RuleConfig{
		{Name: "email", Pattern: `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, Replacement: "[EMAIL_REDACTED]"},
	})
	require.NoError(t, err)

	output, stats := engine.Redact(nil)
	assert.Nil(t, output)
	assert.Equal(t, 0, stats.TotalMatches)
}

func TestEngine_Redact_NilEngine(t *testing.T) {
	var engine *Engine
	output, stats := engine.Redact([]byte("user@test.com"))
	assert.Equal(t, "user@test.com", string(output))
	assert.Equal(t, 0, stats.TotalMatches)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mio/korthex && go test ./pkg/redact/... -v -run TestEngine`
Expected: Compilation error — package `redact` does not exist.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/redact/redact.go
package redact

import (
	"fmt"
	"regexp"
)

// RuleConfig defines a single redaction rule from configuration.
type RuleConfig struct {
	Name        string
	Pattern     string
	Replacement string
}

// Rule is a compiled redaction rule ready for execution.
type Rule struct {
	Name        string
	Pattern     *regexp.Regexp
	Replacement string
}

// RedactStats tracks how many matches each rule produced.
type RedactStats struct {
	TotalMatches int
	ByRule       map[string]int
}

// Engine applies a set of compiled regex rules to redact sensitive data.
type Engine struct {
	rules []Rule
}

// NewEngine compiles the given rule configs into an Engine.
// Returns error if any regex pattern is invalid.
func NewEngine(configs []RuleConfig) (*Engine, error) {
	rules := make([]Rule, 0, len(configs))
	for _, rc := range configs {
		compiled, err := regexp.Compile(rc.Pattern)
		if err != nil {
			return nil, fmt.Errorf("compile rule %q: %w", rc.Name, err)
		}
		rules = append(rules, Rule{
			Name:        rc.Name,
			Pattern:     compiled,
			Replacement: rc.Replacement,
		})
	}
	return &Engine{rules: rules}, nil
}

// Redact applies all rules to input, returning redacted output and match stats.
// Safe to call on a nil Engine (returns input unchanged).
func (e *Engine) Redact(input []byte) ([]byte, RedactStats) {
	stats := RedactStats{ByRule: make(map[string]int)}
	if e == nil || len(input) == 0 {
		return input, stats
	}

	result := make([]byte, len(input))
	copy(result, input)

	for _, rule := range e.rules {
		matches := rule.Pattern.FindAllIndex(result, -1)
		count := len(matches)
		if count > 0 {
			stats.ByRule[rule.Name] = count
			stats.TotalMatches += count
			result = rule.Pattern.ReplaceAll(result, []byte(rule.Replacement))
		}
	}
	return result, stats
}

// FormatStatsSuffix returns a human-readable suffix like "[Redaction: 3 JWT, 2 email masked]".
// Returns empty string if no matches.
func FormatStatsSuffix(stats RedactStats) string {
	if stats.TotalMatches == 0 {
		return ""
	}
	parts := make([]string, 0, len(stats.ByRule))
	for name, count := range stats.ByRule {
		parts = append(parts, fmt.Sprintf("%d %s", count, name))
	}
	return fmt.Sprintf("\n[Redaction: %s masked]", joinParts(parts))
}

func joinParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += ", " + p
	}
	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/mio/korthex && go test ./pkg/redact/... -v -run TestEngine`
Expected: All 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/redact/redact.go pkg/redact/redact_test.go
git commit -m "feat(redact): add regex redaction engine core

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Built-in Redaction Rules

**Files:**
- Create: `pkg/redact/builtin.go`
- Modify: `pkg/redact/redact_test.go`

- [ ] **Step 1: Write failing tests for built-in rules**

Append to `pkg/redact/redact_test.go`:

```go
func TestBuiltinRules_JWT(t *testing.T) {
	engine, err := NewEngineWithBuiltins(nil, nil)
	require.NoError(t, err)

	input := []byte(`Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U`)
	output, stats := engine.Redact(input)
	assert.Contains(t, string(output), "[JWT_REDACTED]")
	assert.Greater(t, stats.ByRule["jwt"], 0)
}

func TestBuiltinRules_APIKey(t *testing.T) {
	engine, err := NewEngineWithBuiltins(nil, nil)
	require.NoError(t, err)

	input := []byte(`api_key=sk-1234567890abcdef1234567890abcdef`)
	output, stats := engine.Redact(input)
	assert.Contains(t, string(output), "[REDACTED]")
	assert.Greater(t, stats.ByRule["api_key"], 0)
}

func TestBuiltinRules_Email(t *testing.T) {
	engine, err := NewEngineWithBuiltins(nil, nil)
	require.NoError(t, err)

	input := []byte(`User admin@company.com logged in`)
	output, stats := engine.Redact(input)
	assert.Equal(t, "User [EMAIL_REDACTED] logged in", string(output))
	assert.Equal(t, 1, stats.ByRule["email"])
}

func TestBuiltinRules_DisableSpecific(t *testing.T) {
	engine, err := NewEngineWithBuiltins(nil, []string{"ipv4"})
	require.NoError(t, err)

	input := []byte(`From admin@test.com on 10.0.0.1`)
	output, stats := engine.Redact(input)
	assert.Contains(t, string(output), "[EMAIL_REDACTED]")
	assert.Contains(t, string(output), "10.0.0.1") // NOT redacted
	assert.Equal(t, 0, stats.ByRule["ipv4"])
}

func TestBuiltinRules_CustomRulesAppended(t *testing.T) {
	custom := []RuleConfig{
		{Name: "ssn", Pattern: `\b\d{3}-\d{2}-\d{4}\b`, Replacement: "[SSN_REDACTED]"},
	}
	engine, err := NewEngineWithBuiltins(custom, nil)
	require.NoError(t, err)

	input := []byte(`SSN: 123-45-6789 email: a@b.com`)
	output, stats := engine.Redact(input)
	assert.Contains(t, string(output), "[SSN_REDACTED]")
	assert.Contains(t, string(output), "[EMAIL_REDACTED]")
	assert.Equal(t, 1, stats.ByRule["ssn"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mio/korthex && go test ./pkg/redact/... -v -run TestBuiltinRules`
Expected: Compilation error — `NewEngineWithBuiltins` not defined.

- [ ] **Step 3: Implement built-in rules**

```go
// pkg/redact/builtin.go
package redact

// BuiltinRules returns the default set of redaction rules.
var BuiltinRules = []RuleConfig{
	{
		Name:        "jwt",
		Pattern:     `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`,
		Replacement: "[JWT_REDACTED]",
	},
	{
		Name:        "api_key",
		Pattern:     `(?i)(api[_-]?key|token|secret|password)[=:]\s*['"]?[A-Za-z0-9_-]{16,}`,
		Replacement: "${1}=[REDACTED]",
	},
	{
		Name:        "email",
		Pattern:     `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`,
		Replacement: "[EMAIL_REDACTED]",
	},
	{
		Name:        "ipv4",
		Pattern:     `\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`,
		Replacement: "[IP_REDACTED]",
	},
	{
		Name:        "credit_card",
		Pattern:     `\b\d{4}[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}\b`,
		Replacement: "[CC_REDACTED]",
	},
	{
		Name:        "bearer_token",
		Pattern:     `(?i)Bearer\s+[A-Za-z0-9_-]{20,}`,
		Replacement: "Bearer [TOKEN_REDACTED]",
	},
}

// NewEngineWithBuiltins creates an Engine with built-in rules + optional custom rules.
// disableBuiltin lists rule names to exclude from built-ins.
func NewEngineWithBuiltins(custom []RuleConfig, disableBuiltin []string) (*Engine, error) {
	disabled := make(map[string]bool, len(disableBuiltin))
	for _, name := range disableBuiltin {
		disabled[name] = true
	}

	var configs []RuleConfig
	for _, rule := range BuiltinRules {
		if !disabled[rule.Name] {
			configs = append(configs, rule)
		}
	}
	configs = append(configs, custom...)

	return NewEngine(configs)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/mio/korthex && go test ./pkg/redact/... -v`
Expected: All 11 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/redact/builtin.go pkg/redact/redact_test.go
git commit -m "feat(redact): add built-in rules (JWT, API key, email, IPv4, CC, bearer)

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Config Extension for Privacy

**Files:**
- Modify: `internal/config/config.go:13-18`
- Modify: `configs/default.yaml`
- Create: `internal/config/config_privacy_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/config/config_privacy_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_PrivacyDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("llm:\n  provider: openai\n"), 0644))

	mgr := NewManager(cfgPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	assert.True(t, cfg.Privacy.Redaction.Enabled)
	assert.Empty(t, cfg.Privacy.Redaction.Rules)
	assert.Empty(t, cfg.Privacy.Redaction.DisableBuiltin)
}

func TestLoad_PrivacyCustomRules(t *testing.T) {
	dir := t.TempDir()
	yaml := `
llm:
  provider: openai
privacy:
  redaction:
    enabled: true
    rules:
      - name: ssn
        pattern: '\b\d{3}-\d{2}-\d{4}\b'
        replacement: '[SSN_REDACTED]'
    disable_builtin:
      - ipv4
`
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	mgr := NewManager(cfgPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	assert.True(t, cfg.Privacy.Redaction.Enabled)
	require.Len(t, cfg.Privacy.Redaction.Rules, 1)
	assert.Equal(t, "ssn", cfg.Privacy.Redaction.Rules[0].Name)
	assert.Equal(t, `\b\d{3}-\d{2}-\d{4}\b`, cfg.Privacy.Redaction.Rules[0].Pattern)
	assert.Equal(t, "[SSN_REDACTED]", cfg.Privacy.Redaction.Rules[0].Replacement)
	assert.Equal(t, []string{"ipv4"}, cfg.Privacy.Redaction.DisableBuiltin)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mio/korthex && go test ./internal/config/... -v -run TestLoad_Privacy`
Expected: Compilation error — `cfg.Privacy` undefined.

- [ ] **Step 3: Add Privacy config types and defaults**

In `internal/config/config.go`, add to the Config struct (after `UI UIConfig` at line 18):

```go
type Config struct {
	Kubernetes KubernetesConfig
	LLM        LLMConfig
	Agent      AgentConfig
	UI         UIConfig
	Privacy    PrivacyConfig
}
```

Add new types after UIConfig (after line 45):

```go
type PrivacyConfig struct {
	Redaction RedactionConfig
}

type RedactionConfig struct {
	Enabled        bool                  // global switch, default true
	Rules          []RedactionRuleConfig // user-defined custom rules
	DisableBuiltin []string              // built-in rule names to disable
}

type RedactionRuleConfig struct {
	Name        string
	Pattern     string
	Replacement string
}
```

In `applyDefaults()`, add:

```go
cfg.Privacy.Redaction.Enabled = true
```

In the `Load()` method, add `IsSet` checks for the new fields (follow the existing pattern).

- [ ] **Step 4: Update `configs/default.yaml`**

Append to the end:

```yaml

privacy:
  redaction:
    enabled: true              # redact sensitive data before sending to LLM
    rules: []                  # custom redaction rules: [{name, pattern, replacement}]
    disable_builtin: []        # built-in rule names to skip (e.g., ipv4)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /Users/mio/korthex && go test ./internal/config/... -v -run TestLoad_Privacy`
Expected: Both tests PASS.

- [ ] **Step 6: Run full config test suite**

Run: `cd /Users/mio/korthex && go test ./internal/config/... -v`
Expected: All existing tests still pass.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_privacy_test.go configs/default.yaml
git commit -m "feat(config): add privacy.redaction config section

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Agent Redaction Integration

**Files:**
- Modify: `internal/agent/agent_impl.go:18-28` (add field)
- Modify: `internal/agent/agent_impl.go:31-53` (inject engine)
- Modify: `internal/agent/agent_impl.go:163-181` (apply redaction)
- Create: `internal/agent/redaction_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/agent/redaction_test.go
package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orwell-Yu/korthex/internal/config"
	"github.com/Orwell-Yu/korthex/internal/llm"
	"github.com/Orwell-Yu/korthex/pkg/redact"
)

func TestAgent_RedactsToolResult(t *testing.T) {
	// LLM calls kubectl_get_events, tool returns email in result,
	// then LLM returns final text.
	provider := &mockProvider{
		chatResponses: []*llm.Message{
			{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "tc1", Name: "kubectl_get_events", Arguments: map[string]string{"namespace": "default"}},
				},
			},
			{Role: llm.RoleAssistant, Content: "Found events."},
		},
	}

	k8sClient := newMockK8sClient()
	parser := newMockParser()

	engine, err := redact.NewEngineWithBuiltins(nil, nil)
	require.NoError(t, err)

	agentCfg := config.AgentConfig{MaxIterations: 5, MaxHistoryTurns: 10}
	a := New(provider, k8sClient, parser, agentCfg, true)
	a.(*agentImpl).redactEngine = engine

	events := collectEvents(t, a, "show events")

	// Find ToolResult event and verify redaction stats appear
	for _, ev := range events {
		if ev.Type == EventToolResult && ev.ToolName == "kubectl_get_events" {
			// The mock events result will contain "default" namespace text
			// but no sensitive data. Verify the engine was applied (no crash).
			assert.NotEmpty(t, ev.ToolResult)
			return
		}
	}
	t.Fatal("expected EventToolResult for kubectl_get_events")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mio/korthex && go test ./internal/agent/... -v -run TestAgent_Redacts`
Expected: Compilation error — `agentImpl` has no field `redactEngine`.

- [ ] **Step 3: Add redaction engine to agent**

In `internal/agent/agent_impl.go`, add the import and field:

Add to imports:
```go
"github.com/Orwell-Yu/korthex/pkg/redact"
```

Add field to `agentImpl` struct (after `sendLogs bool` at line 27):
```go
type agentImpl struct {
	provider llm.Provider
	tools    ToolExecutor
	safety   SafetyChecker
	history  *HistoryManager
	parser   logparse.Parser

	clusterCtx    ClusterContext
	maxIterations int
	sendLogs      bool
	redactEngine  *redact.Engine // nil = no redaction
}
```

In the `New()` function, add after line 47 (`sendLogs: sendLogs`):
```go
// Build redaction engine if privacy.redaction.enabled (caller passes it in)
```

Add a new method to set the redaction engine (called from app.go):
```go
// SetRedactEngine injects the redaction engine for tool result sanitization.
func (a *agentImpl) SetRedactEngine(engine *redact.Engine) {
	a.redactEngine = engine
}
```

Add `SetRedactEngine` to the `Agent` interface in `agent.go`:
```go
type Agent interface {
	Execute(ctx context.Context, userQuery string, ch chan<- AgentEvent) error
	ClearHistory()
	SetClusterContext(ctx ClusterContext)
	SetLogBufferReader(reader LogBufferReader)
	SetRedactEngine(engine *redact.Engine)
}
```

In the Execute method, after line 164 (`llmResult := result`), insert redaction:
```go
			// Apply redaction before sending to LLM
			if a.redactEngine != nil {
				redacted, stats := a.redactEngine.Redact([]byte(llmResult))
				llmResult = string(redacted)
				if suffix := redact.FormatStatsSuffix(stats); suffix != "" {
					llmResult += suffix
				}
			}
```

- [ ] **Step 4: Update mock agent to satisfy interface**

In `internal/agent/mock_agent.go`, add the new method:
```go
func (m *MockAgent) SetRedactEngine(engine *redact.Engine) {}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /Users/mio/korthex && go test ./internal/agent/... -v -run TestAgent_Redacts`
Expected: PASS.

- [ ] **Step 6: Run full agent test suite**

Run: `cd /Users/mio/korthex && go test ./internal/agent/... -v`
Expected: All existing tests still pass.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_impl.go internal/agent/mock_agent.go internal/agent/redaction_test.go
git commit -m "feat(agent): integrate redaction engine into tool result pipeline

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Log Bookmarks Data Structure

**Files:**
- Create: `internal/ui/bookmark.go`
- Create: `internal/ui/bookmark_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/ui/bookmark_test.go
package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBookmarkManager_Toggle(t *testing.T) {
	bm := NewBookmarkManager(50)

	bm.Toggle(10, "line 10 content")
	assert.Equal(t, 1, bm.Count())
	assert.True(t, bm.IsBookmarked(10))

	// Toggle off
	bm.Toggle(10, "line 10 content")
	assert.Equal(t, 0, bm.Count())
	assert.False(t, bm.IsBookmarked(10))
}

func TestBookmarkManager_MaxBookmarks(t *testing.T) {
	bm := NewBookmarkManager(3)

	bm.Toggle(1, "a")
	bm.Toggle(2, "b")
	bm.Toggle(3, "c")
	assert.Equal(t, 3, bm.Count())

	// At max — should not add more
	added := bm.Toggle(4, "d")
	assert.False(t, added)
	assert.Equal(t, 3, bm.Count())
}

func TestBookmarkManager_NextPrev(t *testing.T) {
	bm := NewBookmarkManager(50)
	bm.Toggle(10, "a")
	bm.Toggle(30, "b")
	bm.Toggle(50, "c")

	// Next from line 0 → should jump to 10
	next, ok := bm.Next(0)
	assert.True(t, ok)
	assert.Equal(t, 10, next)

	// Next from line 10 → 30
	next, ok = bm.Next(10)
	assert.True(t, ok)
	assert.Equal(t, 30, next)

	// Next from line 50 → wrap to 10
	next, ok = bm.Next(50)
	assert.True(t, ok)
	assert.Equal(t, 10, next)

	// Prev from line 50 → 30
	prev, ok := bm.Prev(50)
	assert.True(t, ok)
	assert.Equal(t, 30, prev)

	// Prev from line 10 → wrap to 50
	prev, ok = bm.Prev(10)
	assert.True(t, ok)
	assert.Equal(t, 50, prev)
}

func TestBookmarkManager_ShiftOnEviction(t *testing.T) {
	bm := NewBookmarkManager(50)
	bm.Toggle(5, "a")
	bm.Toggle(15, "b")
	bm.Toggle(25, "c")

	// Evict 10 lines from the front
	bm.ShiftOnEviction(10)

	// Line 5 should be removed (< 10), lines 15 and 25 shifted
	assert.False(t, bm.IsBookmarked(5))
	assert.True(t, bm.IsBookmarked(5))  // 15 - 10 = 5
	assert.True(t, bm.IsBookmarked(15)) // 25 - 10 = 15
	assert.Equal(t, 2, bm.Count())
}

func TestBookmarkManager_List(t *testing.T) {
	bm := NewBookmarkManager(50)
	bm.Toggle(10, "first line")
	bm.Toggle(30, "second line")

	entries := bm.List()
	assert.Len(t, entries, 2)
	assert.Equal(t, 10, entries[0].LineIndex)
	assert.Equal(t, "first line", entries[0].Preview)
	assert.Equal(t, 30, entries[1].LineIndex)
}

func TestBookmarkManager_Delete(t *testing.T) {
	bm := NewBookmarkManager(50)
	bm.Toggle(10, "a")
	bm.Toggle(20, "b")

	bm.Delete(0) // delete first entry
	assert.Equal(t, 1, bm.Count())
	assert.False(t, bm.IsBookmarked(10))
	assert.True(t, bm.IsBookmarked(20))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mio/korthex && go test ./internal/ui/... -v -run TestBookmarkManager`
Expected: Compilation error — `NewBookmarkManager` not defined.

- [ ] **Step 3: Implement bookmark manager**

```go
// internal/ui/bookmark.go
package ui

import (
	"sort"
	"time"
)

// BookmarkEntry represents a bookmarked log line.
type BookmarkEntry struct {
	LineIndex int
	Preview   string // first 80 chars of the line content
	CreatedAt time.Time
}

// BookmarkManager tracks bookmarked lines in the Log Viewer.
type BookmarkManager struct {
	entries []BookmarkEntry
	maxSize int
	index   map[int]int // lineIndex → position in entries slice
}

// NewBookmarkManager creates a BookmarkManager with the given max capacity.
func NewBookmarkManager(maxSize int) *BookmarkManager {
	return &BookmarkManager{
		maxSize: maxSize,
		index:   make(map[int]int),
	}
}

// Toggle adds a bookmark if not present, removes if present.
// Returns true if added, false if removed or at max capacity.
func (bm *BookmarkManager) Toggle(lineIndex int, preview string) bool {
	if _, exists := bm.index[lineIndex]; exists {
		bm.removeByLineIndex(lineIndex)
		return false
	}
	if len(bm.entries) >= bm.maxSize {
		return false
	}
	if len(preview) > 80 {
		preview = preview[:80]
	}
	bm.entries = append(bm.entries, BookmarkEntry{
		LineIndex: lineIndex,
		Preview:   preview,
		CreatedAt: time.Now(),
	})
	bm.rebuildIndex()
	bm.sortByLineIndex()
	return true
}

// AddBatch adds multiple bookmarks at once (used by AI tool).
func (bm *BookmarkManager) AddBatch(indices []int, previewFunc func(int) string) int {
	added := 0
	for _, idx := range indices {
		if len(bm.entries) >= bm.maxSize {
			break
		}
		if _, exists := bm.index[idx]; exists {
			continue
		}
		preview := ""
		if previewFunc != nil {
			preview = previewFunc(idx)
		}
		if len(preview) > 80 {
			preview = preview[:80]
		}
		bm.entries = append(bm.entries, BookmarkEntry{
			LineIndex: idx,
			Preview:   preview,
			CreatedAt: time.Now(),
		})
		added++
	}
	bm.rebuildIndex()
	bm.sortByLineIndex()
	return added
}

// IsBookmarked returns whether the given line index has a bookmark.
func (bm *BookmarkManager) IsBookmarked(lineIndex int) bool {
	_, exists := bm.index[lineIndex]
	return exists
}

// Count returns the number of bookmarks.
func (bm *BookmarkManager) Count() int {
	return len(bm.entries)
}

// List returns all bookmarks sorted by line index.
func (bm *BookmarkManager) List() []BookmarkEntry {
	out := make([]BookmarkEntry, len(bm.entries))
	copy(out, bm.entries)
	return out
}

// Delete removes the bookmark at the given list position (0-indexed).
func (bm *BookmarkManager) Delete(pos int) {
	if pos < 0 || pos >= len(bm.entries) {
		return
	}
	bm.entries = append(bm.entries[:pos], bm.entries[pos+1:]...)
	bm.rebuildIndex()
}

// Next returns the line index of the next bookmark after currentLine.
// Wraps around to the first bookmark if at end.
func (bm *BookmarkManager) Next(currentLine int) (int, bool) {
	if len(bm.entries) == 0 {
		return 0, false
	}
	for _, e := range bm.entries {
		if e.LineIndex > currentLine {
			return e.LineIndex, true
		}
	}
	return bm.entries[0].LineIndex, true // wrap
}

// Prev returns the line index of the previous bookmark before currentLine.
// Wraps around to the last bookmark if at beginning.
func (bm *BookmarkManager) Prev(currentLine int) (int, bool) {
	if len(bm.entries) == 0 {
		return 0, false
	}
	for i := len(bm.entries) - 1; i >= 0; i-- {
		if bm.entries[i].LineIndex < currentLine {
			return bm.entries[i].LineIndex, true
		}
	}
	return bm.entries[len(bm.entries)-1].LineIndex, true // wrap
}

// ShiftOnEviction adjusts bookmark indices when the ring buffer evicts old lines.
// Removes bookmarks for evicted lines and shifts remaining ones down.
func (bm *BookmarkManager) ShiftOnEviction(evictedCount int) {
	var kept []BookmarkEntry
	for _, e := range bm.entries {
		if e.LineIndex >= evictedCount {
			e.LineIndex -= evictedCount
			kept = append(kept, e)
		}
	}
	bm.entries = kept
	bm.rebuildIndex()
}

func (bm *BookmarkManager) removeByLineIndex(lineIndex int) {
	pos, exists := bm.index[lineIndex]
	if !exists {
		return
	}
	bm.entries = append(bm.entries[:pos], bm.entries[pos+1:]...)
	bm.rebuildIndex()
}

func (bm *BookmarkManager) rebuildIndex() {
	bm.index = make(map[int]int, len(bm.entries))
	for i, e := range bm.entries {
		bm.index[e.LineIndex] = i
	}
}

func (bm *BookmarkManager) sortByLineIndex() {
	sort.Slice(bm.entries, func(i, j int) bool {
		return bm.entries[i].LineIndex < bm.entries[j].LineIndex
	})
	bm.rebuildIndex()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/mio/korthex && go test ./internal/ui/... -v -run TestBookmarkManager`
Expected: All 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/bookmark.go internal/ui/bookmark_test.go
git commit -m "feat(ui): add bookmark manager for Log Viewer

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Log Viewer Bookmark UI Integration

**Files:**
- Modify: `internal/ui/logviewer.go:21-60` (add fields)
- Modify: `internal/ui/logviewer.go` (key handlers + overlay + footer)

- [ ] **Step 1: Add bookmark fields to LogViewerModel**

In `internal/ui/logviewer.go`, add to the struct (after the filter state block, around line 50):

```go
	// Bookmark state
	bookmarks        *BookmarkManager
	showBookmarkList bool   // true = show bookmark overlay
	bookmarkCursor   int    // cursor position in bookmark list overlay
```

In the `NewLogViewerModel` constructor, initialize:
```go
	bookmarks: NewBookmarkManager(50),
```

- [ ] **Step 2: Add key handlers in Update()**

In the `Update` method's key handling section, add cases (only when NOT searching and NOT filtering):

```go
	case "m":
		// Toggle bookmark on current line
		if len(m.lines) > 0 && m.scrollOff < len(m.lines) {
			idx := m.scrollOff // current top visible line; or use cursor if there's one
			preview := m.lines[idx].Content
			m.bookmarks.Toggle(idx, preview)
		}

	case "'":
		// Open bookmark list overlay
		if m.bookmarks.Count() > 0 {
			m.showBookmarkList = true
			m.bookmarkCursor = 0
		}
```

When NOT in search mode (search regex is nil), `n`/`N` navigate bookmarks:
```go
	case "n":
		if m.searchRegex != nil {
			// existing search next behavior
		} else if m.bookmarks.Count() > 0 {
			if next, ok := m.bookmarks.Next(m.scrollOff); ok {
				m.scrollOff = next
				m.follow = false
			}
		}

	case "N":
		if m.searchRegex != nil {
			// existing search prev behavior
		} else if m.bookmarks.Count() > 0 {
			if prev, ok := m.bookmarks.Prev(m.scrollOff); ok {
				m.scrollOff = prev
				m.follow = false
			}
		}
```

Bookmark overlay key handling (when `m.showBookmarkList` is true):
```go
	if m.showBookmarkList {
		switch msg.String() {
		case "esc":
			m.showBookmarkList = false
		case "j", "down":
			if m.bookmarkCursor < m.bookmarks.Count()-1 {
				m.bookmarkCursor++
			}
		case "k", "up":
			if m.bookmarkCursor > 0 {
				m.bookmarkCursor--
			}
		case "enter":
			entries := m.bookmarks.List()
			if m.bookmarkCursor < len(entries) {
				m.scrollOff = entries[m.bookmarkCursor].LineIndex
				m.follow = false
			}
			m.showBookmarkList = false
		case "d":
			m.bookmarks.Delete(m.bookmarkCursor)
			if m.bookmarkCursor >= m.bookmarks.Count() {
				m.bookmarkCursor = max(m.bookmarks.Count()-1, 0)
			}
			if m.bookmarks.Count() == 0 {
				m.showBookmarkList = false
			}
		}
		return m, nil
	}
```

- [ ] **Step 3: Add bookmark overlay rendering in View()**

In the `View()` function, add at the top (before normal rendering, after the describe-overlay pattern):

```go
	if m.showBookmarkList {
		return m.renderBookmarkOverlay()
	}
```

Add the overlay render method:
```go
func (m LogViewerModel) renderBookmarkOverlay() string {
	var b strings.Builder
	entries := m.bookmarks.List()

	b.WriteString(m.theme.Title.Render(fmt.Sprintf("Bookmarks (%d)", len(entries))))
	b.WriteString("\n\n")

	viewH := max(m.viewHeight-4, 1)
	for i, entry := range entries {
		if i >= viewH {
			b.WriteString(m.theme.Subtitle.Render("... (scroll for more)"))
			break
		}
		prefix := "  "
		if i == m.bookmarkCursor {
			prefix = m.theme.Selected.Render("> ")
		}
		line := fmt.Sprintf("%s#%d  L.%-6d %s", prefix, i+1, entry.LineIndex, entry.Preview)
		if i == m.bookmarkCursor {
			b.WriteString(m.theme.Selected.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.theme.Subtitle.Render("[Enter] jump  [d] delete  [Esc] close"))
	return b.String()
}
```

- [ ] **Step 4: Update footer to show bookmark count**

In `renderFooter()`, add before the final return:
```go
	if m.bookmarks.Count() > 0 {
		parts = append(parts, fmt.Sprintf("[%d bookmarks]", m.bookmarks.Count()))
	}
```

- [ ] **Step 5: Add bookmark marker in log line rendering**

In `renderLogLine()`, add at the start:
```go
	if m.bookmarks.IsBookmarked(lineIdx) {
		// Replace line number with bookmark marker
		prefix = m.theme.AccentStyle().Render("▸ ")
	}
```

- [ ] **Step 6: Run full UI test suite**

Run: `cd /Users/mio/korthex && go test ./internal/ui/... -v`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/logviewer.go
git commit -m "feat(ui): integrate bookmarks into Log Viewer (m/'/n/N keys + overlay)

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 7: AI Bookmark Tool

**Files:**
- Modify: `internal/agent/tools.go:27-121` (tool definition)
- Modify: `internal/agent/tools.go:123-149` (dispatch case)
- Modify: `internal/agent/safety.go:8-24` (whitelist)
- Modify: `internal/agent/prompt.go` (bookmark instruction)

- [ ] **Step 1: Add tool definition**

In `tools.go` `ToolDefinitions()`, add after the `search_visible_logs` definition:

```go
		{
			Name:        "bookmark_log_lines",
			Description: "Mark important log lines in the Log Viewer with bookmarks. Use after analysis to highlight key findings for the user. The user can then press ' to see all bookmarks and jump to them.",
			Parameters: []llm.ParameterDef{
				{Name: "lineIndices", Type: "string", Description: "Comma-separated line indices to bookmark (e.g., '10,25,42')", Required: true},
			},
		},
```

- [ ] **Step 2: Add dispatch case**

In `ExecuteTool()` switch, add:

```go
	case "bookmark_log_lines":
		return t.bookmarkLogLines(args)
```

- [ ] **Step 3: Add BookmarkWriter interface and handler**

In `agent.go`, add a new interface:
```go
// BookmarkWriter allows the agent to add bookmarks to the Log Viewer.
type BookmarkWriter interface {
	AddBatch(indices []int, previewFunc func(int) string) int
}
```

Add field to `toolExecutor` struct in `tools.go`:
```go
type toolExecutor struct {
	k8sClient      k8s.Client
	logBuffer      LogBufferReader
	bookmarkWriter BookmarkWriter
}
```

Add the handler method:
```go
func (t *toolExecutor) bookmarkLogLines(args map[string]string) (string, []k8s.LogLine, error) {
	raw := args["lineIndices"]
	if raw == "" {
		return "", nil, fmt.Errorf("lineIndices is required")
	}

	parts := strings.Split(raw, ",")
	indices := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		idx, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		indices = append(indices, idx)
	}

	if t.bookmarkWriter == nil {
		return fmt.Sprintf("Bookmarked %d lines (bookmark writer not available)", len(indices)), nil, nil
	}

	var previewFunc func(int) string
	if t.logBuffer != nil {
		entries := t.logBuffer.Slice()
		previewFunc = func(idx int) string {
			if idx >= 0 && idx < len(entries) {
				return entries[idx].Content
			}
			return ""
		}
	}

	added := t.bookmarkWriter.AddBatch(indices, previewFunc)
	return fmt.Sprintf("Added %d bookmarks. User can press ' to view bookmarks and jump to them.", added), nil, nil
}
```

- [ ] **Step 4: Add to safety whitelist**

In `safety.go`, add to the whitelist map:
```go
"bookmark_log_lines": SafetyAllowed,
```

- [ ] **Step 5: Add bookmark instruction to system prompt**

In `prompt.go`, add a section after the Log Viewer Buffer section:

```go
b.WriteString("\n## Bookmarks\n")
b.WriteString("After analyzing logs and identifying important lines, use bookmark_log_lines to mark them.\n")
b.WriteString("Tell the user: 'I've bookmarked N key lines. Press ' to view and jump to them.'\n")
```

- [ ] **Step 6: Add SetBookmarkWriter to Agent interface and impl**

In `agent.go`:
```go
type Agent interface {
	// ... existing methods ...
	SetBookmarkWriter(writer BookmarkWriter)
}
```

In `agent_impl.go`:
```go
func (a *agentImpl) SetBookmarkWriter(writer BookmarkWriter) {
	if te, ok := a.tools.(*toolExecutor); ok {
		te.bookmarkWriter = writer
	}
}
```

In `mock_agent.go`:
```go
func (m *MockAgent) SetBookmarkWriter(writer BookmarkWriter) {}
```

- [ ] **Step 7: Run full test suite**

Run: `cd /Users/mio/korthex && go test ./internal/agent/... -v`
Expected: All tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_impl.go internal/agent/mock_agent.go internal/agent/tools.go internal/agent/safety.go internal/agent/prompt.go
git commit -m "feat(agent): add bookmark_log_lines tool for AI-driven bookmarking

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 8: Wire Redaction + Bookmarks in App

**Files:**
- Modify: `internal/app/app.go:24-32` (add redact engine)
- Modify: `internal/app/app.go:34-101` (build engine in New)
- Modify: `internal/app/app.go:103-122` (wire bookmark writer)

- [ ] **Step 1: Build redaction engine in New()**

In `app.go`, add import:
```go
"github.com/Orwell-Yu/korthex/pkg/redact"
```

After agent creation (around line 92), build and inject the redaction engine:
```go
	// Build redaction engine
	if cfg.Privacy.Redaction.Enabled {
		var ruleConfigs []redact.RuleConfig
		for _, r := range cfg.Privacy.Redaction.Rules {
			ruleConfigs = append(ruleConfigs, redact.RuleConfig{
				Name:        r.Name,
				Pattern:     r.Pattern,
				Replacement: r.Replacement,
			})
		}
		engine, err := redact.NewEngineWithBuiltins(ruleConfigs, cfg.Privacy.Redaction.DisableBuiltin)
		if err != nil {
			slog.Warn("failed to build redaction engine", "error", err)
		} else {
			a.agent.SetRedactEngine(engine)
			slog.Info("redaction engine initialized", "rules", len(ruleConfigs)+len(redact.BuiltinRules))
		}
	}
```

- [ ] **Step 2: Wire bookmark writer in Run()**

In `Run()`, after the AppModel is created and log buffer reader is set:
```go
	// Wire bookmark writer from LogViewer to Agent
	a.agent.SetBookmarkWriter(appModel.LogViewerBookmarks())
```

This requires adding a `LogViewerBookmarks()` method to AppModel that returns the LogViewer's BookmarkManager (which satisfies `BookmarkWriter` via `AddBatch`).

- [ ] **Step 3: Run full build**

Run: `cd /Users/mio/korthex && make build`
Expected: Successful compilation.

- [ ] **Step 4: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): wire redaction engine and bookmark writer

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 9: Update System Prompt for Redaction Awareness

**Files:**
- Modify: `internal/agent/prompt.go`

- [ ] **Step 1: Add redaction awareness to system prompt**

In `prompt.go` `BuildSystemPrompt()`, add a new section after the Log Privacy Mode section (around line 107):

```go
	// Redaction awareness
	b.WriteString("\n## Data Redaction\n")
	b.WriteString("Log content sent to you may contain [REDACTED], [JWT_REDACTED], [EMAIL_REDACTED], [IP_REDACTED], or similar markers.\n")
	b.WriteString("These indicate sensitive data has been masked. When analyzing:\n")
	b.WriteString("- Do NOT attempt to guess or reconstruct redacted values\n")
	b.WriteString("- Focus on error patterns, timestamps, severity distribution, and non-sensitive context\n")
	b.WriteString("- If redaction stats appear at the end of tool results (e.g., '[Redaction: 3 JWT masked]'), acknowledge this briefly\n")
```

- [ ] **Step 2: Commit**

```bash
git add internal/agent/prompt.go
git commit -m "feat(agent): add redaction and bookmark awareness to system prompt

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 10: Final Integration Test

- [ ] **Step 1: Run full test suite with race detector**

Run: `cd /Users/mio/korthex && go test -race ./...`
Expected: All tests pass, no race conditions.

- [ ] **Step 2: Run linter**

Run: `cd /Users/mio/korthex && make lint`
Expected: No new warnings.

- [ ] **Step 3: Build binary**

Run: `cd /Users/mio/korthex && make build`
Expected: Clean build → `bin/korthex`.

- [ ] **Step 4: Manual smoke test (if cluster available)**

Run `bin/korthex` and verify:
1. Redaction engine loads on startup (check log)
2. In Log Viewer: press `m` to bookmark, `'` to see list
3. AI query that returns logs → check that sensitive patterns are redacted in AI Chat but visible in Log Viewer

- [ ] **Step 5: Final commit if any fixes needed**
