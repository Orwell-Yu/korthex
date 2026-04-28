package k8s

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// newFakeResources sets up a resourceLister backed by fake clientset for testing.
func newFakeResources(objects ...runtime.Object) (*resourceLister, *fake.Clientset) {
	cs := fake.NewSimpleClientset(objects...)
	im := NewInformerManager(cs, 10)
	return &resourceLister{clientset: cs, informers: im}, cs
}

func int32Ptr(i int32) *int32 { return &i }

func TestListNamespaces(t *testing.T) {
	rl, _ := newFakeResources(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "production"}},
	)

	nsList, err := rl.ListNamespaces()
	require.NoError(t, err)
	assert.Len(t, nsList, 3)

	names := make(map[string]bool)
	for _, ns := range nsList {
		names[ns.Name] = true
	}
	assert.True(t, names["default"])
	assert.True(t, names["kube-system"])
	assert.True(t, names["production"])
}

func TestListDeployments(t *testing.T) {
	dep1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "order-service", Namespace: "production", Labels: map[string]string{"app": "order"}},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(3),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "order"}},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 3, AvailableReplicas: 3},
	}
	dep2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-service", Namespace: "production", Labels: map[string]string{"app": "payment"}},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "payment"}},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 2, AvailableReplicas: 2},
	}

	rl, _ := newFakeResources(dep1, dep2)
	deps, err := rl.ListDeployments("production")
	require.NoError(t, err)
	assert.Len(t, deps, 2)

	// Verify type conversion.
	for _, d := range deps {
		assert.NotEmpty(t, d.Name)
		assert.Equal(t, "production", d.Namespace)
		assert.NotNil(t, d.Selector)
	}
}

func TestListPods(t *testing.T) {
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "order-service-abc12", Namespace: "production",
			Labels:            map[string]string{"app": "order"},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "order", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
		},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "order-service-def34", Namespace: "production",
			Labels:            map[string]string{"app": "order"},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-30 * time.Minute)),
		},
		Spec: corev1.PodSpec{NodeName: "node-2"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "order", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
		},
	}

	rl, _ := newFakeResources(pod1, pod2)
	pods, err := rl.ListPods("production")
	require.NoError(t, err)
	assert.Len(t, pods, 2)

	for _, p := range pods {
		assert.Equal(t, "Running", p.Status)
		assert.Equal(t, "production", p.Namespace)
		assert.Len(t, p.Containers, 1)
		assert.Equal(t, "running", p.Containers[0].State)
	}
}

func TestListPodsBySelector(t *testing.T) {
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "order-abc", Namespace: "production",
			Labels: map[string]string{"app": "order", "tier": "backend"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "payment-abc", Namespace: "production",
			Labels: map[string]string{"app": "payment", "tier": "backend"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	pod3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "frontend-abc", Namespace: "production",
			Labels: map[string]string{"app": "frontend", "tier": "frontend"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	rl, _ := newFakeResources(pod1, pod2, pod3)

	// Select backend tier only.
	pods, err := rl.ListPodsBySelector("production", map[string]string{"tier": "backend"})
	require.NoError(t, err)
	assert.Len(t, pods, 2)

	// Select specific app.
	pods, err = rl.ListPodsBySelector("production", map[string]string{"app": "order"})
	require.NoError(t, err)
	assert.Len(t, pods, 1)
	assert.Equal(t, "order-abc", pods[0].Name)
}

func TestFindDeploymentByName(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "order-service", Namespace: "production"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(3),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "order"}},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 3},
	}

	rl, _ := newFakeResources(dep)

	// Found.
	found, err := rl.FindDeploymentByName("production", "order-service")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "order-service", found.Name)
	assert.Equal(t, int32(3), found.Replicas)

	// Not found.
	notFound, err := rl.FindDeploymentByName("production", "nonexistent")
	assert.Error(t, err)
	assert.Nil(t, notFound)
}

