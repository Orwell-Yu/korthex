package logparse

import (
	"testing"
	"time"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantStr string // expected time formatted as RFC3339Nano, or "" for zero
	}{
		// RFC 3339 / ISO 8601
		{
			"rfc3339 nano UTC",
			"2024-01-01T14:23:05.123456789Z",
			"2024-01-01T14:23:05.123456789Z",
		},
		{
			"rfc3339 UTC",
			"2024-01-01T14:23:05Z",
			"2024-01-01T14:23:05Z",
		},

		// RFC 3339 with offset
		{
			"rfc3339 offset +08:00",
			"2024-01-01T14:23:05+08:00",
			"2024-01-01T14:23:05+08:00",
		},
		{
			"rfc3339 offset -05:00",
			"2024-06-15T09:30:00-05:00",
			"2024-06-15T09:30:00-05:00",
		},

		// Java style
		{
			"java style with millis",
			"2024-01-01 14:23:05.123",
			"2024-01-01T14:23:05.123Z",
		},
		{
			"java style no millis",
			"2024-01-01 14:23:05",
			"2024-01-01T14:23:05Z",
		},

		// Go default
		{
			"go default",
			"2024/01/01 14:23:05",
			"2024-01-01T14:23:05Z",
		},

		// Kubelet --timestamps (timestamp followed by log content)
		{
			"kubelet timestamps nano",
			"2024-01-01T14:23:05.123456789Z ERROR something bad happened",
			"2024-01-01T14:23:05.123456789Z",
		},
		{
			"kubelet timestamps millis",
			"2024-01-01T14:23:05.123Z some log line here",
			"2024-01-01T14:23:05.123Z",
		},
		{
			"kubelet timestamps with offset",
			"2024-01-01T14:23:05+08:00 INFO starting",
			"2024-01-01T14:23:05+08:00",
		},

		// No timestamp
		{"no timestamp", "just a plain log line", ""},
		{"empty line", "", ""},
		{"garbage", "not-a-date at all", ""},

		// Malformed timestamps
		{"partial date", "2024-01-01", ""},
		{"time only", "14:23:05", ""},
		{"invalid month", "2024-13-01T00:00:00Z", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTimestamp(tt.line)

			if tt.wantStr == "" {
				if !got.IsZero() {
					t.Errorf("ParseTimestamp(%q) = %v, want zero time", tt.line, got)
				}
				return
			}

			want, err := time.Parse(time.RFC3339Nano, tt.wantStr)
			if err != nil {
				// Try RFC3339 without nano
				want, err = time.Parse(time.RFC3339, tt.wantStr)
				if err != nil {
					t.Fatalf("bad test: cannot parse wantStr %q: %v", tt.wantStr, err)
				}
			}

			if !got.Equal(want) {
				t.Errorf("ParseTimestamp(%q) = %v, want %v", tt.line, got, want)
			}
		})
	}
}
