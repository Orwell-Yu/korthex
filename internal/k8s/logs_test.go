package k8s

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errReader returns lines then an error.
type errReader struct {
	lines []string
	idx   int
	err   error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.idx < len(r.lines) {
		line := r.lines[r.idx] + "\n"
		r.idx++
		n := copy(p, line)
		return n, nil
	}
	if r.err != nil {
		return 0, r.err
	}
	return 0, io.EOF
}

func TestReadAllFromStream_HappyPath(t *testing.T) {
	input := "line 1\nline 2\nline 3\n"
	reader := strings.NewReader(input)

	ch := make(chan LogLine, 10)
	readAllFromStream(reader, "pod-1", "container-1", ch)
	close(ch)

	var lines []LogLine
	for l := range ch {
		lines = append(lines, l)
	}

	require.Len(t, lines, 3)
	assert.Equal(t, "line 1", lines[0].Content)
	assert.Equal(t, "line 2", lines[1].Content)
	assert.Equal(t, "line 3", lines[2].Content)

	for _, l := range lines {
		assert.Equal(t, "pod-1", l.PodName)
		assert.Equal(t, "container-1", l.Container)
	}
}

func TestReadAllFromStream_ErrorMidStream(t *testing.T) {
	reader := &errReader{
		lines: []string{"good line 1", "good line 2"},
		err:   errors.New("network error"),
	}

	ch := make(chan LogLine, 10)
	readAllFromStream(reader, "pod-err", "main", ch)
	close(ch)

	var lines []LogLine
	for l := range ch {
		lines = append(lines, l)
	}

	// Should have received the good lines before the error.
	assert.Len(t, lines, 2)
	assert.Equal(t, "good line 1", lines[0].Content)
	assert.Equal(t, "good line 2", lines[1].Content)
}

func TestReadAllFromStream_ContextCancellation(t *testing.T) {
	// Simulate a slow reader that blocks.
	ctx, cancel := context.WithCancel(context.Background())

	// Use a pipe: we control when data arrives.
	pr, pw := io.Pipe()

	ch := make(chan LogLine, logChannelBuffer)
	done := make(chan struct{})
	go func() {
		defer close(done)
		readAllFromStream(pr, "pod-cancel", "main", ch)
	}()

	// Write one line.
	_, err := pw.Write([]byte("line before cancel\n"))
	require.NoError(t, err)

	// Give time for the line to be read.
	time.Sleep(50 * time.Millisecond)

	// Cancel context and close the writer to unblock the scanner.
	cancel()
	_ = ctx // use ctx to suppress lint
	pw.Close()

	<-done
	close(ch)

	var lines []LogLine
	for l := range ch {
		lines = append(lines, l)
	}
	assert.GreaterOrEqual(t, len(lines), 1)
	assert.Equal(t, "line before cancel", lines[0].Content)
}

func TestReadAllFromStream_NonBlockingSend(t *testing.T) {
	// Channel with capacity 2, send 5 lines → 3 should be dropped silently.
	ch := make(chan LogLine, 2)
	input := "line1\nline2\nline3\nline4\nline5\n"
	reader := strings.NewReader(input)

	readAllFromStream(reader, "pod-nb", "main", ch)
	close(ch)

	var lines []LogLine
	for l := range ch {
		lines = append(lines, l)
	}
	// We should get exactly 2 (channel capacity), rest dropped.
	assert.Equal(t, 2, len(lines))
}

func TestReadAllFromStream_MultipleContainersConcurrent(t *testing.T) {
	ch := make(chan LogLine, 100)

	var wg sync.WaitGroup
	containers := []struct {
		pod       string
		container string
		data      string
	}{
		{"pod-1", "app", "app-log-1\napp-log-2\n"},
		{"pod-1", "sidecar", "sidecar-log-1\n"},
		{"pod-2", "app", "pod2-log-1\npod2-log-2\npod2-log-3\n"},
	}

	for _, c := range containers {
		wg.Add(1)
		go func(pod, container, data string) {
			defer wg.Done()
			readAllFromStream(bytes.NewBufferString(data), pod, container, ch)
		}(c.pod, c.container, c.data)
	}

	wg.Wait()
	close(ch)

	var lines []LogLine
	for l := range ch {
		lines = append(lines, l)
	}

	// Total: 2 + 1 + 3 = 6 lines.
	assert.Equal(t, 6, len(lines))

	// Verify all containers are represented.
	byKey := make(map[string]int)
	for _, l := range lines {
		byKey[l.PodName+"/"+l.Container]++
	}
	assert.Equal(t, 2, byKey["pod-1/app"])
	assert.Equal(t, 1, byKey["pod-1/sidecar"])
	assert.Equal(t, 3, byKey["pod-2/app"])
}

func TestReadAllFromStream_EmptyInput(t *testing.T) {
	reader := strings.NewReader("")
	ch := make(chan LogLine, 10)
	readAllFromStream(reader, "pod-empty", "main", ch)
	close(ch)

	var lines []LogLine
	for l := range ch {
		lines = append(lines, l)
	}
	assert.Empty(t, lines)
}
