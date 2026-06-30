package k8s

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// Compile-time check: k8sClient implements PodExecutor.
var _ PodExecutor = (*k8sClient)(nil)

// ExecInPod executes a command inside a pod container via SPDY.
// If container is empty, K8s defaults to the first container.
func (c *k8sClient) ExecInPod(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
	return c.ExecInPodWithStdin(ctx, namespace, podName, container, command, nil)
}

// ExecInPodWithStdin is like ExecInPod but pipes stdin into the command. Used to
// pass secrets (e.g. a DB password) to the in-pod process WITHOUT placing them in
// the command argv — argv is visible in API Server audit logs and `ps` output,
// stdin is not.
func (c *k8sClient) ExecInPodWithStdin(ctx context.Context, namespace, podName, container string, command []string, stdin []byte) ([]byte, []byte, error) {
	c.mu.RLock()
	clientset := c.clientset
	restConfig := c.restConfig
	c.mu.RUnlock()

	if clientset == nil || restConfig == nil {
		return nil, nil, fmt.Errorf("exec: not connected")
	}

	useStdin := len(stdin) > 0

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     useStdin,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return nil, nil, fmt.Errorf("create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	streamOpts := remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if useStdin {
		streamOpts.Stdin = bytes.NewReader(stdin)
	}

	err = exec.StreamWithContext(ctx, streamOpts)
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("exec in pod %s/%s: %w", namespace, podName, err)
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}
