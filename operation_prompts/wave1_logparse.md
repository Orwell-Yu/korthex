# Wave 1A: pkg/logparse — Log Parsing Utilities

> **Prerequisite:** Wave 0 (scaffold) merged to main
> **Branch:** feat/logparse
> **Parallel with:** wave1_config.md (Instance B)
> **Files owned:** `pkg/logparse/*.go` (nothing else)
> **Estimated time:** 3-4 hours

---

You are developing the `pkg/logparse` module for the Korthex project — a stateless, zero-dependency log parsing library.

## Step 1: Read Documentation

Read these files **in order** before writing any code:
1. `pkg/logparse/CLAUDE.md` — module rules (Layer 0, zero deps, stateless, compiled regex)
2. `pkg/logparse/README.md` — interfaces, file responsibilities, supported formats, testing checklist
3. `SPEC.md` Section 3.2 — LogParse interface contract (the types are already defined in the skeleton)

## Step 2: Implement

The skeleton file `pkg/logparse/parser.go` already has type definitions (LogEntry, Severity, Parser interface). You need to implement the actual logic.

### File 1: `pkg/logparse/severity.go`

Implement `DetectSeverity(line string) Severity`:
- Use compiled regex patterns (cache with `sync.Once` or package-level `var`)
- Priority order (first match wins): FATAL/PANIC/CRITICAL > ERROR/ERR > WARN/WARNING > INFO > DEBUG/TRACE
- Case-insensitive matching
- Must handle various formats: `[ERROR]`, `level=error`, `"level":"error"`, `ERROR:`, ` ERROR `
- Return `SeverityUnknown` if no keyword found

### File 2: `pkg/logparse/timestamp.go`

Implement `ParseTimestamp(line string) time.Time`:
- Try formats in this order (first success wins):
  1. RFC 3339 / ISO 8601: `2024-01-01T14:23:05.123456789Z`
  2. RFC 3339 with offset: `2024-01-01T14:23:05+08:00`
  3. Java style: `2024-01-01 14:23:05.123`
  4. Go default: `2024/01/01 14:23:05`
  5. Kubelet --timestamps: extract leading timestamp from `2024-01-01T14:23:05.123Z rest of line`
- Return `time.Time{}` (zero value) if unparsable — never panic, never log.Fatal

### File 3: `pkg/logparse/json.go`

Implement:
- `IsJSONLine(line string) bool` — use `json.Valid([]byte(line))`
- `FormatJSON(line string) string` — use `json.Indent`, return original on error
- Do NOT unmarshal to struct — only detect and format

### File 4: `pkg/logparse/parser.go` (implementation)

Complete the `Parser` implementation:
- `Parse(podName, container, rawLine string) LogEntry`
- Call `ParseTimestamp`, `DetectSeverity`, `IsJSONLine`/`FormatJSON`
- Set `LogEntry.Raw = rawLine` (never modify)
- Set `LogEntry.PodName`, `LogEntry.Container`
- Implement `Size() int` → `100 + len(e.Raw) + len(e.JSONPretty)`
- Constructor: `func NewParser() Parser`

### File 5: `pkg/logparse/severity_test.go`

Table-driven tests covering:
- All severity keywords: ERROR, ERR, WARN, WARNING, INFO, DEBUG, TRACE, FATAL, PANIC, CRITICAL
- Various formats: `[ERROR]`, `level=error`, `"level":"error"`, `ERROR:`, ` ERROR `
- No keyword → SeverityUnknown
- Case insensitivity

### File 6: `pkg/logparse/timestamp_test.go`

Table-driven tests covering:
- All 5 timestamp formats listed above
- No timestamp → zero time
- Malformed timestamps
- Timezone handling

### File 7: `pkg/logparse/parser_test.go`

Table-driven tests + benchmarks:
- Various log formats (Java, Go, JSON, plain text, Kubernetes)
- JSON detection + formatting
- Size() calculation for normal and long (>10KB) lines
- `func BenchmarkDetectSeverity(b *testing.B)`
- `func BenchmarkParseTimestamp(b *testing.B)`

## Step 3: Verify

```bash
go test -v -race -bench=. ./pkg/logparse/...
go vet ./pkg/logparse/...
```

All tests must pass. Benchmarks must run.

## Critical Rules

- **ZERO dependencies**: only use `stdlib` — no third-party imports, no `internal/` imports
- **Parser must be stateless**: no fields on the struct, safe for concurrent use from multiple goroutines
- **Never modify `LogEntry.Raw`**: this field is the original log line, used for export and search
- **Compiled regex**: severity detection is on the hot path (called per log line), regex must be compiled once
- **This is `pkg/` not `internal/`**: public API, backward compatibility matters, do not change interface signatures from the skeleton
