package k8s

import "context"

// MockPodExecutor is a test double for the PodExecutor interface.
type MockPodExecutor struct {
	ExecInPodFunc          func(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error)
	ExecInPodWithStdinFunc func(ctx context.Context, namespace, podName, container string, command []string, stdin []byte) ([]byte, []byte, error)
}

func (m *MockPodExecutor) ExecInPod(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
	if m.ExecInPodFunc != nil {
		return m.ExecInPodFunc(ctx, namespace, podName, container, command)
	}
	return nil, nil, nil
}

func (m *MockPodExecutor) ExecInPodWithStdin(ctx context.Context, namespace, podName, container string, command []string, stdin []byte) ([]byte, []byte, error) {
	if m.ExecInPodWithStdinFunc != nil {
		return m.ExecInPodWithStdinFunc(ctx, namespace, podName, container, command, stdin)
	}
	// Default: ignore stdin, delegate to ExecInPodFunc so existing tests keep working.
	if m.ExecInPodFunc != nil {
		return m.ExecInPodFunc(ctx, namespace, podName, container, command)
	}
	return nil, nil, nil
}

// MockPodInspector is a test double for the PodInspector interface.
type MockPodInspector struct {
	GetPodContainerEnvsFunc  func(namespace, podName string) (map[string][]EnvVar, error)
	GetPodEnvFromSecretsFunc func(namespace, podName string) (map[string][]EnvFromSource, error)
	GetSecretDataFunc        func(ctx context.Context, namespace, secretName string) (map[string]string, error)
}

func (m *MockPodInspector) GetPodContainerEnvs(namespace, podName string) (map[string][]EnvVar, error) {
	if m.GetPodContainerEnvsFunc != nil {
		return m.GetPodContainerEnvsFunc(namespace, podName)
	}
	return nil, nil
}

func (m *MockPodInspector) GetPodEnvFromSecrets(namespace, podName string) (map[string][]EnvFromSource, error) {
	if m.GetPodEnvFromSecretsFunc != nil {
		return m.GetPodEnvFromSecretsFunc(namespace, podName)
	}
	return nil, nil
}

func (m *MockPodInspector) GetSecretData(ctx context.Context, namespace, secretName string) (map[string]string, error) {
	if m.GetSecretDataFunc != nil {
		return m.GetSecretDataFunc(ctx, namespace, secretName)
	}
	return nil, nil
}

// MockResourceLister is a test double for the ResourceLister interface.
type MockResourceLister struct {
	ListNamespacesFunc       func() ([]Namespace, error)
	ListDeploymentsFunc      func(ns string) ([]Deployment, error)
	ListPodsFunc             func(ns string) ([]Pod, error)
	ListPodsBySelectorFunc   func(ns string, selector map[string]string) ([]Pod, error)
	FindDeploymentByNameFunc func(ns, name string) (*Deployment, error)
	SearchResourcesFunc      func(ns, query string) ([]SearchResult, error)

	// Phase 2
	ListStatefulSetsFunc func(ns string) ([]StatefulSet, error)
	ListDaemonSetsFunc   func(ns string) ([]DaemonSet, error)
	ListJobsFunc         func(ns string) ([]Job, error)
	ListCronJobsFunc     func(ns string) ([]CronJob, error)
}

func (m *MockResourceLister) ListNamespaces() ([]Namespace, error) {
	if m.ListNamespacesFunc != nil {
		return m.ListNamespacesFunc()
	}
	return nil, nil
}

func (m *MockResourceLister) ListDeployments(ns string) ([]Deployment, error) {
	if m.ListDeploymentsFunc != nil {
		return m.ListDeploymentsFunc(ns)
	}
	return nil, nil
}

func (m *MockResourceLister) ListPods(ns string) ([]Pod, error) {
	if m.ListPodsFunc != nil {
		return m.ListPodsFunc(ns)
	}
	return nil, nil
}

func (m *MockResourceLister) ListPodsBySelector(ns string, selector map[string]string) ([]Pod, error) {
	if m.ListPodsBySelectorFunc != nil {
		return m.ListPodsBySelectorFunc(ns, selector)
	}
	return nil, nil
}

func (m *MockResourceLister) FindDeploymentByName(ns, name string) (*Deployment, error) {
	if m.FindDeploymentByNameFunc != nil {
		return m.FindDeploymentByNameFunc(ns, name)
	}
	return nil, nil
}

func (m *MockResourceLister) SearchResources(ns, query string) ([]SearchResult, error) {
	if m.SearchResourcesFunc != nil {
		return m.SearchResourcesFunc(ns, query)
	}
	return nil, nil
}

func (m *MockResourceLister) ListStatefulSets(ns string) ([]StatefulSet, error) {
	if m.ListStatefulSetsFunc != nil {
		return m.ListStatefulSetsFunc(ns)
	}
	return nil, nil
}

func (m *MockResourceLister) ListDaemonSets(ns string) ([]DaemonSet, error) {
	if m.ListDaemonSetsFunc != nil {
		return m.ListDaemonSetsFunc(ns)
	}
	return nil, nil
}

