# internal/k8s — CLAUDE.md

> All Kubernetes cluster interaction: Informer/Cache for resources, GetLogs for log streaming, events, describe.

## Parent Reference

- **When confused about architecture:** Read [`../../CLAUDE.md`](../../CLAUDE.md) (root project rules, especially Rule #3: client-go 不泄漏)
- **When confused about interface design:** Read [`../../SPEC.md`](../../SPEC.md) Section 3.3 (K8s interfaces)
- **When confused about performance patterns:** Read [`../../SPEC.md`](../../SPEC.md) Section 4.3 (日志流管道)
- **When confused about performance requirements:** Read [`../../PRD.md`](../../PRD.md) Section 6.3 (Performance Architecture) 和 Section 9 (Non-Functional Requirements)
- **When confused about large cluster strategy:** Read [`../../PRD.md`](../../PRD.md) Section 6.3.5

## Module Rules

| # | Rule | Reason |
|---|------|--------|
| 1 | **Layer 1: 只能 import `internal/config`** | 与 llm 同层，禁止互相 import |
| 2 | **client-go 类型封装** — 所有返回给外部的类型必须是 `types.go` 中的 Korthex struct，禁止返回 `v1.Pod`/`v1.Deployment` 等 client-go 类型 | Root CLAUDE.md Rule #3: 隔离 client-go API 变更 |
| 3 | **资源列表从 Informer Cache 读** — ListNamespaces/ListDeployments/ListPods 必须从 Lister 读，零 API 调用 | 性能关键: 大集群下直接 API 调用会压垮 API Server |
| 4 | **日志流 Non-blocking** — channel send 必须使用 `select { case ch <- line: default: }` 模式 | 日志生产者永远不能阻塞, UI 慢了丢日志而非卡死 |
| 5 | **Per-container goroutine** — StreamMultiPodLogs 每个容器独立 goroutine | 一个慢容器不影响其他容器 |
| 6 | **指数退避重试** — 网络错误使用 cenkalti/backoff，不要裸重试 | 防止暴打 API Server |
| 7 | **Pod 状态感知** — Succeeded/Failed/Deleted 状态的 Pod 停止重试 | 不在死 Pod 上浪费资源 |
| 8 | **Resource Registry 模式** — 新增资源类型 (StatefulSet/DaemonSet/Job/CronJob) 通过 `ResourceAccessor` 注册到 `registry.go`。UI/Agent 通过 `AccessorFor()` 查询，新增资源类型不需要修改调用方代码 | k9s DAO Registry 模式借鉴, 可扩展性 |
| 9 | **Informer 按需启动** — 新资源类型的 Informer (AppsV1().StatefulSets(), BatchV1().Jobs() 等) 首次访问时启动，不在启动时创建。遵循现有 per-namespace LRU 模式 | 避免启动时创建未使用的 Informer, 节省内存和 API Server watch 连接 |

## Interfaces (defined in this module)

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
}
```

> Full interfaces (ResourceLister, LogStreamer, EventLister, ResourceDescriber) → [`README.md`](./README.md) | SPEC Section 3.3 → [`../../SPEC.md`](../../SPEC.md)

## Files

| File | Do | Don't |
|------|-----|-------|
| `client.go` | kubeconfig 加载, rest.Config, Connect/Disconnect | 不要在这里做资源查询 |
| `informer.go` | per-ns factory 管理, LRU 驱逐 (10 slots), 10min resync | 不要创建全局 informer (除 all-ns 模式) |
| `resources.go` | 从 Lister 读, fuzzy search (sahilm/fuzzy) | 不要直接调 API Server List() |
| `logs.go` | per-container goroutine, buffered channel (cap=50), non-blocking send | 不要阻塞 send, 不要无限 buffer |
| `events.go` | CoreV1().Events().List() | 简单查询, 不需要 Informer |
| `describe.go` | Get resource + List events → 拼接 describe 输出 | 不要调 kubectl 二进制 |
| `registry.go` | ResourceAccessor 接口, accessor 注册 map, AccessorFor() 查找, 5 种 accessor 实现 (Deployment/StatefulSet/DaemonSet/Job/CronJob) | 不要在 UI 或 Agent 中硬编码 switch-case |
| `types.go` | Korthex 内部类型定义 | 不要 import client-go 类型到这个文件的公共 API |

## Cross-Module Dependencies

| This module | → | Dependency | Via |
|-------------|---|-----------|-----|
| k8s | imports | config | `config.KubernetesConfig` |
| agent | imports | k8s | `k8s.Client` interface |
| ui | imports | k8s | `k8s.Client` interface (manual browsing) |

## Key Performance Patterns (from k9s)

```
Informer Watch Stream → In-memory Cache → Lister (zero API call) → TUI

