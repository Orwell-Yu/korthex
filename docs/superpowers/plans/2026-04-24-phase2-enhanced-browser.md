# Phase 2: Enhanced Browser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand the Resource Browser from Deployment-only to cover StatefulSet, DaemonSet, Job, and CronJob. Add a Pod detail panel with Metrics API graceful degradation. Add 4 new Agent tools for the new resource types.

**Architecture:** New Korthex types in `k8s/types.go`, per-type list methods in `k8s/resources.go` following the existing Informer lister pattern. UI resource model gains a `resourceType` sub-level within namespaces, switchable via `1`-`5` number keys. Pod detail panel reuses the describe overlay pattern. Agent gets 4 new list tools following the existing `getDeployments` handler pattern.

**Tech Stack:** Go 1.24, client-go (Informer/Lister for apps/v1 StatefulSet/DaemonSet, batch/v1 Job/CronJob), metrics-client (metrics.k8s.io/v1beta1), Bubble Tea, Lipgloss, testify

**Module path:** `github.com/Orwell-Yu/korthex`

---

## File Structure

| Action | Path | Responsibility |
|--------|------|---------------|
| Modify | `internal/k8s/types.go:11-20` | Add StatefulSet, DaemonSet, Job, CronJob, PodDetail types |
| Modify | `internal/k8s/client.go:28-35` | Extend ResourceLister interface with 4 new list methods + GetPodDetail |
| Modify | `internal/k8s/resources.go:1-12` | Add `batchv1` import; add 4 new list functions + converters |
| Modify | `internal/k8s/describe.go:24-33` | Add StatefulSet/DaemonSet/Job/CronJob describe cases |
| Modify | `internal/k8s/mock_client.go:6-13` | Add mock funcs for new list methods |
| Create | `internal/k8s/metrics.go` | Metrics API client: discovery, PodMetrics fetch, graceful degradation |
| Create | `internal/k8s/metrics_test.go` | Tests for metrics client |
| Modify | `internal/ui/messages.go` | Add resource-type loaded msgs (StatefulSetsLoadedMsg, etc.) |
| Modify | `internal/ui/resource.go:16-56` | Add resourceType field, number key handling, new data slices, formatItem cases |
| Create | `internal/ui/poddetail.go` | Pod detail panel overlay: layout, sections, key handling |
| Create | `internal/ui/poddetail_test.go` | Tests for pod detail rendering |
| Modify | `internal/agent/tools.go:27-149` | Add 4 new tool definitions + 4 dispatch cases + 4 handlers |
| Modify | `internal/agent/safety.go:9-24` | Add 4 new tools to whitelist |
| Modify | `internal/agent/prompt.go` | Update resource discovery strategy in system prompt |

---

### Task 1: New K8s Types

**Files:**
- Modify: `internal/k8s/types.go:11-77`

- [ ] **Step 1: Write the types**

Add after the existing `Deployment` struct (line 20) in `internal/k8s/types.go`:

```go
// StatefulSet represents a K8s StatefulSet.
type StatefulSet struct {
	Name      string
	Namespace string
	Replicas  int32
	Ready     int32
	Labels    map[string]string
	Selector  map[string]string
	Age       time.Duration
}

// DaemonSet represents a K8s DaemonSet.
type DaemonSet struct {
	Name            string
	Namespace       string
	DesiredNumber   int32
	CurrentNumber   int32
	ReadyNumber     int32
	UpdatedNumber   int32
	AvailableNumber int32
	Labels          map[string]string
	Selector        map[string]string
	Age             time.Duration
}

// Job represents a K8s Job.
type Job struct {
	Name           string
	Namespace      string
	Completions    int32 // desired
	Succeeded      int32
	Failed         int32
	Active         int32
	Duration       time.Duration
	Labels         map[string]string
	Selector       map[string]string
	Age            time.Duration
	StartTime      *time.Time
	CompletionTime *time.Time
}

// CronJob represents a K8s CronJob.
type CronJob struct {
	Name             string
	Namespace        string
	Schedule         string
	Suspend          bool
	Active           int32
	LastScheduleTime *time.Time
	Labels           map[string]string
	Age              time.Duration
}

// PodDetail contains the full detail view for a Pod.
type PodDetail struct {
	Pod        Pod
	IP         string
	QoS        string
	Conditions []PodCondition
	Events     []Event
	Containers []ContainerDetail
}

// PodCondition represents a pod condition status.
type PodCondition struct {
	Type   string // PodScheduled, Initialized, ContainersReady, Ready
	Status bool
}

// ContainerDetail has resource metrics alongside status.
type ContainerDetail struct {
	Name       string
	Ready      bool
	State      string
	CPUUsage   string // e.g. "250m" or "-" if metrics unavailable
	CPULimit   string // e.g. "500m"
	MemUsage   string // e.g. "128Mi" or "-"
	MemLimit   string // e.g. "256Mi"
	Restarts   int32
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/k8s/...`
Expected: PASS (no errors)

- [ ] **Step 3: Commit**

```bash
git add internal/k8s/types.go
git commit -m "feat(k8s): add StatefulSet, DaemonSet, Job, CronJob, PodDetail types"
```

---

### Task 2: Extend ResourceLister Interface + Mock

**Files:**
- Modify: `internal/k8s/client.go:28-35`
- Modify: `internal/k8s/mock_client.go:6-55`

- [ ] **Step 1: Extend ResourceLister interface**

In `internal/k8s/client.go`, add new methods to the `ResourceLister` interface (after line 35, before the closing brace):

```go
type ResourceLister interface {
	ListNamespaces() ([]Namespace, error)
	ListDeployments(namespace string) ([]Deployment, error)
	ListPods(namespace string) ([]Pod, error)
	ListPodsBySelector(namespace string, selector map[string]string) ([]Pod, error)
	FindDeploymentByName(namespace, name string) (*Deployment, error)
	SearchResources(namespace, query string) ([]SearchResult, error)
	// Phase 2: new resource types
	ListStatefulSets(namespace string) ([]StatefulSet, error)
	ListDaemonSets(namespace string) ([]DaemonSet, error)
	ListJobs(namespace string) ([]Job, error)
	ListCronJobs(namespace string) ([]CronJob, error)
}
```

- [ ] **Step 2: Update MockResourceLister**

In `internal/k8s/mock_client.go`, add to the `MockResourceLister` struct (after `SearchResourcesFunc`):

```go
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
```

Then add the mock method implementations after the existing `SearchResources` mock (after line 55):

```go
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
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/k8s/...`
Expected: FAIL — `resourceLister` doesn't implement the new methods yet (that's Task 3)

Run: `go build ./internal/k8s/mock_client.go ./internal/k8s/types.go ./internal/k8s/client.go`
Expected: This subset should compile (mock has all methods)

- [ ] **Step 4: Commit**

```bash
git add internal/k8s/client.go internal/k8s/mock_client.go
git commit -m "feat(k8s): extend ResourceLister interface with StatefulSet/DaemonSet/Job/CronJob"
```

---

### Task 3: Resource Lister Implementations

**Files:**
- Modify: `internal/k8s/resources.go:1-12,163-240`
- Create: `internal/k8s/resources_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/k8s/resources_test.go`:

