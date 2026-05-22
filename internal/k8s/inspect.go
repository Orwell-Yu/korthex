package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Compile-time check: k8sClient implements PodInspector.
var _ PodInspector = (*k8sClient)(nil)

// GetPodContainerEnvs returns environment variables for each container in a pod.
// Key: container name, Value: slice of EnvVar (direct values only; secretKeyRef info included but not resolved).
func (c *k8sClient) GetPodContainerEnvs(namespace, podName string) (map[string][]EnvVar, error) {
	c.mu.RLock()
	informers := c.informers
	c.mu.RUnlock()

	if informers == nil {
		return nil, fmt.Errorf("inspect: not connected")
	}

	factory := informers.Access(namespace)
	factory.Core().V1().Pods().Informer()
	informers.EnsureSynced(namespace)

	lister := factory.Core().V1().Pods().Lister().Pods(namespace)
	podList, err := lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list pods for env scan: %w", err)
	}

	for _, pod := range podList {
		if pod.Name != podName {
			continue
		}
		result := make(map[string][]EnvVar, len(pod.Spec.Containers))
		for _, c := range pod.Spec.Containers {
			var envs []EnvVar
			for _, e := range c.Env {
				ev := EnvVar{Name: e.Name, Value: e.Value}
				if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
					ev.SecretName = e.ValueFrom.SecretKeyRef.Name
					ev.SecretKey = e.ValueFrom.SecretKeyRef.Key
				}
				envs = append(envs, ev)
			}
			result[c.Name] = envs
		}
		return result, nil
	}
	return nil, fmt.Errorf("pod %s/%s not found", namespace, podName)
}

// GetPodEnvFromSecrets returns envFrom secretRef entries for each container.
// Key: container name, Value: slice of EnvFromSource.
func (c *k8sClient) GetPodEnvFromSecrets(namespace, podName string) (map[string][]EnvFromSource, error) {
	c.mu.RLock()
	informers := c.informers
	c.mu.RUnlock()

	if informers == nil {
		return nil, fmt.Errorf("inspect: not connected")
	}

	factory := informers.Access(namespace)
	factory.Core().V1().Pods().Informer()
	informers.EnsureSynced(namespace)

	lister := factory.Core().V1().Pods().Lister().Pods(namespace)
	podList, err := lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list pods for envFrom scan: %w", err)
	}

	for _, pod := range podList {
		if pod.Name != podName {
			continue
		}
		result := make(map[string][]EnvFromSource, len(pod.Spec.Containers))
		for _, c := range pod.Spec.Containers {
			var sources []EnvFromSource
			for _, ef := range c.EnvFrom {
				if ef.SecretRef != nil {
					sources = append(sources, EnvFromSource{SecretName: ef.SecretRef.Name})
				}
			}
			result[c.Name] = sources
		}
		return result, nil
	}
	return nil, fmt.Errorf("pod %s/%s not found", namespace, podName)
}

// GetSecretData reads a K8s Secret and returns its data as string key-value pairs.
func (c *k8sClient) GetSecretData(ctx context.Context, namespace, secretName string) (map[string]string, error) {
	c.mu.RLock()
	clientset := c.clientset
	c.mu.RUnlock()

	if clientset == nil {
		return nil, fmt.Errorf("inspect: not connected")
	}

	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", namespace, secretName, err)
	}

	result := make(map[string]string, len(secret.Data))
	for k, v := range secret.Data {
		result[k] = string(v)
	}
	return result, nil
}