Log API Stream → per-container goroutine → buffered channel (cap=50)
  → non-blocking send (drop on overflow) → Ring Buffer → UI Batched Flush
```

## Informer LRU + Grace Period Design (informer.go)

> SPEC §8.6 identifies this as MEDIUM risk. This section is the authoritative implementation spec.

### Data Structure

```go
type informerEntry struct {
    factory   informers.SharedInformerFactory
    stopCh    chan struct{}
    lastUsed  time.Time
    graceTimer *time.Timer  // nil if actively in use
}

type InformerManager struct {
    mu       sync.Mutex
    entries  map[string]*informerEntry  // key = namespace
    capacity int                         // default 10
    grace    time.Duration               // default 30s
    program  *tea.Program                // for p.Send() on resource updates
}
```

### Lifecycle State Machine (per namespace)

```
                 ┌──────────┐
    Access(ns) → │  Active   │ ← factory running, graceTimer=nil
                 └────┬─────┘
                      │ Switch to different ns
                      ▼
                 ┌──────────┐
                 │  Grace    │ ← graceTimer started (30s), factory still running
                 └────┬─────┘
                      │
              ┌───────┴───────┐
              │               │
         Timer fires     Access(ns) again
              │               │
              ▼               ▼
         ┌──────────┐   ┌──────────┐
         │  Evicted  │   │  Active   │ ← cancel timer, promote back
         └──────────┘   └──────────┘
         factory.Stop()
         delete from map
```

### Key Rules

| Rule | Why |
|------|-----|
| **Grace period = 30s** | 用户快速切换 ns 时不反复创建/销毁 factory |
| **Timer 由 InformerManager 持有** | 不是 informerEntry 自己管理, 避免并发问题 |
| **Access 时先检查 grace 状态** | 如果 ns 在 grace 中, cancel timer 并 promote 回 Active, 不重建 |
| **LRU 驱逐触发条件** | len(entries) > capacity 且无法通过 grace eviction 腾出空间 → 立即停止 lastUsed 最老的 |
| **factory.Stop() 是异步的** | 调 `close(stopCh)` 后 informer goroutine 会自行退出, 不阻塞调用方 |
| **all-namespaces 模式** | 使用单独的全局 factory, 不参与 LRU, 在用户切换到具体 ns 时 stop |

### Access() 伪代码

```
func (m *InformerManager) Access(namespace string) informers.SharedInformerFactory:
    m.mu.Lock(); defer m.mu.Unlock()

    // 1. 已存在且 Active → 更新 lastUsed, return
    if entry, ok := m.entries[namespace]; ok:
        if entry.graceTimer != nil:
            entry.graceTimer.Stop()  // cancel grace eviction
            entry.graceTimer = nil
        entry.lastUsed = time.Now()
        return entry.factory

    // 2. 需要腾出空间
    if len(m.entries) >= m.capacity:
        m.evictOldest()  // stop + delete lastUsed 最老的 entry

    // 3. 创建新 factory
    factory := informers.NewSharedInformerFactoryWithOptions(
        clientset, 10*time.Minute,
        informers.WithNamespace(namespace),
    )
    stopCh := make(chan struct{})
    factory.Start(stopCh)
    factory.WaitForCacheSync(stopCh)

    m.entries[namespace] = &informerEntry{
        factory: factory, stopCh: stopCh,
        lastUsed: time.Now(), graceTimer: nil,
    }
    return factory
```

### Release() 伪代码 (切换离开 ns 时调用)

```
func (m *InformerManager) Release(namespace string):
    m.mu.Lock(); defer m.mu.Unlock()

    entry, ok := m.entries[namespace]
    if !ok: return

    // 启动 grace timer, 30s 后如果没被重新 Access 就真正停止
    entry.graceTimer = time.AfterFunc(m.grace, func() {
        m.mu.Lock(); defer m.mu.Unlock()
        if e, ok := m.entries[namespace]; ok && e.graceTimer != nil:
            close(e.stopCh)
            delete(m.entries, namespace)
    })
```

## Testing Checklist

- [ ] Fake clientset: ListNamespaces, ListDeployments, ListPods
- [ ] Fake clientset: ListPodsBySelector with label matching
- [ ] Fake io.ReadCloser: StreamLogs happy path
- [ ] Fake io.ReadCloser: StreamLogs with error mid-stream
- [ ] StreamMultiPodLogs: multiple pods concurrent streaming
- [ ] Informer LRU eviction: access 11th namespace evicts oldest
- [ ] Informer grace period: release ns → access within 30s → no rebuild
- [ ] Informer grace period: release ns → wait 30s → factory stopped
- [ ] SearchResources: fuzzy match "order-svc" finds "order-service"
