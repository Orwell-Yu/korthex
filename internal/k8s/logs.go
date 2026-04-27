package k8s

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const logChannelBuffer = 50

// logStreamer implements LogStreamer using the K8s GetLogs API.
type logStreamer struct {
	clientset kubernetes.Interface
	resources *resourceLister
}

// StreamLogs opens a streaming connection and sends log lines to ch.
// It blocks until ctx is cancelled, the stream ends, or a fatal error occurs.
func (l *logStreamer) StreamLogs(ctx context.Context, req LogRequest, ch chan<- LogLine) error {
	containers, err := l.resolveContainers(ctx, req)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	for _, container := range containers {
		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			l.streamContainerLogs(ctx, req, c, ch)
		}(container)
	}
	wg.Wait()
	return nil
}

// GetLogs fetches logs non-streaming and returns all lines.
func (l *logStreamer) GetLogs(ctx context.Context, req LogRequest) ([]LogLine, error) {
	req.Follow = false // batch fetch must not follow

	ch := make(chan LogLine, logChannelBuffer)
	var lines []LogLine

	var streamErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(ch)
		streamErr = l.StreamLogs(ctx, req, ch)
	}()

	// Collect until both goroutine finishes and channel drains.
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				<-done
				return lines, streamErr
			}
			lines = append(lines, line)
		case <-done:
			// Drain remaining.
			for line := range ch {
				lines = append(lines, line)
			}
			return lines, streamErr
		}
	}
}

// StreamMultiPodLogs streams logs from all pods matching selector into ch.
func (l *logStreamer) StreamMultiPodLogs(ctx context.Context, namespace string,
	selector map[string]string, opts LogRequest, ch chan<- LogLine) error {

	pods, err := l.resources.ListPodsBySelector(namespace, selector)
	if err != nil {
		return fmt.Errorf("list pods for multi-stream: %w", err)
	}

	if len(pods) == 0 {
		return fmt.Errorf("no pods match selector in namespace %s", namespace)
	}

	var wg sync.WaitGroup
	for _, pod := range pods {
		podReq := opts
		podReq.Namespace = namespace
		podReq.PodName = pod.Name

		wg.Add(1)
		go func(r LogRequest) {
			defer wg.Done()
			if err := l.StreamLogs(ctx, r, ch); err != nil {
				slog.Warn("multi-pod stream error", "pod", r.PodName, "err", err)
			}
		}(podReq)
	}
	wg.Wait()
	return nil
}

// resolveContainers determines which containers to stream logs from.
func (l *logStreamer) resolveContainers(ctx context.Context, req LogRequest) ([]string, error) {
	if req.Container != "" {
		return []string{req.Container}, nil
	}

	pod, err := l.clientset.CoreV1().Pods(req.Namespace).Get(ctx, req.PodName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get pod %s/%s: %w", req.Namespace, req.PodName, err)
	}

	containers := make([]string, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		containers = append(containers, c.Name)
	}
	return containers, nil
}

// streamContainerLogs streams logs from a single container with exponential backoff retry.
func (l *logStreamer) streamContainerLogs(ctx context.Context, req LogRequest, container string, ch chan<- LogLine) {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 500 * time.Millisecond
	b.MaxInterval = 15 * time.Second
	b.MaxElapsedTime = 0 // controlled by maxRetries

	attempts := 0
	const maxRetries = 20

	operation := func() error {
		attempts++
		if attempts > maxRetries {
			return backoff.Permanent(fmt.Errorf("max retries (%d) exceeded for %s/%s", maxRetries, req.PodName, container))
		}

		// Check if pod is in terminal state before retrying.
		if attempts > 1 {
			if terminal, _ := l.isPodTerminal(ctx, req.Namespace, req.PodName); terminal {
				return backoff.Permanent(fmt.Errorf("pod %s is in terminal state", req.PodName))
			}
		}

		opts := l.buildLogOptions(req, container)
		stream, err := l.clientset.CoreV1().Pods(req.Namespace).GetLogs(req.PodName, opts).Stream(ctx)
		if err != nil {
			slog.Warn("log stream open failed", "pod", req.PodName, "container", container, "attempt", attempts, "err", err)
			return err
		}
		defer stream.Close()

		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return backoff.Permanent(ctx.Err())
			default:
			}

			line := LogLine{
				PodName:   req.PodName,
				Container: container,
				Content:   scanner.Text(),
			}
			// Non-blocking send: drop line if consumer is slow.
			select {
			case ch <- line:
			default:
			}
		}

		if err := scanner.Err(); err != nil {
			if ctx.Err() != nil {
				return backoff.Permanent(ctx.Err())
			}
			slog.Warn("log stream read error", "pod", req.PodName, "container", container, "err", err)
			return err
		}

		return nil
	}

	if err := backoff.Retry(operation, backoff.WithContext(b, ctx)); err != nil {
		slog.Debug("log stream ended", "pod", req.PodName, "container", container, "err", err)
	}
}

func (l *logStreamer) buildLogOptions(req LogRequest, container string) *corev1.PodLogOptions {
	opts := &corev1.PodLogOptions{
		Container:  container,
		Follow:     req.Follow,
		Previous:   req.Previous,
		Timestamps: req.AddTimestamps,
	}
	if req.TailLines != nil {
		opts.TailLines = req.TailLines
	}
	if req.Since > 0 {
		seconds := int64(req.Since.Seconds())
		opts.SinceSeconds = &seconds
	}
	if req.SinceTime != nil {
		t := metav1.NewTime(*req.SinceTime)
		opts.SinceTime = &t
	}
	return opts
}

// isPodTerminal checks if a pod is in Succeeded, Failed, or deleted state.
func (l *logStreamer) isPodTerminal(ctx context.Context, namespace, podName string) (bool, error) {
	pod, err := l.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return true, err // Assume terminal if we can't reach it.
	}
	switch pod.Status.Phase {
	case corev1.PodSucceeded, corev1.PodFailed:
		return true, nil
	case corev1.PodPending, corev1.PodRunning, corev1.PodUnknown:
		return false, nil
	}
	return false, nil
}

// readAllFromStream is a helper used in tests.
func readAllFromStream(r io.Reader, podName, container string, ch chan<- LogLine) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := LogLine{
			PodName:   podName,
			Container: container,
			Content:   scanner.Text(),
		}
		select {
		case ch <- line:
		default:
		}
	}
}