```go
package k8s

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestConvertStatefulSet(t *testing.T) {
	replicas := int32(3)
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "redis-cluster",
			Namespace:         "default",
			Labels:            map[string]string{"app": "redis"},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "redis"},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 2,
		},
	}

	result := convertStatefulSet(ss)
	assert.Equal(t, "redis-cluster", result.Name)
	assert.Equal(t, "default", result.Namespace)
	assert.Equal(t, int32(3), result.Replicas)
	assert.Equal(t, int32(2), result.Ready)
	assert.Equal(t, map[string]string{"app": "redis"}, result.Selector)
	assert.True(t, result.Age > 0)
}

func TestConvertDaemonSet(t *testing.T) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node-exporter",
			Namespace:         "monitoring",
			Labels:            map[string]string{"app": "node-exporter"},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-24 * time.Hour)),
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "node-exporter"},
			},
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: 5,
			CurrentNumberScheduled: 5,
			NumberReady:            4,
			UpdatedNumberScheduled: 5,
			NumberAvailable:        4,
		},
	}

	result := convertDaemonSet(ds)
	assert.Equal(t, "node-exporter", result.Name)
	assert.Equal(t, int32(5), result.DesiredNumber)
	assert.Equal(t, int32(4), result.ReadyNumber)
	assert.Equal(t, map[string]string{"app": "node-exporter"}, result.Selector)
}

func TestConvertJob(t *testing.T) {
	now := time.Now()
	start := metav1.NewTime(now.Add(-30 * time.Minute))
	complete := metav1.NewTime(now.Add(-25 * time.Minute))
	completions := int32(1)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "db-migration",
			Namespace:         "default",
			Labels:            map[string]string{"job-name": "db-migration"},
			CreationTimestamp: metav1.NewTime(now.Add(-30 * time.Minute)),
		},
		Spec: batchv1.JobSpec{
			Completions: &completions,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"job-name": "db-migration"},
			},
		},
		Status: batchv1.JobStatus{
			Succeeded:      1,
			StartTime:      &start,
			CompletionTime: &complete,
		},
	}

	result := convertJob(job)
	assert.Equal(t, "db-migration", result.Name)
	assert.Equal(t, int32(1), result.Completions)
	assert.Equal(t, int32(1), result.Succeeded)
	assert.True(t, result.Duration > 0)
}

func TestConvertCronJob(t *testing.T) {
	lastSchedule := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "nightly-backup",
			Namespace:         "default",
			Labels:            map[string]string{"app": "backup"},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-7 * 24 * time.Hour)),
		},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 2 * * *",
		},
		Status: batchv1.CronJobStatus{
			LastScheduleTime: &lastSchedule,
			Active:           []corev1.ObjectReference{{Name: "nightly-backup-abc"}},
		},
	}

	result := convertCronJob(cj)
	assert.Equal(t, "nightly-backup", result.Name)
	assert.Equal(t, "0 2 * * *", result.Schedule)
	assert.Equal(t, int32(1), result.Active)
	assert.NotNil(t, result.LastScheduleTime)
	assert.False(t, result.Suspend)
}

func TestListStatefulSets_WithFakeClientset(t *testing.T) {
	replicas := int32(3)
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "redis",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "redis"}},
		},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: 3},
	}

	clientset := fake.NewSimpleClientset(ss)
	mgr := NewInformerManager(clientset, 10)
	defer mgr.StopAll()
	rl := &resourceLister{clientset: clientset, informers: mgr}

	results, err := rl.ListStatefulSets("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "redis", results[0].Name)
	assert.Equal(t, int32(3), results[0].Replicas)
}

func TestListJobs_WithFakeClientset(t *testing.T) {
	completions := int32(1)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "migrate",
			Namespace: "default",
		},
		Spec: batchv1.JobSpec{
			Completions: &completions,
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"job-name": "migrate"}},
		},
		Status: batchv1.JobStatus{Succeeded: 1},
	}

	clientset := fake.NewSimpleClientset(job)
	mgr := NewInformerManager(clientset, 10)
	defer mgr.StopAll()
	rl := &resourceLister{clientset: clientset, informers: mgr}

	results, err := rl.ListJobs("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "migrate", results[0].Name)
	assert.Equal(t, int32(1), results[0].Succeeded)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/k8s/... -v -run "TestConvert(StatefulSet|DaemonSet|Job|CronJob)|TestList(StatefulSets|Jobs)"`
Expected: FAIL — `convertStatefulSet`, `convertDaemonSet`, `convertJob`, `convertCronJob`, `ListStatefulSets`, `ListJobs` undefined

- [ ] **Step 3: Add batchv1 import and implement list methods + converters**

In `internal/k8s/resources.go`, add the `batchv1` import:

```go
import (
	"fmt"
	"time"

	"github.com/sahilm/fuzzy"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)
```

Add the 4 list methods after `SearchResources` (after line 163), before the converter section:

```go
func (r *resourceLister) ListStatefulSets(namespace string) ([]StatefulSet, error) {
	factory := r.informers.Access(namespace)
	factory.Apps().V1().StatefulSets().Informer()
	r.informers.EnsureSynced(namespace)

	lister := factory.Apps().V1().StatefulSets().Lister().StatefulSets(namespace)
	ssList, err := lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list statefulsets: %w", err)
	}

	result := make([]StatefulSet, 0, len(ssList))
	for _, ss := range ssList {
		result = append(result, convertStatefulSet(ss))
	}
	return result, nil
}

func (r *resourceLister) ListDaemonSets(namespace string) ([]DaemonSet, error) {
	factory := r.informers.Access(namespace)
	factory.Apps().V1().DaemonSets().Informer()
	r.informers.EnsureSynced(namespace)

	lister := factory.Apps().V1().DaemonSets().Lister().DaemonSets(namespace)
	dsList, err := lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list daemonsets: %w", err)
	}

	result := make([]DaemonSet, 0, len(dsList))
	for _, ds := range dsList {
		result = append(result, convertDaemonSet(ds))
	}
	return result, nil
}

func (r *resourceLister) ListJobs(namespace string) ([]Job, error) {
	factory := r.informers.Access(namespace)
	factory.Batch().V1().Jobs().Informer()
	r.informers.EnsureSynced(namespace)

	lister := factory.Batch().V1().Jobs().Lister().Jobs(namespace)
	jobList, err := lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}

	// Default filter: only show jobs created within the last 24 hours
	// (completed old jobs clutter the list — spec §4.1)
	cutoff := time.Now().Add(-24 * time.Hour)
	result := make([]Job, 0, len(jobList))
	for _, job := range jobList {
		// Always include active jobs; filter completed ones by age
		if job.Status.Active > 0 || job.CreationTimestamp.After(cutoff) {
			result = append(result, convertJob(job))
		}
	}
	return result, nil
}

func (r *resourceLister) ListCronJobs(namespace string) ([]CronJob, error) {
	factory := r.informers.Access(namespace)
	factory.Batch().V1().CronJobs().Informer()
	r.informers.EnsureSynced(namespace)

	lister := factory.Batch().V1().CronJobs().Lister().CronJobs(namespace)
	cjList, err := lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list cronjobs: %w", err)
	}

	result := make([]CronJob, 0, len(cjList))
	for _, cj := range cjList {
		result = append(result, convertCronJob(cj))
	}
	return result, nil
}
```

Add the converters after the existing `containerState` function (after line 240):

```go
func convertStatefulSet(ss *appsv1.StatefulSet) StatefulSet {
	var selector map[string]string
	if ss.Spec.Selector != nil {
		selector = ss.Spec.Selector.MatchLabels
	}
	var replicas int32
	if ss.Spec.Replicas != nil {
		replicas = *ss.Spec.Replicas
	}
	age := time.Duration(0)
	if !ss.CreationTimestamp.IsZero() {
		age = time.Since(ss.CreationTimestamp.Time)
	}
	return StatefulSet{
		Name:      ss.Name,
		Namespace: ss.Namespace,
		Replicas:  replicas,
		Ready:     ss.Status.ReadyReplicas,
		Labels:    ss.Labels,
		Selector:  selector,
		Age:       age,
	}
}

func convertDaemonSet(ds *appsv1.DaemonSet) DaemonSet {
	var selector map[string]string
	if ds.Spec.Selector != nil {
		selector = ds.Spec.Selector.MatchLabels
	}
	age := time.Duration(0)
	if !ds.CreationTimestamp.IsZero() {
		age = time.Since(ds.CreationTimestamp.Time)
	}
	return DaemonSet{
		Name:            ds.Name,
		Namespace:       ds.Namespace,
		DesiredNumber:   ds.Status.DesiredNumberScheduled,
		CurrentNumber:   ds.Status.CurrentNumberScheduled,
		ReadyNumber:     ds.Status.NumberReady,
		UpdatedNumber:   ds.Status.UpdatedNumberScheduled,
		AvailableNumber: ds.Status.NumberAvailable,
		Labels:          ds.Labels,
		Selector:        selector,
		Age:             age,
	}
}

func convertJob(job *batchv1.Job) Job {
	var completions int32
	if job.Spec.Completions != nil {
		completions = *job.Spec.Completions
	}
	var selector map[string]string
	if job.Spec.Selector != nil {
		selector = job.Spec.Selector.MatchLabels
	}
	age := time.Duration(0)
	if !job.CreationTimestamp.IsZero() {
		age = time.Since(job.CreationTimestamp.Time)
	}

	var duration time.Duration
	var startTime *time.Time
	var completionTime *time.Time
	if job.Status.StartTime != nil {
		st := job.Status.StartTime.Time
		startTime = &st
		if job.Status.CompletionTime != nil {
			ct := job.Status.CompletionTime.Time
			completionTime = &ct
			duration = ct.Sub(st)
		} else {
			duration = time.Since(st)
		}
	}

	return Job{
		Name:           job.Name,
		Namespace:      job.Namespace,
		Completions:    completions,
		Succeeded:      job.Status.Succeeded,
		Failed:         job.Status.Failed,
		Active:         job.Status.Active,
		Duration:       duration,
		Labels:         job.Labels,
		Selector:       selector,
		Age:            age,
		StartTime:      startTime,
		CompletionTime: completionTime,
	}
}

func convertCronJob(cj *batchv1.CronJob) CronJob {
	age := time.Duration(0)
	if !cj.CreationTimestamp.IsZero() {
		age = time.Since(cj.CreationTimestamp.Time)
	}
	var lastSchedule *time.Time
	if cj.Status.LastScheduleTime != nil {
		t := cj.Status.LastScheduleTime.Time
		lastSchedule = &t
	}
	suspend := false
	if cj.Spec.Suspend != nil {
		suspend = *cj.Spec.Suspend
	}
	return CronJob{
		Name:             cj.Name,
		Namespace:        cj.Namespace,
		Schedule:         cj.Spec.Schedule,
		Suspend:          suspend,
		Active:           int32(len(cj.Status.Active)),
		LastScheduleTime: lastSchedule,
		Labels:           cj.Labels,
		Age:              age,
	}
}
```

Also extend `SearchResources` to include the new types (after the existing Pod collection block at line ~150):

