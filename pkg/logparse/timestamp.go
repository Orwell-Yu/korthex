package logparse

import (
	"regexp"
	"time"
)

// Timestamp formats to try, in priority order.
var timestampLayouts = []string{
	time.RFC3339Nano,          // 2024-01-01T14:23:05.123456789Z
	time.RFC3339,              // 2024-01-01T14:23:05Z or +08:00
	"2006-01-02 15:04:05.000", // Java style: yyyy-MM-dd HH:mm:ss.SSS
	"2006-01-02 15:04:05",     // Java style without millis
	"2006/01/02 15:04:05",     // Go default
}

// kubeletTimestampPattern matches a leading RFC3339-like timestamp followed by a space.
// This handles kubelet --timestamps format: "2024-01-01T14:23:05.123456789Z rest of line"
var kubeletTimestampPattern = regexp.MustCompile(
	`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))\s`,
)

// ParseTimestamp attempts to extract a timestamp from a log line.
// Tries multiple formats in priority order. Returns time.Time{} (zero value)
// if no timestamp can be parsed. Never panics.
func ParseTimestamp(line string) time.Time {
	// Try direct parsing of the full line with known layouts
	for _, layout := range timestampLayouts {
		// Try parsing just the beginning of the line (length of layout format)
		if len(line) >= len(layout) {
			if t, err := time.Parse(layout, line[:len(layout)]); err == nil {
				return t
			}
		}
		// Also try the full line for short lines
		if t, err := time.Parse(layout, line); err == nil {
			return t
		}
	}

	// Try kubelet --timestamps format: extract leading timestamp
	if m := kubeletTimestampPattern.FindStringSubmatch(line); m != nil {
		for _, layout := range timestampLayouts {
			if t, err := time.Parse(layout, m[1]); err == nil {
				return t
			}
		}
	}

	return time.Time{}
}
