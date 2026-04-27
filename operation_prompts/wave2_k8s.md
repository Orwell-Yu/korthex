# Wave 2C: internal/k8s — Kubernetes Client

> **Prerequisite:** Wave 1 (logparse + config) merged to main
> **Branch:** feat/k8s
> **Parallel with:** wave2_llm.md (Instance D)
> **Files owned:** `internal/k8s/*.go` (nothing else)
> **Estimated time:** 5-7 hours

---

You are developing the `internal/k8s` module for the Korthex project — all Kubernetes cluster interaction including resource discovery via Informer/Cache, log streaming, events, and describe.

## Step 1: Read Documentation

Read these files **in order** before writing any code:
1. `internal/k8s/CLAUDE.md` — module rules (client-go types don't leak, Informer Cache read, non-blocking log send, LRU + Grace Period design with full pseudocode)
2. `internal/k8s/README.md` — interfaces, file responsibilities, performance patterns, large cluster strategy
3. `SPEC.md` Section 3.3 — K8s interface contract (types already in skeleton)
4. `SPEC.md` Section 4.3 — Log streaming pipeline design
5. `PRD.md` Section 6.3 — Performance architecture (Informer + Cache, Ring Buffer, large cluster optimization)

**Pay special attention** to the "Informer LRU + Grace Period Design" section in `k8s/CLAUDE.md` — it has complete data structures, state machine diagram, and pseudocode for `Access()` and `Release()`.

## Step 2: Implement

The skeleton files already have type and interface definitions. Implement the logic.

### File 1: `internal/k8s/types.go`

Already defined in skeleton. Verify completeness, add any helper methods if needed.

### File 2: `internal/k8s/client.go`

Implement `Client`:
- `Connect(kubeconfig, context string) error` — load kubeconfig using `clientcmd`, build `rest.Config`, create `kubernetes.Clientset`
- `Disconnect()` — stop all informers, clean up
- `IsConnected() bool`, `CurrentContext() string`
- `Resources()`, `Logs()`, `Events()`, `Describer()` — return sub-interface implementations
- Store `*kubernetes.Clientset` and `*rest.Config` internally

Constructor: `func NewClient() Client`

### File 3: `internal/k8s/informer.go`

Implement `InformerManager` — **follow the pseudocode in k8s/CLAUDE.md exactly**:

```go
type informerEntry struct {
    factory    informers.SharedInformerFactory
    stopCh     chan struct{}
    lastUsed   time.Time
    graceTimer *time.Timer
}

type InformerManager struct {
    mu       sync.Mutex
    entries  map[string]*informerEntry
    capacity int            // default 10
    grace    time.Duration  // default 30s
}
```

- `Access(namespace string) SharedInformerFactory` — check existing → cancel grace if needed → evict oldest if full → create new factory → Start → WaitForCacheSync
- `Release(namespace string)` — start grace timer (30s), on timer fire: close stopCh + delete entry
- `evictOldest()` — find entry with oldest lastUsed → close stopCh → delete
- All-namespaces mode: separate global factory, not in LRU

### File 4: `internal/k8s/resources.go`

Implement `ResourceLister` using Informer Listers (zero API calls):
- `ListNamespaces()` — from namespace informer Lister
- `ListDeployments(ns)` — from deployment informer Lister
- `ListPods(ns)` — from pod informer Lister
- `ListPodsBySelector(ns, selector)` — filter pods by label match
- `FindDeploymentByName(ns, name)` — exact match from Lister
- `SearchResources(ns, query)` — use `sahilm/fuzzy` for fuzzy matching across deployments and pods

**Critical:** Convert client-go types (`v1.Pod`, `appsv1.Deployment`) to Korthex internal types (`k8s.Pod`, `k8s.Deployment`) before returning. Never expose client-go types.

### File 5: `internal/k8s/logs.go`

Implement `LogStreamer`:

**StreamLogs(ctx, req, ch):**
- Call `CoreV1().Pods(ns).GetLogs(pod, opts).Stream(ctx)` → `io.ReadCloser`
- Per-container goroutine: `bufio.Scanner` reads lines
- Buffered channel (cap=50), non-blocking send: `select { case ch <- line: default: /* drop */ }`
- Respect `ctx.Done()` → close reader, exit goroutine
- Exponential backoff retry on network errors (cenkalti/backoff): initial 500ms, max 15s, max 20 attempts
- Stop retry on Pod Succeeded/Failed/Deleted

**GetLogs(ctx, req):**
- Non-streaming version: collect all lines into `[]LogLine` and return

**StreamMultiPodLogs(ctx, ns, selector, opts, ch):**
- Use `ListPodsBySelector()` to find matching pods
- For each pod, for each container: start independent `StreamLogs` goroutine
- All goroutines write to the same `ch` (merged output)
- Use `sync.WaitGroup` to track completion

**grepPattern support:** if `opts` includes a grep pattern, filter lines client-side with `regexp.MatchString`

### File 6: `internal/k8s/events.go`

Implement `EventLister`:
- `ListEvents(ns)` — `CoreV1().Events(ns).List(ctx, metav1.ListOptions{})`
- `ListEventsForResource(ns, kind, name)` — filter by `fieldSelector`
- Convert to `k8s.Event` before returning

### File 7: `internal/k8s/describe.go`

Implement `ResourceDescriber`:
- `Describe(ns, kind, name)` — Get the resource + ListEventsForResource → assemble describe-like text output
- Support kinds: Namespace, Deployment, Pod
- Format similar to `kubectl describe` output

### File 8: `internal/k8s/client_test.go`

Tests using `client-go/kubernetes/fake`:
- ListNamespaces, ListDeployments, ListPods with prepopulated fake data
- ListPodsBySelector with label matching
- FindDeploymentByName: found and not-found cases
- SearchResources: fuzzy match "order-svc" finds "order-service"

### File 9: `internal/k8s/logs_test.go`

Tests using fake `io.ReadCloser`:
- StreamLogs: happy path (lines arrive in order)
- StreamLogs: error mid-stream (reader returns error)
- StreamLogs: context cancellation stops streaming
- StreamMultiPodLogs: multiple pods concurrent

## Step 3: Verify

```bash
go test -v -race ./internal/k8s/...
go vet ./internal/k8s/...
```

## Critical Rules

- **Only import `internal/config`** — never import llm, agent, or ui
- **client-go types don't leak**: all public methods return `k8s.Pod`, `k8s.Deployment`, etc. — never `v1.Pod`
- **Resource lists from Informer Cache**: `ListNamespaces/ListDeployments/ListPods` use Lister, zero API calls
- **Non-blocking channel send**: `select { case ch <- line: default: }` — producer never blocks
- **Per-container goroutine**: one slow container doesn't block others
- **Exponential backoff**: use `cenkalti/backoff/v4`, not bare retry loops
- **Pod state awareness**: stop retrying on Succeeded/Failed/Deleted pods
- **Use `slog` for debug logging**: `slog.Debug("informer cache synced", "namespace", ns)`, `slog.Warn("log stream reconnecting", "pod", podName, "err", err)`. Bubble Tea occupies stdout — slog writes to file (configured by app layer). See SPEC §6.8