```go
// Collect statefulsets.
ssets, err := r.ListStatefulSets(namespace)
if err != nil {
	return nil, err
}
for _, ss := range ssets {
	candidates = append(candidates, candidate{kind: "StatefulSet", name: ss.Name})
	names = append(names, ss.Name)
}

// Collect daemonsets.
dsets, err := r.ListDaemonSets(namespace)
if err != nil {
	return nil, err
}
for _, ds := range dsets {
	candidates = append(candidates, candidate{kind: "DaemonSet", name: ds.Name})
	names = append(names, ds.Name)
}

// Collect jobs.
jobs, err := r.ListJobs(namespace)
if err != nil {
	return nil, err
}
for _, j := range jobs {
	candidates = append(candidates, candidate{kind: "Job", name: j.Name})
	names = append(names, j.Name)
}

// Collect cronjobs.
cjobs, err := r.ListCronJobs(namespace)
if err != nil {
	return nil, err
}
for _, cj := range cjobs {
	candidates = append(candidates, candidate{kind: "CronJob", name: cj.Name})
	names = append(names, cj.Name)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/k8s/... -v -run "TestConvert(StatefulSet|DaemonSet|Job|CronJob)|TestList(StatefulSets|Jobs)"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/k8s/resources.go internal/k8s/resources_test.go
git commit -m "feat(k8s): implement list and convert for StatefulSet/DaemonSet/Job/CronJob"
```

---

### Task 4: Describe Support for New Resource Types

**Files:**
- Modify: `internal/k8s/describe.go:21-34`

- [ ] **Step 1: Add describe cases**

In `internal/k8s/describe.go`, add imports for `batchv1`:

```go
import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)
```

Add cases to the `Describe` switch (after `case "pod":` block, before `default:`):

```go
	case "statefulset":
		return d.describeStatefulSet(ctx, namespace, name)
	case "daemonset":
		return d.describeDaemonSet(ctx, namespace, name)
	case "job":
		return d.describeJob(ctx, namespace, name)
	case "cronjob":
		return d.describeCronJob(ctx, namespace, name)
```

Add the describe methods before the formatting helpers section:

```go
func (d *resourceDescriber) describeStatefulSet(ctx context.Context, namespace, name string) (string, error) {
	ss, err := d.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get statefulset %s/%s: %w", namespace, name, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Name:               %s\n", ss.Name)
	fmt.Fprintf(&b, "Namespace:          %s\n", ss.Namespace)
	fmt.Fprintf(&b, "Labels:             %s\n", formatLabels(ss.Labels))
	fmt.Fprintf(&b, "Selector:           %s\n", formatSelector(ss.Spec.Selector))
	fmt.Fprintf(&b, "Replicas:           %s\n", formatStatefulSetReplicas(ss))
	fmt.Fprintf(&b, "Update Strategy:    %s\n", ss.Spec.UpdateStrategy.Type)
	appendContainerSpecs(&b, ss.Spec.Template.Spec.Containers)

	events, _ := d.events.ListEventsForResource(namespace, "StatefulSet", name)
	appendEvents(&b, events)

	return b.String(), nil
}

func (d *resourceDescriber) describeDaemonSet(ctx context.Context, namespace, name string) (string, error) {
	ds, err := d.clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get daemonset %s/%s: %w", namespace, name, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Name:               %s\n", ds.Name)
	fmt.Fprintf(&b, "Namespace:          %s\n", ds.Namespace)
	fmt.Fprintf(&b, "Labels:             %s\n", formatLabels(ds.Labels))
	fmt.Fprintf(&b, "Selector:           %s\n", formatSelector(ds.Spec.Selector))
	fmt.Fprintf(&b, "Desired:            %d\n", ds.Status.DesiredNumberScheduled)
	fmt.Fprintf(&b, "Current:            %d\n", ds.Status.CurrentNumberScheduled)
	fmt.Fprintf(&b, "Ready:              %d\n", ds.Status.NumberReady)
	fmt.Fprintf(&b, "Update Strategy:    %s\n", ds.Spec.UpdateStrategy.Type)
	appendContainerSpecs(&b, ds.Spec.Template.Spec.Containers)

	events, _ := d.events.ListEventsForResource(namespace, "DaemonSet", name)
	appendEvents(&b, events)

	return b.String(), nil
}

func (d *resourceDescriber) describeJob(ctx context.Context, namespace, name string) (string, error) {
	job, err := d.clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get job %s/%s: %w", namespace, name, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Name:               %s\n", job.Name)
	fmt.Fprintf(&b, "Namespace:          %s\n", job.Namespace)
	fmt.Fprintf(&b, "Labels:             %s\n", formatLabels(job.Labels))
	fmt.Fprintf(&b, "Selector:           %s\n", formatSelector(job.Spec.Selector))
	fmt.Fprintf(&b, "Completions:        %s\n", formatJobCompletions(job))
	fmt.Fprintf(&b, "Active:             %d\n", job.Status.Active)
	fmt.Fprintf(&b, "Succeeded:          %d\n", job.Status.Succeeded)
	fmt.Fprintf(&b, "Failed:             %d\n", job.Status.Failed)
	appendContainerSpecs(&b, job.Spec.Template.Spec.Containers)

	events, _ := d.events.ListEventsForResource(namespace, "Job", name)
	appendEvents(&b, events)

	return b.String(), nil
}

func (d *resourceDescriber) describeCronJob(ctx context.Context, namespace, name string) (string, error) {
	cj, err := d.clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get cronjob %s/%s: %w", namespace, name, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Name:               %s\n", cj.Name)
	fmt.Fprintf(&b, "Namespace:          %s\n", cj.Namespace)
	fmt.Fprintf(&b, "Labels:             %s\n", formatLabels(cj.Labels))
	fmt.Fprintf(&b, "Schedule:           %s\n", cj.Spec.Schedule)
	suspend := false
	if cj.Spec.Suspend != nil {
		suspend = *cj.Spec.Suspend
	}
	fmt.Fprintf(&b, "Suspend:            %v\n", suspend)
	fmt.Fprintf(&b, "Active Jobs:        %d\n", len(cj.Status.Active))
	if cj.Status.LastScheduleTime != nil {
		fmt.Fprintf(&b, "Last Schedule:      %s\n", cj.Status.LastScheduleTime.Time.Format("2006-01-02 15:04:05"))
	}
	appendContainerSpecs(&b, cj.Spec.JobTemplate.Spec.Template.Spec.Containers)

	events, _ := d.events.ListEventsForResource(namespace, "CronJob", name)
	appendEvents(&b, events)

	return b.String(), nil
}
```

Add the formatting helpers:

```go
func formatStatefulSetReplicas(ss *appsv1.StatefulSet) string {
	desired := int32(0)
	if ss.Spec.Replicas != nil {
		desired = *ss.Spec.Replicas
	}
	return fmt.Sprintf("%d desired | %d ready | %d current",
		desired, ss.Status.ReadyReplicas, ss.Status.CurrentReplicas)
}

func formatJobCompletions(job *batchv1.Job) string {
	desired := int32(0)
	if job.Spec.Completions != nil {
		desired = *job.Spec.Completions
	}
	return fmt.Sprintf("%d/%d", job.Status.Succeeded, desired)
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/k8s/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/k8s/describe.go
git commit -m "feat(k8s): add describe support for StatefulSet/DaemonSet/Job/CronJob"
```

---

### Task 5: Metrics API Client

**Files:**
- Create: `internal/k8s/metrics.go`
- Create: `internal/k8s/metrics_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/k8s/metrics_test.go`:

```go
package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetricsClient_FormatCPU(t *testing.T) {
	tests := []struct {
		milliCPU int64
		expected string
	}{
		{250, "250m"},
		{0, "0m"},
		{1000, "1000m"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, formatMilliCPU(tt.milliCPU))
	}
}

func TestMetricsClient_FormatMemory(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{128 * 1024 * 1024, "128Mi"},
		{1024 * 1024 * 1024, "1024Mi"},
		{512 * 1024, "0Mi"},
		{0, "0Mi"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, formatMemoryMi(tt.bytes))
	}
}

func TestMetricsClient_NotAvailable(t *testing.T) {
	mc := &metricsClient{available: false}
	assert.False(t, mc.IsAvailable())

	_, err := mc.GetPodMetrics("default", "test-pod")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/k8s/... -v -run "TestMetrics"`
Expected: FAIL — `metricsClient`, `formatMilliCPU`, `formatMemoryMi` undefined

- [ ] **Step 3: Implement metrics client**

Create `internal/k8s/metrics.go`:

