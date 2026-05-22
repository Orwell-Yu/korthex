package k8s

import (
	"fmt"
	"sort"
	"time"

	"github.com/sahilm/fuzzy"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// resourceLister implements ResourceLister using Informer Listers (zero API calls).
type resourceLister struct {
	clientset kubernetes.Interface
	informers *InformerManager
}

func (r *resourceLister) ListNamespaces() ([]Namespace, error) {
	// TODO(phase2): Access + register + EnsureSynced is not atomic — another
	// goroutine could LRU-evict this namespace between Access() and EnsureSynced().
	// Phase 1 is single-threaded Elm, so safe for now. Consider returning a
	// handle from Access that holds the lock until sync completes.
	factory := r.informers.Access("")
	factory.Core().V1().Namespaces().Informer() // register before sync
	r.informers.EnsureSynced("")

	lister := factory.Core().V1().Namespaces().Lister()
	nsList, err := lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	result := make([]Namespace, 0, len(nsList))
	for _, ns := range nsList {
		result = append(result, convertNamespace(ns))
	}
	return result, nil
}

func (r *resourceLister) ListDeployments(namespace string) ([]Deployment, error) {
	factory := r.informers.Access(namespace)
	factory.Apps().V1().Deployments().Informer() // register before sync
	r.informers.EnsureSynced(namespace)

	lister := factory.Apps().V1().Deployments().Lister().Deployments(namespace)
	depList, err := lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}

	result := make([]Deployment, 0, len(depList))
	for _, dep := range depList {
		result = append(result, convertDeployment(dep))
	}
	return result, nil
}

func (r *resourceLister) ListPods(namespace string) ([]Pod, error) {
	factory := r.informers.Access(namespace)
	factory.Core().V1().Pods().Informer() // register before sync
	r.informers.EnsureSynced(namespace)

	lister := factory.Core().V1().Pods().Lister().Pods(namespace)
	podList, err := lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	result := make([]Pod, 0, len(podList))
	for _, pod := range podList {
		result = append(result, convertPod(pod))
	}
	return result, nil
}

func (r *resourceLister) ListPodsBySelector(namespace string, selector map[string]string) ([]Pod, error) {
	factory := r.informers.Access(namespace)
	factory.Core().V1().Pods().Informer() // register before sync
	r.informers.EnsureSynced(namespace)

	lister := factory.Core().V1().Pods().Lister().Pods(namespace)

	sel := labels.SelectorFromSet(labels.Set(selector))
	podList, err := lister.List(sel)
	if err != nil {
		return nil, fmt.Errorf("list pods by selector: %w", err)
	}

	result := make([]Pod, 0, len(podList))
	for _, pod := range podList {
		result = append(result, convertPod(pod))
	}
	return result, nil
}

func (r *resourceLister) FindDeploymentByName(namespace, name string) (*Deployment, error) {
	factory := r.informers.Access(namespace)
	factory.Apps().V1().Deployments().Informer() // register before sync
	r.informers.EnsureSynced(namespace)

	lister := factory.Apps().V1().Deployments().Lister().Deployments(namespace)

	dep, err := lister.Get(name)
	if err != nil {
		return nil, fmt.Errorf("find deployment %s/%s: %w", namespace, name, err)
	}

	d := convertDeployment(dep)
	return &d, nil
}

// --- Phase 2: new resource type listing ---

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

	// Sort by start time descending (newest first)
	sort.Slice(jobList, func(i, j int) bool {
		ti := jobList[i].CreationTimestamp.Time
		tj := jobList[j].CreationTimestamp.Time
		return ti.After(tj)
	})

	result := make([]Job, 0, len(jobList))
	for _, job := range jobList {
		result = append(result, convertJob(job))
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

// searchable wraps names so fuzzy.Find can match against them.
type searchable []string

func (s searchable) String(i int) string { return s[i] }
func (s searchable) Len() int            { return len(s) }

func (r *resourceLister) SearchResources(namespace, query string) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	type candidate struct {
		kind string
		name string
	}
	var candidates []candidate
	var names searchable

	// Collect deployments.
	deps, err := r.ListDeployments(namespace)
	if err != nil {
		return nil, err
	}
	for _, d := range deps {
		candidates = append(candidates, candidate{kind: "Deployment", name: d.Name})
		names = append(names, d.Name)
	}

	// Collect statefulsets.
	ssList, err := r.ListStatefulSets(namespace)
	if err == nil { // non-critical: skip on error
		for _, ss := range ssList {
			candidates = append(candidates, candidate{kind: "StatefulSet", name: ss.Name})
			names = append(names, ss.Name)
		}
	}

	// Collect daemonsets.
	dsList, err := r.ListDaemonSets(namespace)
	if err == nil {
		for _, ds := range dsList {
			candidates = append(candidates, candidate{kind: "DaemonSet", name: ds.Name})
			names = append(names, ds.Name)
		}
	}

	// Collect jobs.
	jobList, err := r.ListJobs(namespace)
	if err == nil {
		for _, j := range jobList {
			candidates = append(candidates, candidate{kind: "Job", name: j.Name})
			names = append(names, j.Name)
		}
	}

	// Collect cronjobs.
	cjList, err := r.ListCronJobs(namespace)
	if err == nil {
		for _, cj := range cjList {
			candidates = append(candidates, candidate{kind: "CronJob", name: cj.Name})
			names = append(names, cj.Name)
		}
	}

	// Collect pods.
	pods, err := r.ListPods(namespace)
	if err != nil {
		return nil, err
	}
	for _, p := range pods {
		candidates = append(candidates, candidate{kind: "Pod", name: p.Name})
		names = append(names, p.Name)
	}

	matches := fuzzy.FindFrom(query, names)
	results := make([]SearchResult, 0, len(matches))
	for _, m := range matches {
		c := candidates[m.Index]
		results = append(results, SearchResult{
			Kind:      c.kind,
			Name:      c.name,
			Namespace: namespace,
		})
	}
	return results, nil
}

