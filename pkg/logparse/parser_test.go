package logparse

import (
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name       string
		podName    string
		container  string
		rawLine    string
		wantSev    Severity
		wantJSON   bool
		wantTSZero bool
	}{
		{
			name:      "java style error",
			podName:   "api-server-abc",
			container: "api",
			rawLine:   "2024-01-01 14:23:05.123 ERROR connection refused to database",
			wantSev:   SeverityError,
			wantJSON:  false,
		},
		{
			name:      "go style info",
			podName:   "worker-xyz",
			container: "worker",
			rawLine:   "2024/01/01 14:23:05 [INFO] processing batch 42",
			wantSev:   SeverityInfo,
			wantJSON:  false,
		},
		{
			name:       "json log line",
			podName:    "gateway-123",
			container:  "nginx",
			rawLine:    `{"level":"error","ts":"2024-01-01T14:23:05Z","msg":"upstream timeout"}`,
			wantSev:    SeverityError,
			wantJSON:   true,
			wantTSZero: true, // timestamp is inside JSON value, not at line start
		},
		{
			name:      "kubelet timestamps warn",
			podName:   "scheduler-001",
			container: "scheduler",
			rawLine:   "2024-01-01T14:23:05.123456789Z WARN high latency detected",
			wantSev:   SeverityWarn,
			wantJSON:  false,
		},
		{
			name:       "plain text no timestamp",
			podName:    "debug-pod",
			container:  "app",
			rawLine:    "just some plain log text",
			wantSev:    SeverityUnknown,
			wantJSON:   false,
			wantTSZero: true,
		},
		{
			name:       "empty line",
			podName:    "pod",
			container:  "c",
			rawLine:    "",
			wantSev:    SeverityUnknown,
			wantJSON:   false,
			wantTSZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := p.Parse(tt.podName, tt.container, tt.rawLine)

			if entry.Raw != tt.rawLine {
				t.Errorf("Raw = %q, want %q", entry.Raw, tt.rawLine)
			}
			if entry.PodName != tt.podName {
				t.Errorf("PodName = %q, want %q", entry.PodName, tt.podName)
			}
			if entry.Container != tt.container {
				t.Errorf("Container = %q, want %q", entry.Container, tt.container)
			}
			if entry.Severity != tt.wantSev {
				t.Errorf("Severity = %d, want %d", entry.Severity, tt.wantSev)
			}
			if entry.IsJSON != tt.wantJSON {
				t.Errorf("IsJSON = %v, want %v", entry.IsJSON, tt.wantJSON)
			}
			if tt.wantTSZero && !entry.Timestamp.IsZero() {
				t.Errorf("Timestamp = %v, want zero", entry.Timestamp)
			}
			if !tt.wantTSZero && entry.Timestamp.IsZero() {
				t.Errorf("Timestamp is zero, want non-zero")
			}
		})
	}
}

func TestParseJSONFormatting(t *testing.T) {
	p := NewParser()
	line := `{"level":"info","msg":"ok"}`
	entry := p.Parse("pod", "c", line)

	if !entry.IsJSON {
		t.Fatal("expected IsJSON=true")
	}
	if entry.JSONPretty == "" {
		t.Fatal("expected non-empty JSONPretty")
	}
	if !strings.Contains(entry.JSONPretty, "\n") {
		t.Error("expected JSONPretty to contain newlines (indented)")
	}
}

func TestSize(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		json string
		want int
	}{
		{"short line", "hello", "", 100 + 5},
		{"with json", "raw", "pretty", 100 + 3 + 6},
		{"empty", "", "", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &LogEntry{Raw: tt.raw, JSONPretty: tt.json}
			if got := e.Size(); got != tt.want {
				t.Errorf("Size() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSizeLongLine(t *testing.T) {
	longLine := strings.Repeat("x", 10240) // >10KB
	e := &LogEntry{Raw: longLine}
	expected := 100 + 10240
	if got := e.Size(); got != expected {
		t.Errorf("Size() = %d, want %d", got, expected)
	}
}

func TestParserConcurrentSafety(t *testing.T) {
	p := NewParser()
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = p.Parse("pod", "c", "2024-01-01T00:00:00Z ERROR test")
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func BenchmarkDetectSeverity(b *testing.B) {
	lines := []string{
		"[ERROR] connection refused",
		`{"level":"info","msg":"ok"}`,
		"2024-01-01T14:23:05Z WARN high latency",
		"just a plain line with no severity",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DetectSeverity(lines[i%len(lines)])
	}
}

func BenchmarkParseTimestamp(b *testing.B) {
	lines := []string{
		"2024-01-01T14:23:05.123456789Z",
		"2024-01-01 14:23:05.123",
		"2024/01/01 14:23:05",
		"2024-01-01T14:23:05.123Z ERROR something",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseTimestamp(lines[i%len(lines)])
	}
}

func BenchmarkParse(b *testing.B) {
	p := NewParser()
	line := `{"level":"error","ts":"2024-01-01T14:23:05Z","msg":"upstream timeout","latency":1.23}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse("pod-abc", "container", line)
	}
}

func TestParsePreservesRaw(t *testing.T) {
	p := NewParser()
	raw := "  some weird \t log line \n"
	entry := p.Parse("pod", "c", raw)
	if entry.Raw != raw {
		t.Errorf("Raw was modified: got %q, want %q", entry.Raw, raw)
	}
}

func TestParseTimezoneHandling(t *testing.T) {
	p := NewParser()
	line := "2024-01-01T14:23:05+08:00 INFO processing"
	entry := p.Parse("pod", "c", line)

	if entry.Timestamp.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}

	_, offset := entry.Timestamp.Zone()
	if offset != 8*3600 {
		t.Errorf("expected +08:00 offset (%d), got %d", 8*3600, offset)
	}
}

func TestNewParserNotNil(t *testing.T) {
	p := NewParser()
	if p == nil {
		t.Fatal("NewParser() returned nil")
	}

	// Should be usable immediately
	entry := p.Parse("pod", "c", "test line")
	if entry.Raw != "test line" {
		t.Errorf("unexpected Raw: %q", entry.Raw)
	}
}

func TestTimestampFormats(t *testing.T) {
	p := NewParser()

	// Verify all 5 format families produce non-zero timestamps
	formats := []string{
		"2024-01-01T14:23:05.123456789Z",              // RFC 3339 nano
		"2024-01-01T14:23:05+08:00",                   // RFC 3339 offset
		"2024-01-01 14:23:05.123",                     // Java style
		"2024/01/01 14:23:05",                         // Go default
		"2024-01-01T14:23:05.123Z some log line here", // Kubelet
	}

	for _, f := range formats {
		entry := p.Parse("pod", "c", f)
		if entry.Timestamp.IsZero() {
			t.Errorf("expected non-zero timestamp for %q", f)
		}
		expected := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		if entry.Timestamp.Year() != expected.Year() || entry.Timestamp.Month() != expected.Month() || entry.Timestamp.Day() != expected.Day() {
			t.Errorf("unexpected date for %q: got %v", f, entry.Timestamp)
		}
	}
}