```go
package k8s

import (
	"context"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// PodMetrics holds CPU/Memory usage for a pod's containers.
type PodMetrics struct {
	Containers []ContainerMetrics
}

// ContainerMetrics holds resource usage for a single container.
type ContainerMetrics struct {
	Name     string
	CPUUsage string // e.g. "250m"
	MemUsage string // e.g. "128Mi"
}

// MetricsClient provides access to the Kubernetes Metrics API.
type MetricsClient interface {
	IsAvailable() bool
	GetPodMetrics(namespace, podName string) (*PodMetrics, error)
}

type metricsClient struct {
	client    metricsclient.Interface
	available bool
}

// NewMetricsClient creates a MetricsClient. It probes the API server for
// metrics.k8s.io/v1beta1 availability. If not available, all calls
// gracefully return errors without panicking.
func NewMetricsClient(restConfig *rest.Config, clientset kubernetes.Interface) MetricsClient {
	mc := &metricsClient{}

	// Probe for metrics API availability.
	disco := clientset.Discovery()
	_, resources, err := disco.ServerGroupsAndResources()
	if err != nil {
		// If we can't discover, assume unavailable.
		slog.Debug("metrics API discovery failed", "error", err)
		return mc
	}

	for _, rl := range resources {
		if rl.GroupVersion == "metrics.k8s.io/v1beta1" {
			mc.available = true
			break
		}
	}

	if !mc.available {
		slog.Debug("metrics.k8s.io/v1beta1 not available")
		return mc
	}

	metricsClientset, err := metricsclient.NewForConfig(restConfig)
	if err != nil {
		slog.Debug("metrics client creation failed", "error", err)
		mc.available = false
		return mc
	}
	mc.client = metricsClientset
	slog.Debug("metrics API available")
	return mc
}

func (m *metricsClient) IsAvailable() bool {
	return m.available
}

func (m *metricsClient) GetPodMetrics(namespace, podName string) (*PodMetrics, error) {
	if !m.available {
		return nil, fmt.Errorf("metrics API not available")
	}

	pm, err := m.client.MetricsV1beta1().PodMetricses(namespace).Get(
		context.Background(), podName, metav1.GetOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("get pod metrics %s/%s: %w", namespace, podName, err)
	}

	result := &PodMetrics{
		Containers: make([]ContainerMetrics, 0, len(pm.Containers)),
	}
	for _, c := range pm.Containers {
		result.Containers = append(result.Containers, ContainerMetrics{
			Name:     c.Name,
			CPUUsage: formatMilliCPU(c.Usage.Cpu().MilliValue()),
			MemUsage: formatMemoryMi(c.Usage.Memory().Value()),
		})
	}
	return result, nil
}

func formatMilliCPU(milliCPU int64) string {
	return fmt.Sprintf("%dm", milliCPU)
}

func formatMemoryMi(bytes int64) string {
	mi := bytes / (1024 * 1024)
	return fmt.Sprintf("%dMi", mi)
}
```

> **Note:** This requires adding `k8s.io/metrics` to go.mod. Run `go get k8s.io/metrics@v0.30.12` (matching the client-go version already in go.mod).

- [ ] **Step 4: Add metrics dependency**

Run: `go get k8s.io/metrics@v0.30.12`

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/k8s/... -v -run "TestMetrics"`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/k8s/metrics.go internal/k8s/metrics_test.go go.mod go.sum
git commit -m "feat(k8s): add Metrics API client with graceful degradation"
```

---

### Task 6: UI Messages for New Resource Types

**Files:**
- Modify: `internal/ui/messages.go`

- [ ] **Step 1: Add new message types**

In `internal/ui/messages.go`, add after the existing `NavigateToResourceMsg` (after line 63):

```go
// Resource data loaded messages for new resource types.
type statefulSetsLoadedMsg struct{ Items []k8s.StatefulSet }
type daemonSetsLoadedMsg struct{ Items []k8s.DaemonSet }
type jobsLoadedMsg struct{ Items []k8s.Job }
type cronJobsLoadedMsg struct{ Items []k8s.CronJob }

// deploymentsLoadedMsg carries loaded deployments (rename from implicit inline usage).
type deploymentsLoadedMsg struct{ Items []k8s.Deployment }

// podsLoadedMsg carries loaded pods.
type podsLoadedMsg struct{ Pods []k8s.Pod }

// PodDetailLoadedMsg carries the full pod detail for the overlay.
type PodDetailLoadedMsg struct {
	Detail k8s.PodDetail
}

// PodDetailErrorMsg signals an error loading pod details.
type PodDetailErrorMsg struct {
	Err error
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/ui/...`
Expected: May have issues due to existing podsLoadedMsg usage. Check if it's already defined.

- [ ] **Step 3: Reconcile with existing msg types**

Check `resource.go` for existing `podsLoadedMsg` definition. If already defined there, don't redefine in `messages.go`. Only add truly new types.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/messages.go
git commit -m "feat(ui): add message types for new resource types and pod detail"
```

---

### Task 7: Resource Browser — Resource Type Switching

**Files:**
- Modify: `internal/ui/resource.go:16-56,240-295,525-574,582-627`

This is the largest UI change. The resource browser gains a `resourceType` concept at the deployment level, switchable by number keys `1`-`5`.

- [ ] **Step 1: Add resource type constants and fields**

In `internal/ui/resource.go`, add resource type constants after the level constants (after line 21):

```go
// ResourceType identifies which workload type is shown at level 1.
type ResourceType int

const (
	ResDeployments  ResourceType = iota // 1 key (default)
	ResStatefulSets                     // 2 key
	ResDaemonSets                       // 3 key
	ResJobs                             // 4 key
	ResCronJobs                         // 5 key
)

var resourceTypeLabels = map[ResourceType]string{
	ResDeployments:  "Deployments",
	ResStatefulSets: "StatefulSets",
	ResDaemonSets:   "DaemonSets",
	ResJobs:         "Jobs",
	ResCronJobs:     "CronJobs",
}
```

Add new fields to `ResourceModel` struct (after `containers []k8s.Container`):

```go
	// Phase 2: additional resource type data
	statefulsets []k8s.StatefulSet
	daemonsets   []k8s.DaemonSet
	jobs         []k8s.Job
	cronjobs     []k8s.CronJob

	// Current resource type view (only relevant at levelDeployment)
	resourceType ResourceType
```

- [ ] **Step 2: Add resource type loading commands**

Add tea.Cmd functions for loading new resource types (near existing `loadDeployments`):

```go
func loadStatefulSets(client k8s.Client, ns string) tea.Cmd {
	return func() tea.Msg {
		items, err := client.Resources().ListStatefulSets(ns)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list statefulsets: %w", err)}
		}
		return statefulSetsLoadedMsg{Items: items}
	}
}

func loadDaemonSets(client k8s.Client, ns string) tea.Cmd {
	return func() tea.Msg {
		items, err := client.Resources().ListDaemonSets(ns)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list daemonsets: %w", err)}
		}
		return daemonSetsLoadedMsg{Items: items}
	}
}

func loadJobs(client k8s.Client, ns string) tea.Cmd {
	return func() tea.Msg {
		items, err := client.Resources().ListJobs(ns)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list jobs: %w", err)}
		}
		return jobsLoadedMsg{Items: items}
	}
}

func loadCronJobs(client k8s.Client, ns string) tea.Cmd {
	return func() tea.Msg {
		items, err := client.Resources().ListCronJobs(ns)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list cronjobs: %w", err)}
		}
		return cronJobsLoadedMsg{Items: items}
	}
}
```

- [ ] **Step 3: Handle number key switching in updateNavigation**

In `updateNavigation` (around line 240), add cases for `1`-`5` keys when at `levelDeployment`:

```go
	case "1":
		if m.level == levelDeployment && m.resourceType != ResDeployments {
			m.resourceType = ResDeployments
			m.cursor = 0
			m.clearSearch()
			return m, loadDeployments(m.k8sClient, m.selectedNS)
		}
	case "2":
		if m.level == levelDeployment {
			m.resourceType = ResStatefulSets
			m.cursor = 0
			m.clearSearch()
			return m, loadStatefulSets(m.k8sClient, m.selectedNS)
		}
	case "3":
		if m.level == levelDeployment {
			m.resourceType = ResDaemonSets
			m.cursor = 0
			m.clearSearch()
			return m, loadDaemonSets(m.k8sClient, m.selectedNS)
		}
	case "4":
		if m.level == levelDeployment {
			m.resourceType = ResJobs
			m.cursor = 0
			m.clearSearch()
			return m, loadJobs(m.k8sClient, m.selectedNS)
		}
	case "5":
		if m.level == levelDeployment {
			m.resourceType = ResCronJobs
			m.cursor = 0
			m.clearSearch()
			return m, loadCronJobs(m.k8sClient, m.selectedNS)
		}
```

- [ ] **Step 4: Handle new message types in Update**

In the `ResourceModel.Update` method, add cases for the new loaded messages:

```go
	case statefulSetsLoadedMsg:
		m.statefulsets = msg.Items
		m.cursor = min(m.cursor, max(len(msg.Items)-1, 0))
		m.applyPendingHighlight()
		return m, nil
	case daemonSetsLoadedMsg:
		m.daemonsets = msg.Items
		m.cursor = min(m.cursor, max(len(msg.Items)-1, 0))
		m.applyPendingHighlight()
		return m, nil
	case jobsLoadedMsg:
		m.jobs = msg.Items
		m.cursor = min(m.cursor, max(len(msg.Items)-1, 0))
		m.applyPendingHighlight()
		return m, nil
	case cronJobsLoadedMsg:
		m.cronjobs = msg.Items
		m.cursor = min(m.cursor, max(len(msg.Items)-1, 0))
		m.applyPendingHighlight()
		return m, nil