// --- type converters (client-go → Korthex) ---

func convertNamespace(ns *corev1.Namespace) Namespace {
	return Namespace{
		Name:   ns.Name,
		Status: string(ns.Status.Phase),
	}
}

func convertDeployment(dep *appsv1.Deployment) Deployment {
	var selector map[string]string
	if dep.Spec.Selector != nil {
		selector = dep.Spec.Selector.MatchLabels
	}
	var replicas int32
	if dep.Spec.Replicas != nil {
		replicas = *dep.Spec.Replicas
	}
	return Deployment{
		Name:      dep.Name,
		Namespace: dep.Namespace,
		Replicas:  replicas,
		Ready:     dep.Status.ReadyReplicas,
		Available: dep.Status.AvailableReplicas,
		Labels:    dep.Labels,
		Selector:  selector,
	}
}

func convertStatefulSet(ss *appsv1.StatefulSet) StatefulSet {
	var selector map[string]string
	if ss.Spec.Selector != nil {
		selector = ss.Spec.Selector.MatchLabels
	}
	var replicas int32
	if ss.Spec.Replicas != nil {
		replicas = *ss.Spec.Replicas
	}
	return StatefulSet{
		Name:      ss.Name,
		Namespace: ss.Namespace,
		Replicas:  replicas,
		Ready:     ss.Status.ReadyReplicas,
		Labels:    ss.Labels,
		Selector:  selector,
	}
}

func convertDaemonSet(ds *appsv1.DaemonSet) DaemonSet {
	var selector map[string]string
	if ds.Spec.Selector != nil {
		selector = ds.Spec.Selector.MatchLabels
	}
	return DaemonSet{
		Name:             ds.Name,
		Namespace:        ds.Namespace,
		DesiredScheduled: ds.Status.DesiredNumberScheduled,
		CurrentScheduled: ds.Status.CurrentNumberScheduled,
		Ready:            ds.Status.NumberReady,
		Updated:          ds.Status.UpdatedNumberScheduled,
		Available:        ds.Status.NumberAvailable,
		Labels:           ds.Labels,
		Selector:         selector,
	}
}

func convertJob(job *batchv1.Job) Job {
	var selector map[string]string
	if job.Spec.Selector != nil {
		selector = job.Spec.Selector.MatchLabels
	}
	var completions int32
	if job.Spec.Completions != nil {
		completions = *job.Spec.Completions
	}
	var startTime *time.Time
	if job.Status.StartTime != nil {
		t := job.Status.StartTime.Time
		startTime = &t
	}
	var duration time.Duration
	if startTime != nil {
		if job.Status.CompletionTime != nil {
			duration = job.Status.CompletionTime.Sub(*startTime)
		} else {
			duration = time.Since(*startTime)
		}
	}
	return Job{
		Name:        job.Name,
		Namespace:   job.Namespace,
		Completions: completions,
		Succeeded:   job.Status.Succeeded,
		Failed:      job.Status.Failed,
		Active:      job.Status.Active,
		StartTime:   startTime,
		Duration:    duration,
		Labels:      job.Labels,
		Selector:    selector,
	}
}

func convertCronJob(cj *batchv1.CronJob) CronJob {
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
		Active:           len(cj.Status.Active),
		LastScheduleTime: lastSchedule,
		Labels:           cj.Labels,
	}
}

func convertPod(pod *corev1.Pod) Pod {
	// Build container list from spec (for image/ports) merged with status (for ready/state).
	statusByName := make(map[string]corev1.ContainerStatus, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		statusByName[cs.Name] = cs
	}

	containers := make([]Container, 0, len(pod.Spec.Containers))
	for _, spec := range pod.Spec.Containers {
		c := Container{
			Name:  spec.Name,
			Image: spec.Image,
		}
		if cs, ok := statusByName[spec.Name]; ok {
			c.Ready = cs.Ready
			c.State = containerState(cs.State)
		}
		for _, p := range spec.Ports {
			c.Ports = append(c.Ports, ContainerPort{
				ContainerPort: p.ContainerPort,
				Protocol:      string(p.Protocol),
			})
		}
		containers = append(containers, c)
	}

	var totalRestarts int32
	for _, cs := range pod.Status.ContainerStatuses {
		totalRestarts += cs.RestartCount
	}

	age := time.Duration(0)
	if !pod.CreationTimestamp.IsZero() {
		age = time.Since(pod.CreationTimestamp.Time)
	}

	var ownerKind, ownerName string
	if len(pod.OwnerReferences) > 0 {
		ownerKind = pod.OwnerReferences[0].Kind
		ownerName = pod.OwnerReferences[0].Name
	}

	return Pod{
		Name:       pod.Name,
		Namespace:  pod.Namespace,
		Status:     string(pod.Status.Phase),
		Restarts:   totalRestarts,
		Age:        age,
		Containers: containers,
		Labels:     pod.Labels,
		NodeName:   pod.Spec.NodeName,
		OwnerKind:  ownerKind,
		OwnerName:  ownerName,
	}
}

func containerState(state corev1.ContainerState) string {
	switch {
	case state.Running != nil:
		return "running"
	case state.Waiting != nil:
		return "waiting"
	case state.Terminated != nil:
		return "terminated"
	default:
		return "unknown"
	}
}
