# pkg/logparse - Log Parsing Utilities

> **Parent docs:** [CLAUDE.md (root)](../../CLAUDE.md) | [SPEC.md Section 3.2](../../SPEC.md) | [PRD Section 5.2](../../PRD.md)
>
> **Module rules:** [CLAUDE.md (this module)](./CLAUDE.md)

## Responsibility

Parse raw log lines into structured `LogEntry` values. Detect severity level, parse timestamps across multiple formats, detect and format JSON logs. Stateless and concurrent-safe.

## Public Interfaces

```go
// Parser is stateless and safe for concurrent use
type Parser interface {
    Parse(podName, container, rawLine string) LogEntry
}
```

## Key Types

```go
type Severity int
const (
    SeverityUnknown Severity = iota
    SeverityDebug   // DEBUG, TRACE
    SeverityInfo    // INFO
    SeverityWarn    // WARN, WARNING
    SeverityError   // ERROR, ERR
    SeverityFatal   // FATAL, PANIC, CRITICAL
)

type LogEntry struct {
    Timestamp  time.Time  // zero if unparsable
    Severity   Severity
    PodName    string
    Container  string
    Raw        string     // original line, never modified
    IsJSON     bool
    JSONPretty string     // formatted JSON when IsJSON=true
}
```

## Files

| File | Responsibility |
|------|---------------|
| `parser.go` | LogEntry struct, Parser implementation, `Size()` method (memory estimation) |
| `severity.go` | `DetectSeverity(line) Severity` - priority-ordered compiled regex table: FATAL > ERROR > WARN > INFO > DEBUG |
| `json.go` | `IsJSONLine(line) bool`, `FormatJSON(line) string` - JSON detection and pretty-printing |
| `timestamp.go` | `ParseTimestamp(line) time.Time` - multi-format parsing: ISO 8601, RFC 3339, Java `yyyy-MM-dd HH:mm:ss.SSS`, Go default, kubelet `--timestamps` |
| `parser_test.go` | Table-driven parse tests (various log formats) |
| `severity_test.go` | Severity detection edge cases |
| `timestamp_test.go` | Multi-format timestamp parsing tests |

## Dependencies

- **External**: None (pure stdlib)
- **Internal**: None (Layer 0)

## Design Notes

- This is `pkg/` (not `internal/`) because it is a self-contained utility useful to external consumers
- Parser is stateless: each `Parse()` call is independent, enabling trivially concurrent usage
- `Size()` returns approximate memory footprint: `100 + len(Raw) + len(JSONPretty)` bytes
- Severity detection uses compiled regex (sync.Once) for performance

## Testing Strategy

- **Table-driven tests** for each function with diverse log formats:
  - Java-style: `2024-01-01 14:23:05.123 ERROR ...`
  - Go-style: `2024/01/01 14:23:05 [ERROR] ...`
  - JSON: `{"level":"error","msg":"...",...}`
  - Kubernetes native: `2024-01-01T14:23:05.123456789Z ERROR ...`
  - Plain text with no timestamp/severity
- **Benchmark tests** for `DetectSeverity` and `ParseTimestamp` (hot path)

## Phase 2+ Extension Points

- Add severity-based log statistics (`CountBySeverity([]LogEntry) map[Severity]int`)
- Add log pattern recognition (repeated error detection)
- Add custom timestamp format configuration