func (m *MockResourceLister) ListJobs(ns string) ([]Job, error) {
	if m.ListJobsFunc != nil {
		return m.ListJobsFunc(ns)
	}
	return nil, nil
}

func (m *MockResourceLister) ListCronJobs(ns string) ([]CronJob, error) {
	if m.ListCronJobsFunc != nil {
		return m.ListCronJobsFunc(ns)
	}
	return nil, nil
}

// MockLogStreamer is a test double for the LogStreamer interface.
type MockLogStreamer struct {
	StreamLogsFunc         func(ctx context.Context, req LogRequest, ch chan<- LogLine) error
	GetLogsFunc            func(ctx context.Context, req LogRequest) ([]LogLine, error)
	StreamMultiPodLogsFunc func(ctx context.Context, ns string, selector map[string]string, opts LogRequest, ch chan<- LogLine) error
}

func (m *MockLogStreamer) StreamLogs(ctx context.Context, req LogRequest, ch chan<- LogLine) error {
	if m.StreamLogsFunc != nil {
		return m.StreamLogsFunc(ctx, req, ch)
	}
	return nil
}

func (m *MockLogStreamer) GetLogs(ctx context.Context, req LogRequest) ([]LogLine, error) {
	if m.GetLogsFunc != nil {
		return m.GetLogsFunc(ctx, req)
	}
	return nil, nil
}

func (m *MockLogStreamer) StreamMultiPodLogs(ctx context.Context, ns string, selector map[string]string, opts LogRequest, ch chan<- LogLine) error {
	if m.StreamMultiPodLogsFunc != nil {
		return m.StreamMultiPodLogsFunc(ctx, ns, selector, opts, ch)
	}
	return nil
}

// MockEventLister is a test double for the EventLister interface.
type MockEventLister struct {
	ListEventsFunc            func(ns string) ([]Event, error)
	ListEventsForResourceFunc func(ns, kind, name string) ([]Event, error)
}

func (m *MockEventLister) ListEvents(ns string) ([]Event, error) {
	if m.ListEventsFunc != nil {
		return m.ListEventsFunc(ns)
	}
	return nil, nil
}

func (m *MockEventLister) ListEventsForResource(ns, kind, name string) ([]Event, error) {
	if m.ListEventsForResourceFunc != nil {
		return m.ListEventsForResourceFunc(ns, kind, name)
	}
	return nil, nil
}

// MockResourceDescriber is a test double for the ResourceDescriber interface.
type MockResourceDescriber struct {
	DescribeFunc func(ns, kind, name string) (string, error)
}

func (m *MockResourceDescriber) Describe(ns, kind, name string) (string, error) {
	if m.DescribeFunc != nil {
		return m.DescribeFunc(ns, kind, name)
	}
	return "", nil
}

// MockClient is a test double for the Client interface.
type MockClient struct {
	ConnectFunc        func(kubeconfig, context string) error
	DisconnectFunc     func()
	ReconnectFunc      func(kubeconfig, context string) error
	IsConnectedFunc    func() bool
	CurrentContextFunc func() string
	ContextInfoFunc    func() (string, string)

	MockResources *MockResourceLister
	MockLogs      *MockLogStreamer
	MockEvents    *MockEventLister
	MockDescriber *MockResourceDescriber
}

func (m *MockClient) Connect(kubeconfig, ctx string) error {
	if m.ConnectFunc != nil {
		return m.ConnectFunc(kubeconfig, ctx)
	}
	return nil
}

func (m *MockClient) Disconnect() {
	if m.DisconnectFunc != nil {
		m.DisconnectFunc()
	}
}

func (m *MockClient) Reconnect(kubeconfig, ctx string) error {
	if m.ReconnectFunc != nil {
		return m.ReconnectFunc(kubeconfig, ctx)
	}
	return nil
}

func (m *MockClient) IsConnected() bool {
	if m.IsConnectedFunc != nil {
		return m.IsConnectedFunc()
	}
	return true
}

func (m *MockClient) CurrentContext() string {
	if m.CurrentContextFunc != nil {
		return m.CurrentContextFunc()
	}
	return "mock-context"
}

func (m *MockClient) ContextInfo() (string, string) {
	if m.ContextInfoFunc != nil {
		return m.ContextInfoFunc()
	}
	return "", ""
}

func (m *MockClient) Resources() ResourceLister {
	if m.MockResources != nil {
		return m.MockResources
	}
	return &MockResourceLister{}
}

func (m *MockClient) Logs() LogStreamer {
	if m.MockLogs != nil {
		return m.MockLogs
	}
	return &MockLogStreamer{}
}

func (m *MockClient) Events() EventLister {
	if m.MockEvents != nil {
		return m.MockEvents
	}
	return &MockEventLister{}
}

func (m *MockClient) Describer() ResourceDescriber {
	if m.MockDescriber != nil {
		return m.MockDescriber
	}
	return &MockResourceDescriber{}
}
