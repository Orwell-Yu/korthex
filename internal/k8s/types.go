package k8s

import "time"

// Namespace represents a K8s namespace.
type Namespace struct {
	Name   string
	Status string
}

// Deployment represents a K8s deployment.
type Deployment struct {
	Name      string
	Namespace string
	Replicas  int32
	Ready     int32
	Available int32
	Labels    map[string]string
	Selector  map[string]string // .spec.selector.matchLabels
}

// Pod represents a K8s pod.
type Pod struct {
	Name       string
	Namespace  string
	Status     string // Running, Pending, CrashLoopBackOff, etc.
	Restarts   int32
	Age        time.Duration
	Containers []Container
	Labels     map[string]string
	NodeName   string
	OwnerKind  string // Deployment, StatefulSet, DaemonSet, etc.
	OwnerName  string // name of the owning resource
}

// Container represents a container within a pod.
type Container struct {
	Name  string
	Image string // container image (e.g. "mysql:8.0")
	Ready bool
	State string // running, waiting, terminated
	Ports []ContainerPort
}

// ContainerPort represents a port exposed by a container.
type ContainerPort struct {
	ContainerPort int32
	Protocol      string // TCP, UDP
}

// EnvVar represents a single environment variable in a container.
type EnvVar struct {
	Name  string
	Value string // direct value (empty if from secret)
	// SecretKeyRef fields (non-empty if sourced from a Secret)
	SecretName string
	SecretKey  string
}

// EnvFromSource represents an envFrom entry (e.g. secretRef).
type EnvFromSource struct {
	SecretName string
}

// Event represents a K8s event.
type Event struct {
	Type      string // Normal, Warning
	Reason    string
	Message   string
	Object    string // e.g., "Pod/order-svc-abc12"
	Count     int32
	FirstSeen time.Time
	LastSeen  time.Time
}

// SearchResult represents a fuzzy-search match across resources.
type SearchResult struct {
	Kind      string // "Deployment", "Pod", etc.
	Name      string
	Namespace string
}

// LogRequest specifies parameters for fetching logs.
type LogRequest struct {
	Namespace     string
	PodName       string
	Container     string        // empty = all containers
	Since         time.Duration // 0 = no time filter
	SinceTime     *time.Time
	TailLines     *int64
	Previous      bool
	Follow        bool // true = keep streaming new lines; GetLogs forces false
	AddTimestamps bool
}

// LogLine represents a single line from pod logs.
type LogLine struct {
	PodName   string
	Container string
	Content   string
}

// --- Phase 2 resource types ---

// StatefulSet represents a K8s StatefulSet.
type StatefulSet struct {
	Name      string
	Namespace string
	Replicas  int32
	Ready     int32
	Labels    map[string]string
	Selector  map[string]string
}

// DaemonSet represents a K8s DaemonSet.
type DaemonSet struct {
	Name             string
	Namespace        string
	DesiredScheduled int32
	CurrentScheduled int32
	Ready            int32
	Updated          int32
	Available        int32
	Labels           map[string]string
	Selector         map[string]string
}

// Job represents a K8s Job.
type Job struct {
	Name        string
	Namespace   string
	Completions int32
	Succeeded   int32
	Failed      int32
	Active      int32
	StartTime   *time.Time
	Duration    time.Duration
	Labels      map[string]string
	Selector    map[string]string
}

// CronJob represents a K8s CronJob.
type CronJob struct {
	Name             string
	Namespace        string
	Schedule         string
	Suspend          bool
	Active           int
	LastScheduleTime *time.Time
	Labels           map[string]string
}

// ResourceKind identifies a type of K8s workload resource.
type ResourceKind string

const (
	KindDeployment  ResourceKind = "Deployment"
	KindStatefulSet ResourceKind = "StatefulSet"
	KindDaemonSet   ResourceKind = "DaemonSet"
	KindJob         ResourceKind = "Job"
	KindCronJob     ResourceKind = "CronJob"
)
