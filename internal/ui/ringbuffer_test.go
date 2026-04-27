package ui

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Orwell-Yu/korthex/pkg/logparse"
)

func entry(raw string) logparse.LogEntry {
	return logparse.LogEntry{Raw: raw}
}

func TestRingBuffer_AppendBelowCapacity(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Append(entry("a"))
	rb.Append(entry("b"))
	rb.Append(entry("c"))

	got := rb.Slice()
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if got[i].Raw != w {
			t.Errorf("index %d: want %q, got %q", i, w, got[i].Raw)
		}
	}
}

func TestRingBuffer_AppendBeyondCapacity(t *testing.T) {
	rb := NewRingBuffer(3)
	for i := range 5 {
		rb.Append(entry(fmt.Sprintf("%d", i)))
	}

	got := rb.Slice()
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	// Oldest 0,1 evicted; remaining: 2,3,4 in order
	want := []string{"2", "3", "4"}
	for i, w := range want {
		if got[i].Raw != w {
			t.Errorf("index %d: want %q, got %q", i, w, got[i].Raw)
		}
	}
}

func TestRingBuffer_Len(t *testing.T) {
	rb := NewRingBuffer(3)
	if rb.Len() != 0 {
		t.Fatalf("empty buffer Len() = %d, want 0", rb.Len())
	}
	rb.Append(entry("a"))
	if rb.Len() != 1 {
		t.Fatalf("after 1 append Len() = %d, want 1", rb.Len())
	}
	rb.Append(entry("b"))
	rb.Append(entry("c"))
	rb.Append(entry("d")) // wraps
	if rb.Len() != 3 {
		t.Fatalf("after overflow Len() = %d, want 3", rb.Len())
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Append(entry("a"))
	rb.Append(entry("b"))
	rb.Clear()

	if rb.Len() != 0 {
		t.Fatalf("after Clear() Len() = %d, want 0", rb.Len())
	}
	got := rb.Slice()
	if got != nil {
		t.Fatalf("after Clear() Slice() should be nil, got %v", got)
	}
}

func TestRingBuffer_SliceIsClone(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Append(entry("a"))
	rb.Append(entry("b"))

	s1 := rb.Slice()
	s1[0].Raw = "MODIFIED"

	s2 := rb.Slice()
	if s2[0].Raw != "a" {
		t.Fatalf("Slice returned internal reference, not clone: got %q", s2[0].Raw)
	}
}

func TestRingBuffer_EmptySlice(t *testing.T) {
	rb := NewRingBuffer(5)
	got := rb.Slice()
	if got != nil {
		t.Fatalf("empty buffer Slice() should be nil, got %v", got)
	}
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	rb := NewRingBuffer(100)
	var wg sync.WaitGroup

	// Writers
	for w := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range 500 {
				rb.Append(entry(fmt.Sprintf("w%d-%d", id, i)))
			}
		}(w)
	}

	// Readers
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				_ = rb.Slice()
				_ = rb.Len()
			}
		}()
	}

	wg.Wait()

	// After 2000 writes into capacity 100, should be full.
	if rb.Len() != 100 {
		t.Fatalf("after concurrent writes Len() = %d, want 100", rb.Len())
	}
}

func TestRingBuffer_OrderAfterMultipleWraps(t *testing.T) {
	rb := NewRingBuffer(3)
	// Write 10 entries so we wrap multiple times
	for i := range 10 {
		rb.Append(entry(fmt.Sprintf("%d", i)))
	}
	got := rb.Slice()
	want := []string{"7", "8", "9"}
	for i, w := range want {
		if got[i].Raw != w {
			t.Errorf("index %d: want %q, got %q", i, w, got[i].Raw)
		}
	}
}
