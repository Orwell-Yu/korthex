package k8s

import "fmt"

// ResourceAccessor provides list access for a resource kind via the registry.
type ResourceAccessor interface {
	Kind() ResourceKind
	List(lister ResourceLister, namespace string) ([]ResourceItem, error)
	GetLabelSelector(lister ResourceLister, namespace, name string) (map[string]string, error)
}

// ResourceItem is a generic resource entry returned by the registry.
type ResourceItem struct {
	Kind      ResourceKind
	Name      string
	Namespace string
	Ready     string // "3/3" format
	Status    string // extra info column
	Age       string
	Labels    map[string]string
	Selector  map[string]string
}

// AllKinds returns all supported resource kinds in display order.
func AllKinds() []ResourceKind {
	return []ResourceKind{
		KindDeployment,
		KindStatefulSet,
		KindDaemonSet,
		KindJob,
		KindCronJob,
	}
}

var registry = map[ResourceKind]ResourceAccessor{
	KindDeployment:  &deploymentAccessor{},
	KindStatefulSet: &statefulSetAccessor{},
	KindDaemonSet:   &daemonSetAccessor{},
	KindJob:         &jobAccessor{},
	KindCronJob:     &cronJobAccessor{},
}

// AccessorFor returns the ResourceAccessor for the given kind.
func AccessorFor(kind ResourceKind) (ResourceAccessor, bool) {
	a, ok := registry[kind]
	return a, ok
}

// --- Deployment ---

type deploymentAccessor struct{}

func (a *deploymentAccessor) Kind() ResourceKind { return KindDeployment }

func (a *deploymentAccessor) List(lister ResourceLister, namespace string) ([]ResourceItem, error) {
	deps, err := lister.ListDeployments(namespace)
	if err != nil {
		return nil, err
	}
	items := make([]ResourceItem, len(deps))
	for i, d := range deps {
		items[i] = ResourceItem{
			Kind:      KindDeployment,
			Name:      d.Name,
			Namespace: d.Namespace,
			Ready:     fmt.Sprintf("%d/%d", d.Ready, d.Replicas),
			Labels:    d.Labels,
			Selector:  d.Selector,
		}
	}
	return items, nil
}

func (a *deploymentAccessor) GetLabelSelector(lister ResourceLister, namespace, name string) (map[string]string, error) {
	dep, err := lister.FindDeploymentByName(namespace, name)
	if err != nil {
		return nil, err
	}
	return dep.Selector, nil
}

// --- StatefulSet ---

type statefulSetAccessor struct{}

func (a *statefulSetAccessor) Kind() ResourceKind { return KindStatefulSet }

func (a *statefulSetAccessor) List(lister ResourceLister, namespace string) ([]ResourceItem, error) {
	ssList, err := lister.ListStatefulSets(namespace)
	if err != nil {
		return nil, err
	}
	items := make([]ResourceItem, len(ssList))
	for i, ss := range ssList {
		items[i] = ResourceItem{
			Kind:      KindStatefulSet,
			Name:      ss.Name,
			Namespace: ss.Namespace,
			Ready:     fmt.Sprintf("%d/%d", ss.Ready, ss.Replicas),
			Labels:    ss.Labels,
			Selector:  ss.Selector,
		}
	}
	return items, nil
}

func (a *statefulSetAccessor) GetLabelSelector(lister ResourceLister, namespace, name string) (map[string]string, error) {
	ssList, err := lister.ListStatefulSets(namespace)
	if err != nil {
		return nil, err
	}
	for _, ss := range ssList {
		if ss.Name == name {
			return ss.Selector, nil
		}
	}
	return nil, fmt.Errorf("statefulset %s/%s not found", namespace, name)
}

// --- DaemonSet ---

type daemonSetAccessor struct{}

func (a *daemonSetAccessor) Kind() ResourceKind { return KindDaemonSet }

func (a *daemonSetAccessor) List(lister ResourceLister, namespace string) ([]ResourceItem, error) {
	dsList, err := lister.ListDaemonSets(namespace)
	if err != nil {
		return nil, err
	}
	items := make([]ResourceItem, len(dsList))
	for i, ds := range dsList {
		items[i] = ResourceItem{
			Kind:      KindDaemonSet,
			Name:      ds.Name,
			Namespace: ds.Namespace,
			Ready:     fmt.Sprintf("%d/%d", ds.Ready, ds.DesiredScheduled),
			Labels:    ds.Labels,
			Selector:  ds.Selector,
		}
	}
	return items, nil
}

func (a *daemonSetAccessor) GetLabelSelector(lister ResourceLister, namespace, name string) (map[string]string, error) {
	dsList, err := lister.ListDaemonSets(namespace)
	if err != nil {
		return nil, err
	}
	for _, ds := range dsList {
		if ds.Name == name {
			return ds.Selector, nil
		}
	}
	return nil, fmt.Errorf("daemonset %s/%s not found", namespace, name)
}

// --- Job ---

type jobAccessor struct{}

func (a *jobAccessor) Kind() ResourceKind { return KindJob }

func (a *jobAccessor) List(lister ResourceLister, namespace string) ([]ResourceItem, error) {
	jobs, err := lister.ListJobs(namespace)
	if err != nil {
		return nil, err
	}
	items := make([]ResourceItem, len(jobs))
	for i, j := range jobs {
		status := fmt.Sprintf("%d/%d", j.Succeeded, j.Completions)
		if j.Failed > 0 {
			status += fmt.Sprintf(" (%d failed)", j.Failed)
		}
		items[i] = ResourceItem{
			Kind:      KindJob,
			Name:      j.Name,
			Namespace: j.Namespace,
			Ready:     status,
			Labels:    j.Labels,
			Selector:  j.Selector,
		}
	}
	return items, nil
}

func (a *jobAccessor) GetLabelSelector(lister ResourceLister, namespace, name string) (map[string]string, error) {
	jobs, err := lister.ListJobs(namespace)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j.Name == name {
			return j.Selector, nil
		}
	}
	return nil, fmt.Errorf("job %s/%s not found", namespace, name)
}

// --- CronJob ---

type cronJobAccessor struct{}

func (a *cronJobAccessor) Kind() ResourceKind { return KindCronJob }

func (a *cronJobAccessor) List(lister ResourceLister, namespace string) ([]ResourceItem, error) {
	cjList, err := lister.ListCronJobs(namespace)
	if err != nil {
		return nil, err
	}
	items := make([]ResourceItem, len(cjList))
	for i, cj := range cjList {
		suspend := "False"
		if cj.Suspend {
			suspend = "True"
		}
		items[i] = ResourceItem{
			Kind:      KindCronJob,
			Name:      cj.Name,
			Namespace: cj.Namespace,
			Ready:     cj.Schedule,
			Status:    fmt.Sprintf("suspend=%s active=%d", suspend, cj.Active),
			Labels:    cj.Labels,
		}
	}
	return items, nil
}

func (a *cronJobAccessor) GetLabelSelector(_ ResourceLister, namespace, name string) (map[string]string, error) {
	// CronJobs don't have direct selectors; pods are owned by child Jobs.
	// Return job-name label which is set by the CronJob controller.
	return map[string]string{"job-name": name}, nil
}
