package logparse

import "testing"

func TestDetectSeverity(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Severity
	}{
		// FATAL / PANIC / CRITICAL
		{"fatal bracket", "[FATAL] out of memory", SeverityFatal},
		{"panic keyword", "PANIC: runtime error", SeverityFatal},
		{"critical keyword", "CRITICAL database connection lost", SeverityFatal},
		{"fatal lowercase", "[fatal] shutting down", SeverityFatal},

		// ERROR / ERR
		{"error bracket", "[ERROR] connection refused", SeverityError},
		{"err bracket", "[ERR] timeout", SeverityError},
		{"error colon", "ERROR: something went wrong", SeverityError},
		{"error space", " ERROR in processing", SeverityError},
		{"error level=", "level=error msg=failed", SeverityError},
		{"error json level", `{"level":"error","msg":"bad request"}`, SeverityError},
		{"error lowercase", "[error] failed to connect", SeverityError},
		{"ERR keyword", "ERR something happened", SeverityError},

		// WARN / WARNING
		{"warn bracket", "[WARN] high latency detected", SeverityWarn},
		{"warning bracket", "[WARNING] deprecated API call", SeverityWarn},
		{"warn level=", "level=warn msg=slow query", SeverityWarn},
		{"warning json", `{"level":"warning","msg":"deprecated"}`, SeverityWarn},
		{"warn lowercase", "[warn] retry in 5s", SeverityWarn},

		// INFO
		{"info bracket", "[INFO] server started on :8080", SeverityInfo},
		{"info level=", "level=info msg=healthy", SeverityInfo},
		{"info json", `{"level":"info","msg":"request completed"}`, SeverityInfo},
		{"info uppercase", "INFO starting worker pool", SeverityInfo},

		// DEBUG / TRACE
		{"debug bracket", "[DEBUG] cache hit for key=abc", SeverityDebug},
		{"trace bracket", "[TRACE] entering function Foo", SeverityDebug},
		{"debug level=", "level=debug msg=resolving host", SeverityDebug},
		{"trace json", `{"level":"trace","msg":"span started"}`, SeverityDebug},
		{"debug lowercase", "[debug] variable dump: x=42", SeverityDebug},

		// Unknown
		{"no keyword", "just a plain message", SeverityUnknown},
		{"empty", "", SeverityUnknown},
		{"numbers only", "12345", SeverityUnknown},
		{"error in word", "terrorize the system", SeverityUnknown},

		// Case insensitivity
		{"mixed case ERROR", "[Error] mixed case", SeverityError},
		{"mixed case Info", "Info: starting up", SeverityInfo},
		{"all caps WARN", "WARN: disk almost full", SeverityWarn},

		// Priority: FATAL > ERROR (line has both)
		{"fatal over error", "FATAL ERROR: system crash", SeverityFatal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectSeverity(tt.line)
			if got != tt.want {
				t.Errorf("DetectSeverity(%q) = %d, want %d", tt.line, got, tt.want)
			}
		})
	}
}