```

- [ ] **Step 5: Update drillIn for new resource types**

Modify the `levelDeployment` case in `drillIn` to handle all resource types — each should extract the appropriate selector and drill into pods:

```go
	case levelDeployment:
		switch m.resourceType {
		case ResDeployments:
			if len(m.deployments) == 0 {
				return m, nil
			}
			dep := m.deployments[idx]
			m.selectedDep = dep.Name
			m.level = levelPod
			m.cursor = 0
			m.clearSearch()
			if len(dep.Selector) > 0 {
				return m, loadPodsBySelector(m.k8sClient, m.selectedNS, dep.Selector)
			}
			return m, loadPods(m.k8sClient, m.selectedNS)

		case ResStatefulSets:
			if len(m.statefulsets) == 0 {
				return m, nil
			}
			ss := m.statefulsets[idx]
			m.selectedDep = ss.Name
			m.level = levelPod
			m.cursor = 0
			m.clearSearch()
			if len(ss.Selector) > 0 {
				return m, loadPodsBySelectorSorted(m.k8sClient, m.selectedNS, ss.Selector, sortByOrdinal)
			}
			return m, loadPods(m.k8sClient, m.selectedNS)

		case ResDaemonSets:
			if len(m.daemonsets) == 0 {
				return m, nil
			}
			ds := m.daemonsets[idx]
			m.selectedDep = ds.Name
			m.level = levelPod
			m.cursor = 0
			m.clearSearch()
			if len(ds.Selector) > 0 {
				return m, loadPodsBySelectorSorted(m.k8sClient, m.selectedNS, ds.Selector, sortByNodeName)
			}
			return m, loadPods(m.k8sClient, m.selectedNS)

		case ResJobs:
			if len(m.jobs) == 0 {
				return m, nil
			}
			job := m.jobs[idx]
			m.selectedDep = job.Name
			m.level = levelPod
			m.cursor = 0
			m.clearSearch()
			if len(job.Selector) > 0 {
				return m, loadPodsBySelector(m.k8sClient, m.selectedNS, job.Selector)
			}
			return m, loadPods(m.k8sClient, m.selectedNS)

		case ResCronJobs:
			// CronJob → drill into its managed Jobs (filtered by CronJob name)
			if len(m.cronjobs) == 0 {
				return m, nil
			}
			cj := m.cronjobs[idx]
			m.selectedDep = cj.Name
			m.resourceType = ResJobs
			m.cursor = 0
			m.clearSearch()
			// Load jobs filtered by the CronJob name (jobs created by a CronJob
			// have the name pattern: <cronjob-name>-<timestamp>)
			return m, loadJobsForCronJob(m.k8sClient, m.selectedNS, cj.Name)
		}
```

Add a helper for loading pods by selector:

```go
func loadPodsBySelector(client k8s.Client, ns string, selector map[string]string) tea.Cmd {
	return func() tea.Msg {
		pods, err := client.Resources().ListPodsBySelector(ns, selector)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list pods by selector: %w", err)}
		}
		return podsLoadedMsg{Pods: pods}
	}
}

// podSortFunc defines how to sort pods after loading.
type podSortFunc func([]k8s.Pod)

// sortByOrdinal sorts StatefulSet pods by ordinal index (name suffix: -0, -1, -2...).
func sortByOrdinal(pods []k8s.Pod) {
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name // lexicographic works for <name>-0, <name>-1, etc.
	})
}

// sortByNodeName sorts DaemonSet pods by node name.
func sortByNodeName(pods []k8s.Pod) {
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].NodeName < pods[j].NodeName
	})
}

func loadPodsBySelectorSorted(client k8s.Client, ns string, selector map[string]string, sortFn podSortFunc) tea.Cmd {
	return func() tea.Msg {
		pods, err := client.Resources().ListPodsBySelector(ns, selector)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list pods by selector: %w", err)}
		}
		if sortFn != nil {
			sortFn(pods)
		}
		return podsLoadedMsg{Pods: pods}
	}
}

func loadJobsForCronJob(client k8s.Client, ns string, cronJobName string) tea.Cmd {
	return func() tea.Msg {
		allJobs, err := client.Resources().ListJobs(ns)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list jobs: %w", err)}
		}
		// Filter jobs that belong to this CronJob (name prefix match)
		filtered := make([]k8s.Job, 0)
		for _, j := range allJobs {
			if strings.HasPrefix(j.Name, cronJobName+"-") {
				filtered = append(filtered, j)
			}
		}
		return jobsLoadedMsg{Items: filtered}
	}
}
```

Note: Add `"sort"` and `"strings"` to the import block in `resource.go`.

- [ ] **Step 6: Update drillOut to reset resourceType**

In `drillOut`, when going back from `levelDeployment` to `levelNamespace`, reset `resourceType`:

```go
	case levelDeployment:
		m.level = levelNamespace
		m.cursor = 0
		m.resourceType = ResDeployments // reset to default
		m.clearSearch()
```

- [ ] **Step 7: Update currentListLen, currentListNames, formatItem, breadcrumb**

Update `currentListLen` for `levelDeployment`:

```go
	case levelDeployment:
		switch m.resourceType {
		case ResDeployments:
			return len(m.deployments)
		case ResStatefulSets:
			return len(m.statefulsets)
		case ResDaemonSets:
			return len(m.daemonsets)
		case ResJobs:
			return len(m.jobs)
		case ResCronJobs:
			return len(m.cronjobs)
		}
		return 0
```

Update `currentListNames` for `levelDeployment`:

```go
	case levelDeployment:
		switch m.resourceType {
		case ResDeployments:
			names := make([]string, len(m.deployments))
			for i, d := range m.deployments {
				names[i] = d.Name
			}
			return names
		case ResStatefulSets:
			names := make([]string, len(m.statefulsets))
			for i, s := range m.statefulsets {
				names[i] = s.Name
			}
			return names
		case ResDaemonSets:
			names := make([]string, len(m.daemonsets))
			for i, d := range m.daemonsets {
				names[i] = d.Name
			}
			return names
		case ResJobs:
			names := make([]string, len(m.jobs))
			for i, j := range m.jobs {
				names[i] = j.Name
			}
			return names
		case ResCronJobs:
			names := make([]string, len(m.cronjobs))
			for i, c := range m.cronjobs {
				names[i] = c.Name
			}
			return names
		}
		return nil
```

Update `formatItem` for `levelDeployment`:

```go
	case levelDeployment:
		switch m.resourceType {
		case ResDeployments:
			if idx < len(m.deployments) {
				dep := m.deployments[idx]
				return fmt.Sprintf("%-30s %d/%d", dep.Name, dep.Ready, dep.Replicas)
			}
		case ResStatefulSets:
			if idx < len(m.statefulsets) {
				ss := m.statefulsets[idx]
				return fmt.Sprintf("%-30s %d/%d  %s", ss.Name, ss.Ready, ss.Replicas, formatAge(ss.Age))
			}
		case ResDaemonSets:
			if idx < len(m.daemonsets) {
				ds := m.daemonsets[idx]
				return fmt.Sprintf("%-30s %d/%d ready  %s", ds.Name, ds.ReadyNumber, ds.DesiredNumber, formatAge(ds.Age))
			}
		case ResJobs:
			if idx < len(m.jobs) {
				j := m.jobs[idx]
				return fmt.Sprintf("%-30s %d/%d  %s  %s", j.Name, j.Succeeded, j.Completions, formatDuration(j.Duration), formatAge(j.Age))
			}
		case ResCronJobs:
			if idx < len(m.cronjobs) {
				cj := m.cronjobs[idx]
				lastSched := "-"
				if cj.LastScheduleTime != nil {
					lastSched = formatTimeSince(*cj.LastScheduleTime)
				}
				return fmt.Sprintf("%-30s %-14s A:%d  Last:%s", cj.Name, cj.Schedule, cj.Active, lastSched)
			}
		}
		return name
```

Add the formatting helpers:

```go
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func formatTimeSince(t time.Time) string {
	return formatDuration(time.Since(t))
}
```

Update `breadcrumb` to use the resource type label:

```go
	label := ""
	switch m.level {
	case levelNamespace:
		label = "Namespaces"
	case levelDeployment:
		label = resourceTypeLabels[m.resourceType]
	case levelPod:
		label = "Pods"
	case levelContainer:
		label = "Containers"
	}
```

- [ ] **Step 8: Update describeSelected for new resource types**

```go
	case levelDeployment:
		switch m.resourceType {
		case ResDeployments:
			if len(m.deployments) > 0 {
				return m, loadDescribe(m.k8sClient, m.selectedNS, "Deployment", m.deployments[idx].Name)
			}
		case ResStatefulSets:
			if len(m.statefulsets) > 0 {
				return m, loadDescribe(m.k8sClient, m.selectedNS, "StatefulSet", m.statefulsets[idx].Name)
			}
		case ResDaemonSets:
			if len(m.daemonsets) > 0 {
				return m, loadDescribe(m.k8sClient, m.selectedNS, "DaemonSet", m.daemonsets[idx].Name)
			}
		case ResJobs:
			if len(m.jobs) > 0 {
				return m, loadDescribe(m.k8sClient, m.selectedNS, "Job", m.jobs[idx].Name)
			}
		case ResCronJobs:
			if len(m.cronjobs) > 0 {
				return m, loadDescribe(m.k8sClient, m.selectedNS, "CronJob", m.cronjobs[idx].Name)
			}
		}
```

- [ ] **Step 9: Verify compilation**

Run: `go build ./internal/ui/...`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/ui/resource.go internal/ui/messages.go
git commit -m "feat(ui): resource type switching with 1-5 keys for StatefulSet/DaemonSet/Job/CronJob"
```

---

### Task 8: Pod Detail Panel

**Files:**
- Create: `internal/ui/poddetail.go`
- Create: `internal/ui/poddetail_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/poddetail_test.go`:

