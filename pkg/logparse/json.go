package logparse

import (
	"bytes"
	"encoding/json"
)

// IsJSONLine reports whether line is a valid JSON value.
func IsJSONLine(line string) bool {
	return json.Valid([]byte(line))
}

// FormatJSON returns line as indented JSON. If line is not valid JSON,
// it returns the original line unchanged.
func FormatJSON(line string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(line), "", "  "); err != nil {
		return line
	}
	return buf.String()
}
