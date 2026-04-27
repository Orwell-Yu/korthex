package logparse

import "regexp"

// Compiled severity patterns — compiled once at package init, reused for every call.
// Priority order: FATAL/PANIC/CRITICAL > ERROR/ERR > WARN/WARNING > INFO > DEBUG/TRACE
var severityPatterns []severityRule

type severityRule struct {
	severity Severity
	pattern  *regexp.Regexp
}

func init() {
	severityPatterns = []severityRule{
		{SeverityFatal, regexp.MustCompile(`(?i)(?:\[|"|^|\s)(?:FATAL|PANIC|CRITICAL)(?:\]|"|:|\s|$)`)},
		{SeverityError, regexp.MustCompile(`(?i)(?:\[|"|^|\s)(?:ERROR|ERR)(?:\]|"|:|\s|$)`)},
		{SeverityWarn, regexp.MustCompile(`(?i)(?:\[|"|^|\s)(?:WARN|WARNING)(?:\]|"|:|\s|$)`)},
		{SeverityInfo, regexp.MustCompile(`(?i)(?:\[|"|^|\s)INFO(?:\]|"|:|\s|$)`)},
		{SeverityDebug, regexp.MustCompile(`(?i)(?:\[|"|^|\s)(?:DEBUG|TRACE)(?:\]|"|:|\s|$)`)},
	}
}

// severityLevelValues maps lowercase level strings to Severity for key=value and JSON formats.
var severityLevelValues = map[string]Severity{
	"fatal":    SeverityFatal,
	"panic":    SeverityFatal,
	"critical": SeverityFatal,
	"error":    SeverityError,
	"err":      SeverityError,
	"warn":     SeverityWarn,
	"warning":  SeverityWarn,
	"info":     SeverityInfo,
	"debug":    SeverityDebug,
	"trace":    SeverityDebug,
}

// levelValuePattern matches level=error or "level":"error" formats.
var levelValuePattern = regexp.MustCompile(`(?i)(?:"level"\s*:\s*"([^"]+)"|level=([^\s,;]+))`)

// DetectSeverity returns the severity level detected in the log line.
// Uses compiled regex patterns with priority ordering: FATAL > ERROR > WARN > INFO > DEBUG.
// Returns SeverityUnknown if no severity keyword is found.
func DetectSeverity(line string) Severity {
	// First try structured formats: level=X or "level":"X"
	if matches := levelValuePattern.FindStringSubmatch(line); matches != nil {
		val := matches[1]
		if val == "" {
			val = matches[2]
		}
		// Normalize to lowercase for lookup
		lower := toLower(val)
		if sev, ok := severityLevelValues[lower]; ok {
			return sev
		}
	}

	// Fall back to keyword scanning with priority order
	for _, rule := range severityPatterns {
		if rule.pattern.MatchString(line) {
			return rule.severity
		}
	}

	return SeverityUnknown
}

// toLower is a simple ASCII lowercase without pulling in strings package.
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