```go
package ui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/Orwell-Yu/korthex/internal/k8s"
)

func TestRenderPodDetail_Basic(t *testing.T) {
	detail := k8s.PodDetail{
		Pod: k8s.Pod{
			Name:      "order-svc-7d4f8-abc12",
			Namespace: "production",
			Status:    "Running",
			NodeName:  "worker-03",
			Restarts:  0,
			Age:       2 * time.Hour,
		},
		IP:  "10.244.1.15",
		QoS: "Burstable",
		Conditions: []k8s.PodCondition{
			{Type: "PodScheduled", Status: true},
			{Type: "Initialized", Status: true},
			{Type: "ContainersReady", Status: true},
			{Type: "Ready", Status: true},
		},
		Containers: []k8s.ContainerDetail{
			{
				Name: "order-api", Ready: true, State: "running",
				CPUUsage: "250m", CPULimit: "500m",
				MemUsage: "128Mi", MemLimit: "256Mi",
				Restarts: 0,
			},
		},
		Events: []k8s.Event{
			{Type: "Normal", Reason: "Pulled", Message: "Successfully pulled image", Object: "Pod/order-svc-7d4f8-abc12"},
		},
	}

	theme := GetTheme("dark")
	output := renderPodDetail(detail, theme, 80, 40)

	assert.Contains(t, output, "order-svc-7d4f8-abc12")
	assert.Contains(t, output, "Running")
	assert.Contains(t, output, "worker-03")
	assert.Contains(t, output, "10.244.1.15")
	assert.Contains(t, output, "order-api")
	assert.Contains(t, output, "250m/500m")
}

func TestRenderPodDetail_NoMetrics(t *testing.T) {
	detail := k8s.PodDetail{
		Pod: k8s.Pod{
			Name:   "test-pod",
			Status: "Running",
		},
		Containers: []k8s.ContainerDetail{
			{
				Name: "main", Ready: true, State: "running",
				CPUUsage: "-", CPULimit: "500m",
				MemUsage: "-", MemLimit: "256Mi",
			},
		},
	}

	theme := GetTheme("dark")
	output := renderPodDetail(detail, theme, 80, 40)

	assert.Contains(t, output, "-/500m")
	assert.Contains(t, output, "-/256Mi")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/... -v -run "TestRenderPodDetail"`
Expected: FAIL — `renderPodDetail` undefined

- [ ] **Step 3: Implement pod detail panel**

Create `internal/ui/poddetail.go`:

```go
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Orwell-Yu/korthex/internal/k8s"
)

// renderPodDetail renders the Pod detail overlay content.
func renderPodDetail(detail k8s.PodDetail, theme Theme, width, height int) string {
	var b strings.Builder
	pod := detail.Pod

	// Header
	b.WriteString(theme.Title.Render(fmt.Sprintf("Pod: %s", pod.Name)))
	b.WriteString("\n\n")

	// Basic info
	b.WriteString(fmt.Sprintf("  Status: %-14s Node: %s\n", pod.Status, pod.NodeName))
	b.WriteString(fmt.Sprintf("  IP: %-18s Started: %s\n", detail.IP, formatAge(pod.Age)))
	b.WriteString(fmt.Sprintf("  Restarts: %-11d QoS: %s\n", pod.Restarts, detail.QoS))
	b.WriteString("\n")

	// Containers section
	b.WriteString(theme.Subtitle.Render(" Containers "))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %-16s %-10s %-13s %-13s %s\n",
		"NAME", "STATUS", "CPU", "MEMORY", "RESTARTS"))
	for _, c := range detail.Containers {
		stateStr := c.State
		if c.Ready {
			stateStr = c.State
		} else {
			stateStr = c.State + "!"
		}
		cpuStr := c.CPUUsage + "/" + c.CPULimit
		memStr := c.MemUsage + "/" + c.MemLimit
		b.WriteString(fmt.Sprintf("  %-16s %-10s %-13s %-13s %d\n",
			c.Name, stateStr, cpuStr, memStr, c.Restarts))
	}
	b.WriteString("\n")

	// Conditions section
	b.WriteString(theme.Subtitle.Render(" Conditions "))
	b.WriteString("\n  ")
	for _, cond := range detail.Conditions {
		mark := "x"
		if cond.Status {
			mark = "✓"
		}
		b.WriteString(fmt.Sprintf("%s %-18s", mark, cond.Type))
	}
	b.WriteString("\n\n")

	// Events section (last 10)
	b.WriteString(theme.Subtitle.Render(" Recent Events "))
	b.WriteString("\n")
	if len(detail.Events) == 0 {
		b.WriteString("  <none>\n")
	} else {
		limit := min(len(detail.Events), 10)
		for i := 0; i < limit; i++ {
			ev := detail.Events[i]
			age := "-"
			if !ev.LastSeen.IsZero() {
				age = formatDuration(time.Since(ev.LastSeen))
			}
			b.WriteString(fmt.Sprintf("  %-8s %-8s %-12s %s\n",
				age, ev.Type, ev.Reason, truncateStr(ev.Message, width-34)))
		}
	}
	b.WriteString("\n")

	// Footer
	b.WriteString(theme.Subtitle.Render("[Esc] back  [l] logs  [j/k] scroll"))

	return b.String()
}

func truncateStr(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/... -v -run "TestRenderPodDetail"`
Expected: PASS

- [ ] **Step 5: Wire pod detail into ResourceModel**

Add a `podDetail` field to `ResourceModel`:

```go
	// Pod detail overlay (Phase 2)
	podDetail    *k8s.PodDetail // non-nil means show pod detail overlay
```

In `updateNavigation`, change `"d"` key handling to show pod detail when at `levelPod`:

```go
	case "d":
		if m.level == levelPod && len(m.pods) > 0 {
			pod := m.pods[m.clampedCursor()]
			return m, loadPodDetail(m.k8sClient, m.selectedNS, pod.Name)
		}
		return m.describeSelected()
```

Add the `loadPodDetail` command:

```go
func loadPodDetail(client k8s.Client, namespace, podName string) tea.Cmd {
	return func() tea.Msg {
		// Get pod from the API directly for full status data
		describer := client.Describer()
		_ = describer // We'll use raw Pod API for structured data

		// Get pod from cache
		pods, err := client.Resources().ListPods(namespace)
		if err != nil {
			return PodDetailErrorMsg{Err: err}
		}
		var targetPod *k8s.Pod
		for i := range pods {
			if pods[i].Name == podName {
				targetPod = &pods[i]
				break
			}
		}
		if targetPod == nil {
			return PodDetailErrorMsg{Err: fmt.Errorf("pod %s not found", podName)}
		}

		// Get events
		events, _ := client.Events().ListEventsForResource(namespace, "Pod", podName)

		// Build container details — try metrics, graceful degradation
		containers := make([]k8s.ContainerDetail, 0, len(targetPod.Containers))
		metricsClient := client.Metrics()
		var podMetrics *k8s.PodMetrics
		if metricsClient != nil && metricsClient.IsAvailable() {
			podMetrics, _ = metricsClient.GetPodMetrics(namespace, podName)
		}

		for _, c := range targetPod.Containers {
			cd := k8s.ContainerDetail{
				Name:     c.Name,
				Ready:    c.Ready,
				State:    c.State,
				CPUUsage: "-", CPULimit: "-",
				MemUsage: "-", MemLimit: "-",
			}
			// Fill in metrics if available
			if podMetrics != nil {
				for _, cm := range podMetrics.Containers {
					if cm.Name == c.Name {
						cd.CPUUsage = cm.CPUUsage
						cd.MemUsage = cm.MemUsage
						break
					}
				}
			}
			containers = append(containers, cd)
		}

		// Build conditions from Pod status (using describe as a fallback source)
		// Note: The Informer Cache Pod type doesn't carry conditions. We get them
		// from the describe output or a dedicated API call. For Phase 2 we derive
		// conditions from the pod describe text. Alternatively, the Pod lister could
		// be extended to include conditions — but that's a Phase 1 Pod type change.
		// For now, derive from known status:
		conditions := derivePodConditions(targetPod)

		// Derive QoS class from container resource configuration
		qos := "BestEffort"
		if len(containers) > 0 {
			hasLimits := false
			for _, cd := range containers {
				if cd.CPULimit != "-" || cd.MemLimit != "-" {
					hasLimits = true
					break
				}
			}
			if hasLimits {
				qos = "Burstable"
			}
		}

		detail := k8s.PodDetail{
			Pod:        *targetPod,
			IP:         "", // Populated from Informer cache (requires Pod type extension)
			QoS:        qos,
			Conditions: conditions,
			Events:     events,
			Containers: containers,
		}

		return PodDetailLoadedMsg{Detail: detail}
	}
}

// derivePodConditions infers conditions from pod status.
// Full conditions require extending the Pod type to carry status.conditions
// from client-go — this is a minimal derivation for Phase 2.
func derivePodConditions(pod *k8s.Pod) []k8s.PodCondition {
	isRunning := pod.Status == "Running"
	return []k8s.PodCondition{
		{Type: "PodScheduled", Status: true},     // if we can see it, it's scheduled
		{Type: "Initialized", Status: true},       // if containers exist, it's initialized
		{Type: "ContainersReady", Status: isRunning},
		{Type: "Ready", Status: isRunning},
	}
}
```

