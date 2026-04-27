package logparse

import "time"

// Severity represents the severity level of a log line.
type Severity int

const (
	SeverityUnknown Severity = iota
	SeverityDebug
	SeverityInfo
	SeverityWarn
	SeverityError
	SeverityFatal
)

// LogEntry is the parsed result of a single log line.
type LogEntry struct {
	Timestamp  time.Time // zero value if unparsable
	Severity   Severity
	PodName    string
	Container  string
	Raw        string // original line (never modified)
	IsJSON     bool
	JSONPretty string // formatted JSON (only when IsJSON=true)
}

// Size returns the approximate memory footprint of this LogEntry in bytes.
func (e *LogEntry) Size() int {
	return 100 + len(e.Raw) + len(e.JSONPretty)
}

// Parser is a stateless log parser, safe for concurrent use.
type Parser interface {
	Parse(podName, container, rawLine string) LogEntry
}

// defaultParser implements Parser. It is stateless and safe for concurrent use.
type defaultParser struct{}

// NewParser returns a new Parser instance.
func NewParser() Parser {
	return &defaultParser{}
}

// Parse parses a raw log line into a structured LogEntry.
func (p *defaultParser) Parse(podName, container, rawLine string) LogEntry {
	entry := LogEntry{
		PodName:   podName,
		Container: container,
		Raw:       rawLine,
		Timestamp: ParseTimestamp(rawLine),
		Severity:  DetectSeverity(rawLine),
	}

	if IsJSONLine(rawLine) {
		entry.IsJSON = true
		entry.JSONPretty = FormatJSON(rawLine)
	}

	return entry
}
