package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client is the top-level K8s interface, aggregating all sub-interfaces.
type Client interface {
	Connect(kubeconfig, context string) error
	Disconnect()
	Reconnect(kubeconfig, context string) error
	IsConnected() bool
	CurrentContext() string
	ContextInfo() (kubeconfig string, context string)

	Resources() ResourceLister
	Logs() LogStreamer
	Events() EventLister
	Describer() ResourceDescriber
}

// ResourceLister provides resource discovery from Informer cache.
type ResourceLister interface {
	ListNamespaces() ([]Namespace, error)
	ListDeployments(namespace string) ([]Deployment, error)
	ListPods(namespace string) ([]Pod, error)
	ListPodsBySelector(namespace string, selector map[string]string) ([]Pod, error)
	FindDeploymentByName(namespace, name string) (*Deployment, error)
	SearchResources(namespace, query string) ([]SearchResult, error)

	// Phase 2 resource types
	ListStatefulSets(namespace string) ([]StatefulSet, error)
	ListDaemonSets(namespace string) ([]DaemonSet, error)
	ListJobs(namespace string) ([]Job, error)
	ListCronJobs(namespace string) ([]CronJob, error)
}

// LogStreamer provides log fetching and streaming.
type LogStreamer interface {
	StreamLogs(ctx context.Context, req LogRequest, ch chan<- LogLine) error
	GetLogs(ctx context.Context, req LogRequest) ([]LogLine, error)
	StreamMultiPodLogs(ctx context.Context, namespace string,
		selector map[string]string, opts LogRequest, ch chan<- LogLine) error
}

// EventLister provides event querying.
type EventLister interface {
	ListEvents(namespace string) ([]Event, error)
	ListEventsForResource(namespace, kind, name string) ([]Event, error)
}

// ResourceDescriber provides describe aggregation.
type ResourceDescriber interface {
	Describe(namespace, kind, name string) (string, error)
}

// PodExecutor provides kubectl exec capability for running commands inside pods.
type PodExecutor interface {
	ExecInPod(ctx context.Context, namespace, podName, container string, command []string) (stdout []byte, stderr []byte, err error)
}

// PodInspector provides access to pod spec details (env vars) and K8s secrets.
type PodInspector interface {
	GetPodContainerEnvs(namespace, podName string) (map[string][]EnvVar, error)
	GetPodEnvFromSecrets(namespace, podName string) (map[string][]EnvFromSource, error)
	GetSecretData(ctx context.Context, namespace, secretName string) (map[string]string, error)
}

// k8sClient is the concrete implementation of Client.
type k8sClient struct {
	mu             sync.RWMutex
	clientset      kubernetes.Interface
	restConfig     *rest.Config
	context        string
	kubeconfigPath string
	connected      bool

	informers *InformerManager
	resources *resourceLister
	logs      *logStreamer
	events    *eventLister
	describer *resourceDescriber
}

// NewClient creates a new K8s client.
func NewClient() Client {
	return &k8sClient{}
}

// Connect loads kubeconfig and establishes a connection to the cluster.
func (c *k8sClient) Connect(kubeconfig, ctx string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return fmt.Errorf("already connected; call Disconnect() first")
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if ctx != "" {
		overrides.CurrentContext = ctx
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("build rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create clientset: %w", err)
	}

	// Resolve the actual context name used.
	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		return fmt.Errorf("load raw config: %w", err)
	}
	resolvedContext := rawConfig.CurrentContext
	if ctx != "" {
		resolvedContext = ctx
	}

	c.clientset = clientset
	c.restConfig = restConfig
	c.context = resolvedContext
	c.connected = true
	c.kubeconfigPath = kubeconfig

	c.informers = NewInformerManager(clientset, 10)
	c.resources = &resourceLister{clientset: clientset, informers: c.informers}
	c.logs = &logStreamer{clientset: clientset, resources: c.resources}
	c.events = &eventLister{clientset: clientset}
	c.describer = &resourceDescriber{clientset: clientset, events: c.events}

	slog.Debug("k8s connected", "context", resolvedContext)
	return nil
}

// Disconnect stops all informers and cleans up resources.
func (c *k8sClient) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return
	}

	if c.informers != nil {
		c.informers.StopAll()
	}

	c.clientset = nil
	c.restConfig = nil
	c.context = ""
	c.kubeconfigPath = ""
	c.connected = false
	c.informers = nil
	c.resources = nil
	c.logs = nil
	c.events = nil
	c.describer = nil

	slog.Debug("k8s disconnected")
}

// IsConnected returns true if the client is connected.
func (c *k8sClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// CurrentContext returns the active kubeconfig context name.
func (c *k8sClient) CurrentContext() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.context
}

// ContextInfo returns the kubeconfig path and context name.
func (c *k8sClient) ContextInfo() (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.kubeconfigPath, c.context
}

// Reconnect validates a new connection and hot-swaps it in place of the old one.
// If validation fails, the old connection remains intact.
func (c *k8sClient) Reconnect(kubeconfig, ctx string) error {
	// Build new config outside lock
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if ctx != "" {
		overrides.CurrentContext = ctx
	}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("build rest config: %w", err)
	}
	newClientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create clientset: %w", err)
	}

	// Validate new connection
	if _, err := newClientset.Discovery().ServerVersion(); err != nil {
		return fmt.Errorf("validate connection: %w", err)
	}

	// Resolve context
	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		return fmt.Errorf("load raw config: %w", err)
	}
	resolvedContext := rawConfig.CurrentContext
	if ctx != "" {
		resolvedContext = ctx
	}

	// Lock and swap
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.informers != nil {
		c.informers.StopAll()
	}

	c.clientset = newClientset
	c.restConfig = restConfig
	c.context = resolvedContext
	c.kubeconfigPath = kubeconfig
	c.connected = true

	c.informers = NewInformerManager(newClientset, 10)
	c.resources = &resourceLister{clientset: newClientset, informers: c.informers}
	c.logs = &logStreamer{clientset: newClientset, resources: c.resources}
	c.events = &eventLister{clientset: newClientset}
	c.describer = &resourceDescriber{clientset: newClientset, events: c.events}

	slog.Debug("k8s reconnected", "context", resolvedContext, "kubeconfig", kubeconfig)
	return nil
}

func (c *k8sClient) Resources() ResourceLister {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resources
}

func (c *k8sClient) Logs() LogStreamer {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.logs
}

func (c *k8sClient) Events() EventLister {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.events
}

func (c *k8sClient) Describer() ResourceDescriber {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.describer
}