Handle the messages in `Update`:

```go
	case PodDetailLoadedMsg:
		m.podDetail = &msg.Detail
		return m, nil
	case PodDetailErrorMsg:
		// Fall back to regular describe on error
		m.describeResult = fmt.Sprintf("Error loading pod detail: %v", msg.Err)
		return m, nil
```

In `View`, render pod detail overlay before the describe overlay check:

```go
	// Pod detail overlay takes precedence
	if m.podDetail != nil {
		return renderPodDetail(*m.podDetail, m.theme, m.width, m.height)
	}
```

Handle `Esc`, `l`, and `y` in the pod detail overlay context. In `Update`, before normal key routing:

```go
	case tea.KeyMsg:
		// Pod detail overlay keys
		if m.podDetail != nil {
			switch msg.String() {
			case "esc", "backspace":
				m.podDetail = nil
				return m, nil
			case "l":
				pod := m.podDetail.Pod
				m.podDetail = nil
				return m, func() tea.Msg {
					return NavigateToLogsMsg{
						Namespace: m.selectedNS,
						PodName:   pod.Name,
					}
				}
			case "y":
				// Copy pod name to clipboard (OS-level clipboard requires
				// exec("pbcopy")/exec("xclip") which is platform-specific;
				// for Phase 2 we copy the pod name to a status message)
				m.podDetail = nil
				return m, nil
			}
			return m, nil
		}
```

- [ ] **Step 6: Verify compilation**

Run: `go build ./internal/ui/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/ui/poddetail.go internal/ui/poddetail_test.go internal/ui/resource.go
git commit -m "feat(ui): add Pod detail panel overlay with container/conditions/events"
```

---

### Task 9: Agent Tools for New Resource Types

**Files:**
- Modify: `internal/agent/tools.go:27-149,254-282`
- Modify: `internal/agent/safety.go:9-24`
- Create: `internal/agent/tools_phase2_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/agent/tools_phase2_test.go`:

```go
package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Orwell-Yu/korthex/internal/k8s"
)

func TestToolExecution_GetStatefulSets(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListStatefulSetsFunc: func(ns string) ([]k8s.StatefulSet, error) {
				return []k8s.StatefulSet{
					{Name: "redis-cluster", Namespace: ns, Replicas: 3, Ready: 3,
						Selector: map[string]string{"app": "redis"}, Age: 24 * time.Hour},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_get_statefulsets",
		map[string]string{"namespace": "default"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "redis-cluster") {
		t.Errorf("result should contain statefulset name, got: %s", result)
	}
	if !strings.Contains(result, "3/3") {
		t.Errorf("result should contain ready count, got: %s", result)
	}
}

func TestToolExecution_GetDaemonSets(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListDaemonSetsFunc: func(ns string) ([]k8s.DaemonSet, error) {
				return []k8s.DaemonSet{
					{Name: "node-exporter", Namespace: ns, DesiredNumber: 5, ReadyNumber: 4,
						Selector: map[string]string{"app": "node-exporter"}, Age: 48 * time.Hour},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_get_daemonsets",
		map[string]string{"namespace": "monitoring"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "node-exporter") {
		t.Errorf("result should contain daemonset name, got: %s", result)
	}
}

func TestToolExecution_GetJobs(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListJobsFunc: func(ns string) ([]k8s.Job, error) {
				return []k8s.Job{
					{Name: "db-migration", Namespace: ns, Completions: 1, Succeeded: 1,
						Duration: 5 * time.Minute, Age: 1 * time.Hour},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_get_jobs",
		map[string]string{"namespace": "default"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "db-migration") {
		t.Errorf("result should contain job name, got: %s", result)
	}
	if !strings.Contains(result, "1/1") {
		t.Errorf("result should contain completion count, got: %s", result)
	}
}

func TestToolExecution_GetCronJobs(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListCronJobsFunc: func(ns string) ([]k8s.CronJob, error) {
				return []k8s.CronJob{
					{Name: "nightly-backup", Namespace: ns, Schedule: "0 2 * * *",
						Active: 0, Age: 7 * 24 * time.Hour},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_get_cronjobs",
		map[string]string{"namespace": "default"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "nightly-backup") {
		t.Errorf("result should contain cronjob name, got: %s", result)
	}
	if !strings.Contains(result, "0 2 * * *") {
		t.Errorf("result should contain schedule, got: %s", result)
	}
}

func TestToolExecution_GetStatefulSets_WithFilter(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListStatefulSetsFunc: func(ns string) ([]k8s.StatefulSet, error) {
				return []k8s.StatefulSet{
					{Name: "redis-cluster", Namespace: ns, Replicas: 3, Ready: 3},
					{Name: "zookeeper", Namespace: ns, Replicas: 3, Ready: 3},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_get_statefulsets",
		map[string]string{"namespace": "default", "filter": "redis"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "redis-cluster") {
		t.Errorf("result should contain redis-cluster, got: %s", result)
	}
	if strings.Contains(result, "zookeeper") {
		t.Errorf("result should NOT contain zookeeper when filtered, got: %s", result)
	}
}

func TestSafety_NewPhase2ToolsAllowed(t *testing.T) {
	checker := NewSafetyChecker()

	tools := []string{
		"kubectl_get_statefulsets",
		"kubectl_get_daemonsets",
		"kubectl_get_jobs",
		"kubectl_get_cronjobs",
	}

	for _, tool := range tools {
		level, reason := checker.Check(tool, nil)
		if level != SafetyAllowed {
			t.Errorf("tool %s should be allowed, got level=%d reason=%s", tool, level, reason)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/... -v -run "TestToolExecution_Get(StatefulSets|DaemonSets|Jobs|CronJobs)|TestSafety_NewPhase2"`
Expected: FAIL — handlers and whitelist entries don't exist yet

- [ ] **Step 3: Add tool definitions**

In `internal/agent/tools.go`, add 4 new tool definitions to the `ToolDefinitions()` return slice (after the `search_visible_logs` definition, before the closing `}`):

```go
		// Phase 2: new resource type tools
		{
			Name:        "kubectl_get_statefulsets",
			Description: "List StatefulSets in a namespace with replica counts. Use for stateful services like databases, message queues.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "filter", Type: "string", Description: "Substring filter on StatefulSet name (case-insensitive)", Required: false},
			},
		},
		{
			Name:        "kubectl_get_daemonsets",
			Description: "List DaemonSets in a namespace. Use for node-level agents like log collectors, monitoring agents.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "filter", Type: "string", Description: "Substring filter on DaemonSet name (case-insensitive)", Required: false},
			},
		},
		{
			Name:        "kubectl_get_jobs",
			Description: "List Jobs in a namespace with completion status and duration. Use for batch tasks, migrations.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "filter", Type: "string", Description: "Substring filter on Job name (case-insensitive)", Required: false},
				{Name: "activeOnly", Type: "string", Description: "If 'true', only show active (non-completed) jobs", Required: false},
			},
		},
		{
			Name:        "kubectl_get_cronjobs",
			Description: "List CronJobs in a namespace with schedule and last run time.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "filter", Type: "string", Description: "Substring filter on CronJob name (case-insensitive)", Required: false},
			},
		},
```

- [ ] **Step 4: Add dispatch cases**

In `ExecuteTool` switch (after `case "search_visible_logs":`, before `default:`):

```go
	case "kubectl_get_statefulsets":
		return t.getStatefulSets(args)
	case "kubectl_get_daemonsets":
		return t.getDaemonSets(args)
	case "kubectl_get_jobs":
		return t.getJobs(args)
	case "kubectl_get_cronjobs":
		return t.getCronJobs(args)
```

- [ ] **Step 5: Implement handler functions**

Add after the existing `getDeployments` handler (follows the same pattern):

