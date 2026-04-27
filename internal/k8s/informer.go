package k8s

import (
	"log/slog"
	"sync"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultCapacity    = 10
	defaultGracePeriod = 30 * time.Second
	defaultResync      = 10 * time.Minute
)

// informerEntry tracks a per-namespace SharedInformerFactory and its lifecycle.
type informerEntry struct {
	factory    informers.SharedInformerFactory
	stopCh     chan struct{}
	lastUsed   time.Time
	graceTimer *time.Timer // nil if actively in use
}

// InformerManager manages per-namespace SharedInformerFactories with LRU eviction
// and a grace period to avoid thrashing on rapid namespace switches.
type InformerManager struct {
	mu        sync.Mutex
	clientset kubernetes.Interface
	entries   map[string]*informerEntry
	capacity  int
	grace     time.Duration
}

// NewInformerManager creates an InformerManager with the given clientset and capacity.
func NewInformerManager(clientset kubernetes.Interface, capacity int) *InformerManager {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	return &InformerManager{
		clientset: clientset,
		entries:   make(map[string]*informerEntry),
		capacity:  capacity,
		grace:     defaultGracePeriod,
	}
}

// Access returns the SharedInformerFactory for the given namespace,
// creating one if it doesn't exist. If the namespace is in grace period,
// the timer is cancelled and the entry is promoted back to active.
func (m *InformerManager) Access(namespace string) informers.SharedInformerFactory {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Already exists → cancel grace if needed, update lastUsed, return.
	if entry, ok := m.entries[namespace]; ok {
		if entry.graceTimer != nil {
			entry.graceTimer.Stop()
			entry.graceTimer = nil
			slog.Debug("informer grace cancelled", "namespace", namespace)
		}
		entry.lastUsed = time.Now()
		return entry.factory
	}

	// 2. Evict oldest if at capacity.
	if len(m.entries) >= m.capacity {
		m.evictOldest()
	}

	// 3. Create new factory (not started yet — caller must register
	//    informers then call EnsureSynced).
	factory := informers.NewSharedInformerFactoryWithOptions(
		m.clientset, defaultResync,
		informers.WithNamespace(namespace),
	)
	stopCh := make(chan struct{})

	m.entries[namespace] = &informerEntry{
		factory:  factory,
		stopCh:   stopCh,
		lastUsed: time.Now(),
	}
	slog.Debug("informer created", "namespace", namespace, "total", len(m.entries))
	return factory
}

// EnsureSynced starts all registered informers for the given namespace
// and blocks until their caches are synced. Safe to call multiple times —
// already-running informers are not restarted.
func (m *InformerManager) EnsureSynced(namespace string) {
	m.mu.Lock()
	entry, ok := m.entries[namespace]
	m.mu.Unlock()
	if !ok {
		return
	}
	entry.factory.Start(entry.stopCh)
	entry.factory.WaitForCacheSync(entry.stopCh)
}

// Release starts the grace period for the given namespace. If the namespace
// is not re-accessed within the grace period, the factory is stopped and removed.
func (m *InformerManager) Release(namespace string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[namespace]
	if !ok {
		return
	}

	entry.graceTimer = time.AfterFunc(m.grace, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if e, ok := m.entries[namespace]; ok && e.graceTimer != nil {
			close(e.stopCh)
			delete(m.entries, namespace)
			slog.Debug("informer evicted after grace", "namespace", namespace)
		}
	})
	slog.Debug("informer grace started", "namespace", namespace, "grace", m.grace)
}

// evictOldest stops and removes the entry with the oldest lastUsed time.
// Must be called with m.mu held.
func (m *InformerManager) evictOldest() {
	var oldestNS string
	var oldestTime time.Time

	for ns, entry := range m.entries {
		if oldestNS == "" || entry.lastUsed.Before(oldestTime) {
			oldestNS = ns
			oldestTime = entry.lastUsed
		}
	}

	if oldestNS != "" {
		entry := m.entries[oldestNS]
		if entry.graceTimer != nil {
			entry.graceTimer.Stop()
		}
		close(entry.stopCh)
		delete(m.entries, oldestNS)
		slog.Debug("informer LRU evicted", "namespace", oldestNS)
	}
}

// StopAll stops all informer factories and clears the manager.
func (m *InformerManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for ns, entry := range m.entries {
		if entry.graceTimer != nil {
			entry.graceTimer.Stop()
		}
		close(entry.stopCh)
		slog.Debug("informer stopped", "namespace", ns)
	}
	m.entries = make(map[string]*informerEntry)
}

// Len returns the current number of cached informer factories.
func (m *InformerManager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}
