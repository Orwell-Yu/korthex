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
	IsConnected() bool
	CurrentContext() string

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

// k8sClient is the concrete implementation of Client.
type k8sClient struct {
	mu         sync.RWMutex
	clientset  kubernetes.Interface
	restConfig *rest.Config
	context    string
	connected  bool

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