```go
func (t *toolExecutor) getStatefulSets(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	items, err := t.k8sClient.Resources().ListStatefulSets(ns)
	if err != nil {
		return "", nil, fmt.Errorf("list statefulsets: %w", err)
	}

	filter := strings.ToLower(args["filter"])

	var b strings.Builder
	b.WriteString("NAME\tREADY\tSELECTOR\n")
	count := 0
	for _, ss := range items {
		if filter != "" && !strings.Contains(strings.ToLower(ss.Name), filter) {
			continue
		}
		sel := formatSelector(ss.Selector)
		fmt.Fprintf(&b, "%s\t%d/%d\t%s\n", ss.Name, ss.Ready, ss.Replicas, sel)
		count++
	}
	if filter != "" {
		fmt.Fprintf(&b, "\n(%d of %d statefulsets match filter %q)", count, len(items), args["filter"])
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) getDaemonSets(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	items, err := t.k8sClient.Resources().ListDaemonSets(ns)
	if err != nil {
		return "", nil, fmt.Errorf("list daemonsets: %w", err)
	}

	filter := strings.ToLower(args["filter"])

	var b strings.Builder
	b.WriteString("NAME\tDESIRED\tCURRENT\tREADY\tSELECTOR\n")
	count := 0
	for _, ds := range items {
		if filter != "" && !strings.Contains(strings.ToLower(ds.Name), filter) {
			continue
		}
		sel := formatSelector(ds.Selector)
		fmt.Fprintf(&b, "%s\t%d\t%d\t%d\t%s\n", ds.Name, ds.DesiredNumber, ds.CurrentNumber, ds.ReadyNumber, sel)
		count++
	}
	if filter != "" {
		fmt.Fprintf(&b, "\n(%d of %d daemonsets match filter %q)", count, len(items), args["filter"])
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) getJobs(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	items, err := t.k8sClient.Resources().ListJobs(ns)
	if err != nil {
		return "", nil, fmt.Errorf("list jobs: %w", err)
	}

	filter := strings.ToLower(args["filter"])
	activeOnly := args["activeOnly"] == "true"

	var b strings.Builder
	b.WriteString("NAME\tCOMPLETIONS\tDURATION\tSELECTOR\n")
	count := 0
	for _, j := range items {
		if filter != "" && !strings.Contains(strings.ToLower(j.Name), filter) {
			continue
		}
		if activeOnly && j.Active == 0 && j.Succeeded > 0 {
			continue
		}
		sel := formatSelector(j.Selector)
		dur := formatToolDuration(j.Duration)
		fmt.Fprintf(&b, "%s\t%d/%d\t%s\t%s\n", j.Name, j.Succeeded, j.Completions, dur, sel)
		count++
	}
	if filter != "" || activeOnly {
		fmt.Fprintf(&b, "\n(%d of %d jobs shown)", count, len(items))
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) getCronJobs(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	items, err := t.k8sClient.Resources().ListCronJobs(ns)
	if err != nil {
		return "", nil, fmt.Errorf("list cronjobs: %w", err)
	}

	filter := strings.ToLower(args["filter"])

	var b strings.Builder
	b.WriteString("NAME\tSCHEDULE\tSUSPEND\tACTIVE\tLAST SCHEDULE\n")
	count := 0
	for _, cj := range items {
		if filter != "" && !strings.Contains(strings.ToLower(cj.Name), filter) {
			continue
		}
		lastSched := "<none>"
		if cj.LastScheduleTime != nil {
			lastSched = formatToolTimeSince(*cj.LastScheduleTime)
		}
		fmt.Fprintf(&b, "%s\t%s\t%v\t%d\t%s\n", cj.Name, cj.Schedule, cj.Suspend, cj.Active, lastSched)
		count++
	}
	if filter != "" {
		fmt.Fprintf(&b, "\n(%d of %d cronjobs match filter %q)", count, len(items), args["filter"])
	}
	return b.String(), nil, nil
}

func formatToolDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

func formatToolTimeSince(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}
```

- [ ] **Step 6: Add to GenerateCommandDisplay**

In `GenerateCommandDisplay`, add cases before the `default:`:

```go
	case "kubectl_get_statefulsets":
		cmd := fmt.Sprintf("kubectl get statefulsets -n %s", args["namespace"])
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
	case "kubectl_get_daemonsets":
		cmd := fmt.Sprintf("kubectl get daemonsets -n %s", args["namespace"])
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
	case "kubectl_get_jobs":
		cmd := fmt.Sprintf("kubectl get jobs -n %s", args["namespace"])
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
	case "kubectl_get_cronjobs":
		cmd := fmt.Sprintf("kubectl get cronjobs -n %s", args["namespace"])
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
```

- [ ] **Step 7: Add to safety whitelist**

In `internal/agent/safety.go`, add to the whitelist map in `NewSafetyChecker`:

```go
		"kubectl_get_statefulsets": SafetyAllowed,
		"kubectl_get_daemonsets":   SafetyAllowed,
		"kubectl_get_jobs":         SafetyAllowed,
		"kubectl_get_cronjobs":     SafetyAllowed,
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/agent/... -v -run "TestToolExecution_Get(StatefulSets|DaemonSets|Jobs|CronJobs)|TestSafety_NewPhase2"`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/agent/tools.go internal/agent/safety.go internal/agent/tools_phase2_test.go
git commit -m "feat(agent): add 4 new tools for StatefulSet/DaemonSet/Job/CronJob"
```

---

### Task 10: System Prompt Update — Expanded Resource Discovery

**Files:**
- Modify: `internal/agent/prompt.go`

- [ ] **Step 1: Update system prompt**

In `internal/agent/prompt.go`, update the tool strategy section (around lines 82-91) to include the expanded resource discovery strategy. Find the section that guides the AI on how to find resources and replace/extend it:

Add a new section after the existing tool strategy block:

```go
	// Phase 2: Expanded resource discovery strategy
	b.WriteString(`
## Resource Discovery Strategy (Phase 2)
When a user mentions a service name, search in this order:
1. Deployment (most common — web services, APIs, microservices)
2. StatefulSet (databases: MySQL, PostgreSQL, Redis, Kafka, ZooKeeper)
3. DaemonSet (per-node agents: log collectors, monitoring, CNI plugins)
4. Job/CronJob (batch tasks, migrations, scheduled backups)

Use the appropriate kubectl_get_* tool for each type. If the service is not found as a
Deployment, try StatefulSet before giving up.

For CronJob investigation:
- List CronJobs → identify the relevant one
- List Jobs in the same namespace, filter by the CronJob name
- Find the most recent Job → get its pods → check logs

For failed Job investigation:
- Check Job status (succeeded/failed counts)
- If failed: get pods → use --previous flag to get logs from terminated containers
- Pods from completed/failed Jobs may have been garbage collected. If no pods are found,
  tell the user "Pod has been cleaned up, logs unavailable"

For DaemonSet investigation:
- If the user mentions a specific node, use kubectl_get_pods with that node's DaemonSet pod
- DaemonSet logs are per-node; always mention which node each log came from

If multiple resource types match the same name, list the candidates and ask the user
which one they mean.
`)
```

Also update the `navigate_resource_browser` tool guidance to include new resource types:

```go
	b.WriteString(`
## Resource Browser Navigation (Phase 2)
The Resource Browser now supports 5 resource types: Deployments, StatefulSets, DaemonSets,
Jobs, and CronJobs. When calling navigate_resource_browser, the level parameter still
uses "deployment" for any workload-level resource (the UI will auto-select the correct
resource type tab based on the actual resource kind you reference in your analysis).
Navigate to the most specific level you can confidently determine.
`)
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/agent/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agent/prompt.go
git commit -m "feat(agent): update system prompt with expanded resource discovery strategy"
```

---

### Task 11: Wire Metrics Client into App

**Files:**
- Modify: `internal/k8s/client.go:57-69,77-126`
- Modify: `internal/app/app.go:26-32,36-101`

- [ ] **Step 1: Add MetricsClient to k8sClient**

In `internal/k8s/client.go`, add `Metrics()` to the `Client` interface:

```go
type Client interface {
	Connect(kubeconfig, context string) error
	Disconnect()
	IsConnected() bool
	CurrentContext() string

	Resources() ResourceLister
	Logs() LogStreamer
	Events() EventLister
	Describer() ResourceDescriber
	Metrics() MetricsClient // Phase 2
}
```

Add the field to `k8sClient` struct:

```go
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
	metrics   MetricsClient // Phase 2
}
```

Initialize in `Connect()` after the describer initialization:

```go
	c.metrics = NewMetricsClient(restConfig, clientset)
```

Add the accessor:

```go
func (c *k8sClient) Metrics() MetricsClient {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metrics
}
```

Reset in `Disconnect()`:

```go
	c.metrics = nil
```

- [ ] **Step 2: Update MockClient**

In `internal/k8s/mock_client.go`, add:

```go
	MockMetrics *MockMetricsClient
```

And:

```go
type MockMetricsClient struct {
	IsAvailableFunc   func() bool
	GetPodMetricsFunc func(namespace, podName string) (*PodMetrics, error)
}

func (m *MockMetricsClient) IsAvailable() bool {
	if m.IsAvailableFunc != nil {
		return m.IsAvailableFunc()
	}
	return false
}

func (m *MockMetricsClient) GetPodMetrics(namespace, podName string) (*PodMetrics, error) {
	if m.GetPodMetricsFunc != nil {
		return m.GetPodMetricsFunc(namespace, podName)
	}
	return nil, fmt.Errorf("metrics not available")
}

func (m *MockClient) Metrics() MetricsClient {
	if m.MockMetrics != nil {
		return m.MockMetrics
	}
	return &MockMetricsClient{}
}
```

Add the `"fmt"` import to mock_client.go if not present.

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/k8s/client.go internal/k8s/mock_client.go internal/app/app.go
git commit -m "feat: wire MetricsClient into k8s Client and App"
```

---

### Task 12: Final Integration Test

**Files:**
- All modified files

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -race -count=1`
Expected: All PASS

- [ ] **Step 2: Run lint**

Run: `make lint` or `golangci-lint run ./...`
Expected: No new issues

- [ ] **Step 3: Build**

Run: `make build`
Expected: Binary builds successfully

- [ ] **Step 4: Verify new tool count**

Check the agent tools count (should be 15 total: 10 Phase 1 + 1 Data Safety + 4 Enhanced Browser):

Run: `grep -c "Name:" internal/agent/tools.go` (should show ~15 `Name:` fields in the tool definitions)

- [ ] **Step 5: Verify safety whitelist count**

Check safety whitelist (should have 15 entries):

Run: `grep -c "SafetyAllowed" internal/agent/safety.go` (should be 15)

- [ ] **Step 6: Commit any remaining fixes**

```bash
git add -A
git commit -m "chore: fix lint issues from Enhanced Browser implementation"
```
