# internal/k8s - Kubernetes Client

> **Parent docs:** [CLAUDE.md (root)](../../CLAUDE.md) | [SPEC.md Section 3.3](../../SPEC.md) | [PRD Section 6.3](../../PRD.md)
>
> **Module rules:** [CLAUDE.md (this module)](./CLAUDE.md)
>
> **Depends on:** [config](../config/README.md) | **Depended by:** [agent](../agent/README.md), [ui](../ui/README.md)

## Responsibility

All Kubernetes cluster interaction. Resource discovery via Informer/Cache (zero API calls), log streaming via GetLogs API, event retrieval, and describe aggregation. Expose high-level interfaces; consumers never touch client-go types directly.

## Public Interfaces

```go
// Client is the top-level K8s interface, aggregating all sub-interfaces
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

type ResourceLister interface {
    ListNamespaces() ([]Namespace, error)
    ListDeployments(namespace string) ([]Deployment, error)
    ListPods(namespace string) ([]Pod, error)
    ListPodsBySelector(namespace string, selector map[string]string) ([]Pod, error)
    FindDeploymentByName(namespace, name string) (*Deployment, error)
    SearchResources(namespace, query string) ([]SearchResult, error)  // fuzzy
}

type LogStreamer interface {
    StreamLogs(ctx context.Context, req LogRequest, ch chan<- LogLine) error
    GetLogs(ctx context.Context, req LogRequest) ([]LogLine, error)
    StreamMultiPodLogs(ctx context.Context, namespace string,
        selector map[string]string, opts LogRequest, ch chan<- LogLine) error
}

type EventLister interface {
    ListEvents(namespace string) ([]Event, error)
    ListEventsForResource(namespace, kind, name string) ([]Event, error)
}

type ResourceDescriber interface {
    Describe(namespace, kind, name string) (string, error)
}

// Phase 2: Resource Registry pattern (k9s DAO Registry inspired)
type ResourceAccessor interface {
    List(namespace string, filter string) ([]ResourceItem, error)
    GetLabelSelector(namespace, name string) (string, error)
}

func AccessorFor(resourceType string) (ResourceAccessor, bool)
```

## Files

| File | Responsibility |
|------|---------------|
| `client.go` | Client implementation, kubeconfig loading, rest.Config creation, factory lifecycle |
| `informer.go` | Informer factory management: per-namespace factories, LRU eviction (10 slots), 30s grace period, 10min resync, atomic dedup on refresh. **See [CLAUDE.md](./CLAUDE.md) for full LRU + grace period state machine.** |
| `resources.go` | ResourceLister using DynamicSharedInformerFactory + Lister. `SearchResources` uses sahilm/fuzzy |
| `logs.go` | LogStreamer: per-container goroutine, bufio.Scanner, buffered channel (cap=50), non-blocking send |
| `events.go` | EventLister using CoreV1().Events().List() |
| `describe.go` | ResourceDescriber assembling describe-like output from Get + Events |
| `types.go` | Namespace, Deployment, Pod, Container, Event, LogRequest, LogLine, SearchResult, StatefulSet, DaemonSet, Job, CronJob, ResourceItem structs |
| `registry.go` | ResourceAccessor registry: per-type accessor implementations (Deployment, StatefulSet, DaemonSet, Job, CronJob), AccessorFor lookup |
| `client_test.go` | Integration tests with fake clientset |
| `logs_test.go` | Log streaming tests with fake io.ReadCloser |

## Dependencies

- **External**: `k8s.io/client-go`, `k8s.io/api`, `k8s.io/apimachinery`, `sahilm/fuzzy`, `cenkalti/backoff/v4`
- **Internal**: `config` (for KubernetesConfig)

## Key Design Patterns (from k9s)

### Informer + Cache Lister
```
K8s API Server --Watch Stream--> Informer (per-ns) --List from cache--> TUI
```
- `DynamicSharedInformerFactory` per namespace
- Resource lists from Lister (in-memory cache), zero API calls
- Resync interval: 10 minutes
- Switching namespace: start/stop corresponding Informer Factory

### Log Streaming Pipeline
```
K8s API (io.ReadCloser per container)
  --> per-container goroutine (bufio.Scanner)
  --> buffered channel (cap=50, non-blocking send)
  --> caller reads from merged channel
```
- Non-blocking send: `select { case ch <- line: default: /* drop */ }`
- Per-container goroutine: one slow container doesn't block others
- Exponential backoff retry: initial 500ms, max 15s, max 20 attempts
- Pod state awareness: stop retry on Succeeded/Failed/Deleted

### Large Cluster Optimization
| Cluster Size | Strategy |
|---|---|
| < 50 ns | Normal: start/stop informer on ns switch |
| 50-200 ns | LRU: cache 10 most recent ns informers |
| > 200 ns | Force ns selection, disable all-namespaces |
| > 500 pods/ns | Default label selector filter, prompt user |
| > 10000 lines/min | Auto-downsample + prompt to narrow scope |

## Testing Strategy

- **Unit tests**: Fake clientset (`client-go/kubernetes/fake`) for resource listing
- **Log tests**: Fake `io.ReadCloser` simulating various log streams (fast, slow, error, empty)
- **Informer tests**: Verify LRU eviction, namespace switching, cache invalidation
- **Integration tests**: minikube/kind cluster for end-to-end resource + log verification

## Phase 2+ Extension Points

- Add `WatchResource` for real-time resource status updates
- Add Phase 3 write operation methods (Scale, Restart, Delete)
