package ui

import (
	"sync"

	"github.com/Orwell-Yu/korthex/pkg/logparse"
)

// RingBuffer is a thread-safe bounded ring buffer for log entries.
type RingBuffer struct {
	mu           sync.Mutex
	entries      []logparse.LogEntry
	capacity     int
	head         int // next write position
	count        int
	totalWritten int // monotonically increasing append counter
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer{
		entries:  make([]logparse.LogEntry, capacity),
		capacity: capacity,
	}
}

// Append adds an entry to the buffer, evicting the oldest entry when full.
func (rb *RingBuffer) Append(entry logparse.LogEntry) {
	rb.mu.Lock()
	rb.entries[rb.head] = entry
	rb.head = (rb.head + 1) % rb.capacity
	if rb.count < rb.capacity {
		rb.count++
	}
	rb.totalWritten++
	rb.mu.Unlock()
}

// Slice returns a copy of all entries in chronological order.
// The returned slice is a clone; callers may use it freely.
func (rb *RingBuffer) Slice() []logparse.LogEntry {
	rb.mu.Lock()
	n := rb.count
	if n == 0 {
		rb.mu.Unlock()
		return nil
	}
	out := make([]logparse.LogEntry, n)
	// The oldest entry is at (head - count) mod capacity.
	start := (rb.head - rb.count + rb.capacity) % rb.capacity
	for i := range n {
		out[i] = rb.entries[(start+i)%rb.capacity]
	}
	rb.mu.Unlock()
	return out
}

// Len returns the current number of entries in the buffer.
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	n := rb.count
	rb.mu.Unlock()
	return n
}

// Clear empties the buffer.
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	rb.head = 0
	rb.count = 0
	rb.totalWritten = 0
	rb.mu.Unlock()
}

// TotalWritten returns the total number of entries ever appended.
func (rb *RingBuffer) TotalWritten() int {
	rb.mu.Lock()
	n := rb.totalWritten
	rb.mu.Unlock()
	return n
}
