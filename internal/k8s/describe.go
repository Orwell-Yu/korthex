package k8s

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

// resourceDescriber implements ResourceDescriber by fetching the resource
// and its events, then assembling kubectl-describe-like output.
type resourceDescriber struct {
	clientset kubernetes.Interface
	events    *eventLister
}

func (d *resourceDescriber) Describe(namespace, kind, name string) (string, error) {
	ctx := context.Background()

	switch strings.ToLower(kind) {
	case "namespace":
		return d.describeNamespace(ctx, name)
	case "deployment":
		return d.describeDeployment(ctx, namespace, name)
	case "statefulset":
		return d.describeStatefulSet(ctx, namespace, name)
	case "daemonset":
		return d.describeDaemonSet(ctx, namespace, name)
	case "job":
		return d.describeJob(ctx, namespace, name)
	case "cronjob":
		return d.describeCronJob(ctx, namespace, name)
	case "pod":
		return d.describePod(ctx, namespace, name)
	default:
		return "", fmt.Errorf("unsupported kind: %s", kind)
	}
}

func (d *resourceDescriber) describeNamespace(ctx context.Context, name string) (string, error) {
	ns, err := d.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get namespace %s: %w", name, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Name:         %s\n", ns.Name)
	fmt.Fprintf(&b, "Status:       %s\n", ns.Status.Phase)
	fmt.Fprintf(&b, "Labels:       %s\n", formatLabels(ns.Labels))
	fmt.Fprintf(&b, "Annotations:  %s\n", formatLabels(ns.Annotations))

	events, _ := d.events.ListEventsForResource("", "Namespace", name)
	appendEvents(&b, events)

	return b.String(), nil
}

func (d *resourceDescriber) describeDeployment(ctx context.Context, namespace, name string) (string, error) {
	dep, err := d.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Name:               %s\n", dep.Name)
	fmt.Fprintf(&b, "Namespace:          %s\n", dep.Namespace)
	fmt.Fprintf(&b, "Labels:             %s\n", formatLabels(dep.Labels))
	fmt.Fprintf(&b, "Annotations:        %s\n", formatLabels(dep.Annotations))
	fmt.Fprintf(&b, "Selector:           %s\n", formatSelector(dep.Spec.Selector))
	fmt.Fprintf(&b, "Replicas:           %s\n", formatDeploymentReplicas(dep))
	fmt.Fprintf(&b, "Strategy:           %s\n", dep.Spec.Strategy.Type)
	appendContainerSpecs(&b, dep.Spec.Template.Spec.Containers)

	events, _ := d.events.ListEventsForResource(namespace, "Deployment", name)
	appendEvents(&b, events)

	return b.String(), nil
}

func (d *resourceDescriber) describePod(ctx context.Context, namespace, name string) (string, error) {
	pod, err := d.clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get pod %s/%s: %w", namespace, name, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Name:         %s\n", pod.Name)
	fmt.Fprintf(&b, "Namespace:    %s\n", pod.Namespace)
	fmt.Fprintf(&b, "Node:         %s\n", pod.Spec.NodeName)
	fmt.Fprintf(&b, "Status:       %s\n", pod.Status.Phase)
	fmt.Fprintf(&b, "IP:           %s\n", pod.Status.PodIP)
	fmt.Fprintf(&b, "Labels:       %s\n", formatLabels(pod.Labels))
	appendPodContainerStatus(&b, pod)

	events, _ := d.events.ListEventsForResource(namespace, "Pod", name)
	appendEvents(&b, events)

	return b.String(), nil
}

// --- formatting helpers ---

func formatLabels(m map[string]string) string {
	if len(m) == 0 {
		return "<none>"
	}
	pairs := make([]string, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, k+"="+v)
	}
	return strings.Join(pairs, ", ")
}

func formatSelector(sel *metav1.LabelSelector) string {
	if sel == nil {
		return "<none>"
	}
	return formatLabels(sel.MatchLabels)
}

func formatDeploymentReplicas(dep *appsv1.Deployment) string {
	desired := int32(0)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	return fmt.Sprintf("%d desired | %d updated | %d total | %d available | %d unavailable",
		desired,
		dep.Status.UpdatedReplicas,
		dep.Status.Replicas,
		dep.Status.AvailableReplicas,
		dep.Status.UnavailableReplicas,
	)
}

func appendContainerSpecs(b *strings.Builder, containers []corev1.Container) {
	if len(containers) == 0 {
		return
	}
	fmt.Fprintf(b, "Containers:\n")
	for _, c := range containers {
		fmt.Fprintf(b, "  %s:\n", c.Name)
		fmt.Fprintf(b, "    Image:   %s\n", c.Image)
		if len(c.Ports) > 0 {
			ports := make([]string, 0, len(c.Ports))
			for _, p := range c.Ports {
				ports = append(ports, fmt.Sprintf("%d/%s", p.ContainerPort, p.Protocol))
			}
			fmt.Fprintf(b, "    Ports:   %s\n", strings.Join(ports, ", "))
		}
	}
}

func appendPodContainerStatus(b *strings.Builder, pod *corev1.Pod) {
	if len(pod.Status.ContainerStatuses) == 0 {
		return
	}
	fmt.Fprintf(b, "Containers:\n")
	for _, cs := range pod.Status.ContainerStatuses {
		fmt.Fprintf(b, "  %s:\n", cs.Name)
		fmt.Fprintf(b, "    Ready:    %v\n", cs.Ready)
		fmt.Fprintf(b, "    Restarts: %d\n", cs.RestartCount)
		fmt.Fprintf(b, "    State:    %s\n", containerState(cs.State))
	}
}

func appendEvents(b *strings.Builder, events []Event) {
	fmt.Fprintf(b, "Events:\n")
	if len(events) == 0 {
		fmt.Fprintf(b, "  <none>\n")
		return
	}
	for _, ev := range events {
		fmt.Fprintf(b, "  %s  %s  %s: %s\n", ev.Type, ev.Reason, ev.Object, ev.Message)
	}
}

// --- Phase 2 describe methods ---

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
	suspend := "False"
	if cj.Spec.Suspend != nil && *cj.Spec.Suspend {
		suspend = "True"
	}
	fmt.Fprintf(&b, "Suspend:            %s\n", suspend)
	fmt.Fprintf(&b, "Active:             %d\n", len(cj.Status.Active))
	if cj.Status.LastScheduleTime != nil {
		fmt.Fprintf(&b, "Last Schedule:      %s\n", cj.Status.LastScheduleTime.Format("2006-01-02 15:04:05"))
	}
	appendContainerSpecs(&b, cj.Spec.JobTemplate.Spec.Template.Spec.Containers)

	events, _ := d.events.ListEventsForResource(namespace, "CronJob", name)
	appendEvents(&b, events)

	return b.String(), nil
}

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
	return fmt.Sprintf("%d/%d succeeded, %d failed",
		job.Status.Succeeded, desired, job.Status.Failed)
}