func TestSearchResources(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "order-service", Namespace: "production"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "order"}},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "order-service-abc12", Namespace: "production",
			Labels: map[string]string{"app": "order"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	otherPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "payment-xyz", Namespace: "production",
			Labels: map[string]string{"app": "payment"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	rl, _ := newFakeResources(dep, pod, otherPod)

	// Fuzzy match: "order-svc" should match "order-service" and "order-service-abc12".
	results, err := rl.SearchResources("production", "order-svc")
	require.NoError(t, err)
	assert.NotEmpty(t, results)

	foundNames := make(map[string]bool)
	for _, r := range results {
		foundNames[r.Name] = true
		assert.Equal(t, "production", r.Namespace)
	}
	assert.True(t, foundNames["order-service"] || foundNames["order-service-abc12"],
		"expected at least one order-related resource in results: %v", results)

	// Empty query returns nil.
	empty, err := rl.SearchResources("production", "")
	require.NoError(t, err)
	assert.Nil(t, empty)
}

func TestInformerLRU(t *testing.T) {
	cs := fake.NewSimpleClientset()
	im := NewInformerManager(cs, 3) // capacity 3 for easier testing
	defer im.StopAll()

	// Access 3 namespaces — all fit.
	im.Access("ns-1")
	im.Access("ns-2")
	im.Access("ns-3")
	assert.Equal(t, 3, im.Len())

	// Access a 4th — should evict ns-1 (oldest).
	im.Access("ns-4")
	assert.Equal(t, 3, im.Len())

	// ns-1 was evicted; accessing it creates a new entry (evicts ns-2).
	im.Access("ns-1")
	assert.Equal(t, 3, im.Len())
}

func TestInformerGracePeriod(t *testing.T) {
	cs := fake.NewSimpleClientset()
	im := NewInformerManager(cs, 10)
	im.grace = 100 * time.Millisecond // short grace for testing
	defer im.StopAll()

	// Access then release a namespace.
	im.Access("ns-grace")
	assert.Equal(t, 1, im.Len())

	im.Release("ns-grace")
	assert.Equal(t, 1, im.Len()) // still present during grace

	// Re-access before grace expires → no rebuild.
	f := im.Access("ns-grace")
	assert.NotNil(t, f)
	assert.Equal(t, 1, im.Len())

	// Release and wait for grace to expire.
	im.Release("ns-grace")
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 0, im.Len())
}

func TestListEventsForResource(t *testing.T) {
	ev := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-event-1", Namespace: "production"},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod", Name: "order-abc", Namespace: "production",
		},
		Type:    "Warning",
		Reason:  "BackOff",
		Message: "Back-off restarting failed container",
		Count:   3,
	}

	cs := fake.NewSimpleClientset(ev)
	el := &eventLister{clientset: cs}

	events, err := el.ListEvents("production")
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "Warning", events[0].Type)
	assert.Equal(t, "Pod/order-abc", events[0].Object)
}

func TestContextInfo_BeforeConnect(t *testing.T) {
	c := NewClient()
	kp, ctx := c.ContextInfo()
	if kp != "" || ctx != "" {
		t.Errorf("expected empty before connect, got kp=%q ctx=%q", kp, ctx)
	}
}

func TestContextInfo_DirectAccess(t *testing.T) {
	c := &k8sClient{
		kubeconfigPath: "/test/kubeconfig",
		context:        "test-ctx",
		connected:      true,
	}
	kp, ctx := c.ContextInfo()
	if kp != "/test/kubeconfig" {
		t.Errorf("expected /test/kubeconfig, got %s", kp)
	}
	if ctx != "test-ctx" {
		t.Errorf("expected test-ctx, got %s", ctx)
	}
}

func TestDescribePod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "order-abc", Namespace: "production",
			Labels: map[string]string{"app": "order"},
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.5",
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "order", Ready: true, RestartCount: 2, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
		},
	}

	cs := fake.NewSimpleClientset(pod)
	el := &eventLister{clientset: cs}
	desc := &resourceDescriber{clientset: cs, events: el}

	output, err := desc.Describe("production", "Pod", "order-abc")
	require.NoError(t, err)
	assert.Contains(t, output, "order-abc")
	assert.Contains(t, output, "Running")
	assert.Contains(t, output, "10.0.0.5")
	assert.Contains(t, output, "node-1")
}
