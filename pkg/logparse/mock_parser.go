package logparse

// MockParser is a test double for the Parser interface.
// Set ParseFunc to customize behavior per-test.
type MockParser struct {
	ParseFunc func(podName, container, rawLine string) LogEntry
}

func (m *MockParser) Parse(podName, container, rawLine string) LogEntry {
	if m.ParseFunc != nil {
		return m.ParseFunc(podName, container, rawLine)
	}
	return LogEntry{Raw: rawLine, PodName: podName, Container: container}
}
