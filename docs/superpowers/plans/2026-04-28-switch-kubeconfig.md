# Switch Kubeconfig Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add runtime kubeconfig/context switching via UI overlay, slash command, global hotkey, and AI agent tool.

**Architecture:** New `KubeSwitchModel` overlay in UI (Layer 3), kubeconfig discovery in config (Layer 0), hot-reconnect in k8s (Layer 1), `switch_kubeconfig` agent tool (Layer 2), orchestration in app (Layer 4). Chat history preserved on switch.

**Tech Stack:** Go 1.24, Bubble Tea, client-go clientcmd, sahilm/fuzzy

---

### Task 1: Config — Kubeconfig Discovery & Context Parsing

**Files:**
- Modify: `internal/config/config.go:82-97` (add types after existing KubeDetection)
- Modify: `internal/config/config_test.go` (add tests)

- [ ] **Step 1: Write failing tests for DiscoverKubeconfigs**

Add to `internal/config/config_test.go`:

```go
func TestDiscoverKubeconfigs(t *testing.T) {
	// Setup: create temp dir with fake kubeconfig files
	tmpDir := t.TempDir()
	kubeDir := filepath.Join(tmpDir, ".kube")
	if err := os.MkdirAll(kubeDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Write fake kubeconfig files
	fakeConfig := `apiVersion: v1
kind: Config
contexts:
- context:
    cluster: test
    user: test
  name: test-context
current-context: test-context
clusters:
- cluster:
    server: https://localhost:6443
  name: test
users:
- name: test
  user: {}
`
	os.WriteFile(filepath.Join(kubeDir, "config"), []byte(fakeConfig), 0600)
	os.WriteFile(filepath.Join(kubeDir, "config-staging"), []byte(fakeConfig), 0600)
	os.WriteFile(filepath.Join(kubeDir, "config.lock"), []byte("lock"), 0600) // should be excluded

	entries := DiscoverKubeconfigs(kubeDir, "")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	// Verify sorted by path
	if entries[0].Path > entries[1].Path {
		t.Errorf("entries not sorted: %s > %s", entries[0].Path, entries[1].Path)
	}
	// Verify context count
	if entries[0].ContextCount != 1 {
		t.Errorf("expected 1 context, got %d", entries[0].ContextCount)
	}
}

func TestDiscoverKubeconfigs_EnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "env-config")
	fakeConfig := `apiVersion: v1
kind: Config
contexts:
- context:
    cluster: env
    user: env
  name: env-ctx
current-context: env-ctx
clusters:
- cluster:
    server: https://env:6443
  name: env
users:
- name: env
  user: {}
`
	os.WriteFile(envPath, []byte(fakeConfig), 0600)

	entries := DiscoverKubeconfigs("", envPath)
	found := false
	for _, e := range entries {
		if e.Path == envPath {
			found = true
		}
	}
	if !found {
		t.Errorf("env var path %s not found in entries", envPath)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/mio/korthex && go test ./internal/config/... -v -run TestDiscoverKubeconfigs`
Expected: FAIL — `DiscoverKubeconfigs` undefined

- [ ] **Step 3: Implement DiscoverKubeconfigs and types**

Add to `internal/config/config.go` after line 97 (after `KubeDetection`):

```go
// KubeconfigEntry represents a discovered kubeconfig file.
type KubeconfigEntry struct {
	Path         string // absolute path
	ContextCount int    // number of contexts in this file
}

// ContextEntry represents a context within a kubeconfig file.
type ContextEntry struct {
	Name    string // context name
	Cluster string // associated cluster name
	User    string // associated user name
	Current bool   // is this the file's current-context
}

// DiscoverKubeconfigs scans for kubeconfig files.
// kubeDir is the directory to scan (e.g. ~/.kube); envKubeconfig is the $KUBECONFIG value.
// Either may be empty.
func DiscoverKubeconfigs(kubeDir string, envKubeconfig string) []KubeconfigEntry {
	seen := make(map[string]bool)
	var entries []KubeconfigEntry

	addPath := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		if seen[abs] {
			return
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return
		}
		seen[abs] = true
		ctxs, _ := ParseContexts(abs)
		entries = append(entries, KubeconfigEntry{Path: abs, ContextCount: len(ctxs)})
	}

	// 1. $KUBECONFIG paths
	if envKubeconfig != "" {
		for _, p := range filepath.SplitList(envKubeconfig) {
			if p != "" {
				addPath(p)
			}
		}
	}

	// 2. Scan kubeDir for config* files (exclude .lock, directories)
	if kubeDir != "" {
		dirEntries, err := os.ReadDir(kubeDir)
		if err == nil {
			for _, de := range dirEntries {
				if de.IsDir() {
					continue
				}
				name := de.Name()
				if !strings.HasPrefix(name, "config") {
					continue
				}
				if strings.HasSuffix(name, ".lock") {
					continue
				}
				addPath(filepath.Join(kubeDir, name))
			}
		}
	}

	// Sort by path
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries
}
```

Add `"sort"` to the imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/mio/korthex && go test ./internal/config/... -v -run TestDiscoverKubeconfigs`
Expected: PASS

- [ ] **Step 5: Write failing tests for ParseContexts**

Add to `internal/config/config_test.go`:

```go
func TestParseContexts(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config")
	content := `apiVersion: v1
kind: Config
contexts:
- context:
    cluster: prod-cluster
    user: admin
  name: prod
- context:
    cluster: staging-cluster
    user: dev
  name: staging
current-context: prod
clusters:
- cluster:
    server: https://prod:6443
  name: prod-cluster
- cluster:
    server: https://staging:6443
  name: staging-cluster
users:
- name: admin
  user: {}
- name: dev
  user: {}
`
	os.WriteFile(cfgPath, []byte(content), 0600)

	ctxs, err := ParseContexts(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctxs) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(ctxs))
	}

	// Verify prod is current
	var prodCtx *ContextEntry
	for i := range ctxs {
		if ctxs[i].Name == "prod" {
			prodCtx = &ctxs[i]
		}
	}
	if prodCtx == nil {
		t.Fatal("prod context not found")
	}
	if !prodCtx.Current {
		t.Error("prod should be current")
	}
	if prodCtx.Cluster != "prod-cluster" {
		t.Errorf("expected cluster prod-cluster, got %s", prodCtx.Cluster)
	}
}

func TestParseContexts_InvalidFile(t *testing.T) {
	_, err := ParseContexts("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `cd /Users/mio/korthex && go test ./internal/config/... -v -run TestParseContexts`
Expected: FAIL — `ParseContexts` undefined

- [ ] **Step 7: Implement ParseContexts**

Add to `internal/config/config.go`:

```go
// ParseContexts reads a kubeconfig file and returns its contexts.
func ParseContexts(kubeconfigPath string) ([]ContextEntry, error) {
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig: %w", err)
	}

	// Minimal YAML parsing — kubeconfig structure
	type kubeContext struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	}
	type kubeConfig struct {
		CurrentContext string        `yaml:"current-context"`
		Contexts       []kubeContext `yaml:"contexts"`
	}

	var kc kubeConfig
	if err := yaml.Unmarshal(data, &kc); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	entries := make([]ContextEntry, 0, len(kc.Contexts))
	for _, c := range kc.Contexts {
		entries = append(entries, ContextEntry{
			Name:    c.Name,
			Cluster: c.Context.Cluster,
			User:    c.Context.User,
			Current: c.Name == kc.CurrentContext,
		})
	}
	return entries, nil
}
```

Add `"gopkg.in/yaml.v3"` to imports. Check if already a dependency; if not, use `encoding/json` alternative or add dependency.

**Important:** config is Layer 0, so check if yaml.v3 is already available. If Viper already brings it in transitively, no action needed. Otherwise, use the minimal `gopkg.in/yaml.v3` (BSD license, compatible).

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd /Users/mio/korthex && go test ./internal/config/... -v -run TestParseContexts`
Expected: PASS

- [ ] **Step 9: Run full config test suite**

Run: `cd /Users/mio/korthex && go test ./internal/config/... -v -race`
Expected: All tests PASS

- [ ] **Step 10: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add DiscoverKubeconfigs and ParseContexts for runtime kubeconfig switching"
```

---

### Task 2: K8s — Reconnect and ContextInfo

**Files:**
- Modify: `internal/k8s/client.go:14-25` (Client interface)
- Modify: `internal/k8s/client.go:62-75` (k8sClient struct)
- Modify: `internal/k8s/client_test.go`

- [ ] **Step 1: Write failing test for Reconnect**

Add to `internal/k8s/client_test.go`:

```go
func TestReconnect_StoresKubeconfigPath(t *testing.T) {
	c := NewClient()
	kp, ctx := c.ContextInfo()
	if kp != "" || ctx != "" {
		t.Errorf("expected empty before connect, got kp=%q ctx=%q", kp, ctx)
	}
}

func TestContextInfo_AfterConnect(t *testing.T) {
	// This test verifies the ContextInfo method returns stored values.
	// Full Connect test requires real kubeconfig; we test the struct directly.
	c := &k8sClient{
		kubeconfigPath: "/test/path",
		context:        "test-ctx",
		connected:      true,
	}
	kp, ctx := c.ContextInfo()
	if kp != "/test/path" {
		t.Errorf("expected /test/path, got %s", kp)
	}
	if ctx != "test-ctx" {
		t.Errorf("expected test-ctx, got %s", ctx)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/mio/korthex && go test ./internal/k8s/... -v -run "TestReconnect_Stores|TestContextInfo_After"`
Expected: FAIL — `ContextInfo` undefined, `kubeconfigPath` undefined

- [ ] **Step 3: Add kubeconfigPath field and implement ContextInfo**

In `internal/k8s/client.go`, add `kubeconfigPath` to k8sClient struct (after line 67):

```go
type k8sClient struct {
	mu             sync.RWMutex
	clientset      kubernetes.Interface
	restConfig     *rest.Config
	context        string
	kubeconfigPath string
	connected      bool

	informers *InformerManager
	resources *resourceLister
	logs      *logStreamer
	events    *eventLister
	describer *resourceDescriber
}
```

Add to Client interface (after line 19):

```go
type Client interface {
	Connect(kubeconfig, context string) error
	Disconnect()
	Reconnect(kubeconfig, context string) error
	IsConnected() bool
	CurrentContext() string
	ContextInfo() (kubeconfig string, context string)

	Resources() ResourceLister
	Logs() LogStreamer
	Events() EventLister
	Describer() ResourceDescriber
}
```

Store kubeconfigPath in Connect (after line 121):

```go
c.kubeconfigPath = kubeconfig
```

Clear kubeconfigPath in Disconnect (after line 149):

```go
c.kubeconfigPath = ""
```

Add ContextInfo method:

```go
// ContextInfo returns the active kubeconfig path and context name.
func (c *k8sClient) ContextInfo() (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.kubeconfigPath, c.context
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/mio/korthex && go test ./internal/k8s/... -v -run "TestReconnect_Stores|TestContextInfo_After"`
Expected: PASS

- [ ] **Step 5: Implement Reconnect**

Add to `internal/k8s/client.go`:

```go
// Reconnect hot-swaps to a new kubeconfig+context.
// It validates the new connection before disconnecting the old one.
// On failure, the existing connection remains intact.
func (c *k8sClient) Reconnect(kubeconfig, ctx string) error {
	// Build new client config (without holding lock)
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if ctx != "" {
		overrides.CurrentContext = ctx
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("build rest config: %w", err)
	}

	newClientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create clientset: %w", err)
	}

	// Validate new connection
	_, err = newClientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("validate connection: %w", err)
	}

	// Resolve context name
	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		return fmt.Errorf("load raw config: %w", err)
	}
	resolvedContext := rawConfig.CurrentContext
	if ctx != "" {
		resolvedContext = ctx
	}

	// Lock and swap
	c.mu.Lock()
	defer c.mu.Unlock()

	// Teardown old
	if c.informers != nil {
		c.informers.StopAll()
	}
	c.clientset = nil
	c.restConfig = nil
	c.informers = nil
	c.resources = nil
	c.logs = nil
	c.events = nil
	c.describer = nil

	// Setup new
	c.clientset = newClientset
	c.restConfig = restConfig
	c.context = resolvedContext
	c.kubeconfigPath = kubeconfig
	c.connected = true

	c.informers = NewInformerManager(newClientset, 10)
	c.resources = &resourceLister{clientset: newClientset, informers: c.informers}
	c.logs = &logStreamer{clientset: newClientset, resources: c.resources}
	c.events = &eventLister{clientset: newClientset}
	c.describer = &resourceDescriber{clientset: newClientset, events: c.events}

	slog.Debug("k8s reconnected", "context", resolvedContext, "kubeconfig", kubeconfig)
	return nil
}
```

- [ ] **Step 6: Fix compile errors — update mock if exists**

Check for any mock files that implement Client interface and add the new methods:

```go
func (m *MockClient) Reconnect(kubeconfig, context string) error { return nil }
func (m *MockClient) ContextInfo() (string, string) { return "", "" }
```

- [ ] **Step 7: Run full k8s test suite**

Run: `cd /Users/mio/korthex && go test ./internal/k8s/... -v -race`
Expected: All tests PASS

- [ ] **Step 8: Commit**

```bash
git add internal/k8s/client.go internal/k8s/client_test.go
git commit -m "feat(k8s): add Reconnect and ContextInfo for runtime cluster switching"
```

---

### Task 3: Agent — switch_kubeconfig Tool

**Files:**
- Modify: `internal/agent/agent.go:49-60` (add EventKubeSwitch)
- Modify: `internal/agent/tools.go:32-199` (add tool definition)
- Modify: `internal/agent/tools.go:202-248` (add dispatch case)
- Modify: `internal/agent/safety.go:11-33` (add to whitelist)
- Modify: `internal/agent/prompt.go` (add cluster switching guidance)

- [ ] **Step 1: Add EventKubeSwitch event type**

In `internal/agent/agent.go`, add after line 59:

```go
const (
	EventStreamDelta AgentEventType = iota
	EventToolCall
	EventToolResult
	EventLogsReady
	EventSummary
	EventError
	EventComplete
	EventKubeSwitch // Agent requests kubeconfig/context switch
)
```

Add fields to AgentEvent struct (after line 46):

```go
type AgentEvent struct {
	Type      AgentEventType
	Iteration int
	MaxIter   int

	Text           string
	ToolName       string
	ToolArgs       map[string]string
	ToolResult     string
	CommandDisplay string
	LogLines       []k8s.LogLine
	StreamDelta    string

	// EventKubeSwitch fields
	SwitchKubeconfig string // kubeconfig path (empty = keep current)
	SwitchContext    string // target context name
}
```

- [ ] **Step 2: Add tool definition**

In `internal/agent/tools.go`, add to `ToolDefinitions()` return slice (before the closing `}`):

```go
		{
			Name:        "switch_kubeconfig",
			Description: "Switch to a different Kubernetes cluster by changing kubeconfig file and/or context. Use when user asks to switch cluster, change context, or connect to a different environment (e.g. 'switch to staging', 'connect to prod cluster'). Returns available contexts for confirmation.",
			Parameters: []llm.ParameterDef{
				{Name: "kubeconfig", Type: "string", Description: "Path to kubeconfig file. Leave empty to keep current file.", Required: false},
				{Name: "context", Type: "string", Description: "Context name to switch to.", Required: true},
			},
		},
```

- [ ] **Step 3: Add dispatch case and handler**

In `internal/agent/tools.go` `ExecuteTool`, add case before `default`:

```go
	case "switch_kubeconfig":
		return t.switchKubeconfig(args)
```

Add handler function:

```go
// switchKubeconfig validates and returns info about a kubeconfig switch request.
// The actual switch is performed by the UI layer via EventKubeSwitch.
func (t *toolExecutor) switchKubeconfig(args map[string]string) (string, []k8s.LogLine, error) {
	ctx := args["context"]
	if ctx == "" {
		return "ERROR: context parameter is required", nil, nil
	}

	kubeconfig := args["kubeconfig"]
	if kubeconfig == "" {
		// Use current kubeconfig
		kp, _ := t.k8sClient.ContextInfo()
		kubeconfig = kp
	}

	if kubeconfig == "" {
		return "ERROR: no kubeconfig path available. Please specify the kubeconfig parameter.", nil, nil
	}

	// Validate context exists in the file
	ctxs, err := config.ParseContexts(kubeconfig)
	if err != nil {
		return fmt.Sprintf("ERROR: cannot read kubeconfig %s: %v", kubeconfig, err), nil, nil
	}

	found := false
	for _, c := range ctxs {
		if c.Name == ctx {
			found = true
			break
		}
	}
	if !found {
		available := make([]string, 0, len(ctxs))
		for _, c := range ctxs {
			available = append(available, c.Name)
		}
		return fmt.Sprintf("ERROR: context %q not found in %s. Available contexts: %s", ctx, kubeconfig, strings.Join(available, ", ")), nil, nil
	}

	return fmt.Sprintf("Switching to context %q (kubeconfig: %s). The UI will perform the connection switch.", ctx, kubeconfig), nil, nil
}
```

Add `"github.com/Orwell-Yu/korthex/internal/config"` to tools.go imports.

- [ ] **Step 4: Add GenerateCommandDisplay case**

In `internal/agent/tools.go` `GenerateCommandDisplay`, add case:

```go
	case "switch_kubeconfig":
		cmd := "kubectl config use-context " + args["context"]
		if kc := args["kubeconfig"]; kc != "" {
			cmd += " --kubeconfig=" + kc
		}
		return cmd
```

- [ ] **Step 5: Add to safety whitelist**

In `internal/agent/safety.go`, add to whitelist map:

```go
			"switch_kubeconfig":       SafetyAllowed,
```

- [ ] **Step 6: Add system prompt guidance**

In `internal/agent/prompt.go`, add a section after the cluster context section:

```go
	// Cluster switching
	b.WriteString("## Cluster Switching\n")
	b.WriteString("When the user asks to switch clusters, change context, or connect to a different environment, ")
	b.WriteString("use the `switch_kubeconfig` tool. You need to know the target context name. ")
	b.WriteString("If the user is vague (e.g. 'switch to staging'), infer the context name from the current cluster context or ask for clarification.\n\n")
```

- [ ] **Step 7: Handle EventKubeSwitch in agent_impl.go**

In `internal/agent/agent_impl.go`, in the tool execution section, after the existing tool call event emission, add special handling for switch_kubeconfig:

Find where `EventToolCall` is emitted and after the tool is executed, add:

```go
		// Special: switch_kubeconfig emits EventKubeSwitch for UI to perform the actual switch
		if call.Name == "switch_kubeconfig" && err == nil {
			ch <- AgentEvent{
				Type:             EventKubeSwitch,
				SwitchKubeconfig: call.Args["kubeconfig"],
				SwitchContext:    call.Args["context"],
			}
		}
```

- [ ] **Step 8: Run compile check**

Run: `cd /Users/mio/korthex && go build ./...`
Expected: No compile errors

- [ ] **Step 9: Run agent tests**

Run: `cd /Users/mio/korthex && go test ./internal/agent/... -v -race`
Expected: All tests PASS

- [ ] **Step 10: Commit**

```bash
git add internal/agent/agent.go internal/agent/tools.go internal/agent/safety.go internal/agent/prompt.go internal/agent/agent_impl.go
git commit -m "feat(agent): add switch_kubeconfig tool for AI-driven cluster switching"
```

---

### Task 4: UI — Message Types and Panel Reset Methods

**Files:**
- Modify: `internal/ui/messages.go:90-99` (add new message types)
- Modify: `internal/ui/resource.go` (add Reset method)
- Modify: `internal/ui/logviewer.go` (add Reset method)

- [ ] **Step 1: Add new message types**

In `internal/ui/messages.go`, add after `showHelpMsg`:

```go
// showKubeSwitchMsg triggers the KubeSwitch overlay.
type showKubeSwitchMsg struct{}

// kubeSwitchExecuteMsg is sent when user confirms kubeconfig+context selection.
type kubeSwitchExecuteMsg struct {
	Kubeconfig string
	Context    string
}

// kubeSwitchCompleteMsg carries the result of a kubeconfig switch attempt.
type kubeSwitchCompleteMsg struct {
	ContextName string // new context name on success
	Err         error  // non-nil on failure
}
```

- [ ] **Step 2: Add ResourceModel.Reset()**

In `internal/ui/resource.go`, add method:

```go
// Reset clears resource state for cluster switch. Returns a tea.Cmd to reload namespaces.
func (m *ResourceModel) Reset() tea.Cmd {
	m.level = levelNamespace
	m.cursor = 0
	m.namespaces = nil
	m.deployments = nil
	m.pods = nil
	m.containers = nil
	m.resourceItems = nil
	m.selectedNS = ""
	m.selectedDeploy = ""
	m.selectedPod = ""
	m.resourceKind = k8s.KindDeployment
	m.searchMode = false
	m.searchQuery = ""
	m.describeResult = ""
	m.pendingHighlight = ""
	return loadNamespaces(m.k8sClient)
}
```

- [ ] **Step 3: Add LogViewerModel.Reset()**

In `internal/ui/logviewer.go`, add method:

```go
// Reset clears log state for cluster switch.
func (m *LogViewerModel) Reset() {
	// Cancel existing log stream
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
	// Clear buffer
	m.buffer.Reset()
	// Clear viewport state
	m.lines = nil
	m.lineToBuffer = nil
	m.scrollOff = 0
	m.scrollX = 0
	m.follow = true
	// Clear search/filter
	m.clearSearch()
	m.clearFilter()
	// Clear bookmarks
	m.bookmarks.Clear()
}
```

Check if `RingBuffer.Reset()` exists. If not, add it:

In `internal/ui/ringbuffer.go`:

```go
// Reset clears all entries from the buffer.
func (r *RingBuffer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = r.entries[:0]
	r.totalWritten = 0
}
```

Check if `BookmarkManager.Clear()` exists. If not, add it to `internal/ui/bookmark.go`:

```go
// Clear removes all bookmarks.
func (bm *BookmarkManager) Clear() {
	bm.entries = nil
}
```

- [ ] **Step 4: Run compile check**

Run: `cd /Users/mio/korthex && go build ./internal/ui/...`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages.go internal/ui/resource.go internal/ui/logviewer.go internal/ui/ringbuffer.go internal/ui/bookmark.go
git commit -m "feat(ui): add message types and panel Reset methods for kubeconfig switching"
```

---

### Task 5: UI — KubeSwitchModel Overlay

**Files:**
- Create: `internal/ui/kubeswitch.go`

- [ ] **Step 1: Create KubeSwitchModel**

Create `internal/ui/kubeswitch.go`:

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/Orwell-Yu/korthex/internal/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

type kubeSwitchStep int

const (
	stepSelectKubeconfig kubeSwitchStep = iota
	stepSelectContext
	stepConnecting
	stepError
)

// KubeSwitchModel is the overlay for switching kubeconfig file and context.
type KubeSwitchModel struct {
	visible bool
	step    kubeSwitchStep

	// Kubeconfig selection
	kubeconfigs    []config.KubeconfigEntry
	kubeCursor     int
	kubeFiltered   []int // indices into kubeconfigs after fuzzy filter

	// Context selection
	contexts       []config.ContextEntry
	ctxCursor      int
	ctxFiltered    []int // indices into contexts after fuzzy filter

	// Search
	searchMode     bool
	searchQuery    string

	// Current state (for marking "current")
	currentKubeconfig string
	currentContext    string

	// Selected kubeconfig path (for context loading)
	selectedKubeconfig string

	// Error message
	errMsg string

	// Dimensions
	width  int
	height int
	theme  Theme
}

// NewKubeSwitchModel creates a KubeSwitch overlay.
func NewKubeSwitchModel(theme Theme) KubeSwitchModel {
	return KubeSwitchModel{theme: theme}
}

// Show opens the overlay and populates kubeconfig list.
func (m *KubeSwitchModel) Show(currentKubeconfig, currentContext, kubeDir, envKubeconfig string) {
	m.visible = true
	m.step = stepSelectKubeconfig
	m.currentKubeconfig = currentKubeconfig
	m.currentContext = currentContext
	m.searchMode = false
	m.searchQuery = ""
	m.errMsg = ""

	m.kubeconfigs = config.DiscoverKubeconfigs(kubeDir, envKubeconfig)
	m.kubeCursor = 0
	m.kubeFiltered = allIndices(len(m.kubeconfigs))

	// If only one kubeconfig, skip to context selection
	if len(m.kubeconfigs) == 1 {
		m.selectedKubeconfig = m.kubeconfigs[0].Path
		m.loadContexts()
		m.step = stepSelectContext
	}
}

// Visible returns whether the overlay is visible.
func (m KubeSwitchModel) Visible() bool {
	return m.visible
}

// Close hides the overlay.
func (m *KubeSwitchModel) Close() {
	m.visible = false
}

// ShowError displays an error in the overlay.
func (m *KubeSwitchModel) ShowError(err error) {
	m.step = stepError
	m.errMsg = err.Error()
}

// SetConnecting shows the connecting spinner state.
func (m *KubeSwitchModel) SetConnecting() {
	m.step = stepConnecting
}

// SetDimensions updates the overlay size.
func (m *KubeSwitchModel) SetDimensions(w, h int) {
	m.width = w
	m.height = h
}

func (m *KubeSwitchModel) loadContexts() {
	ctxs, err := config.ParseContexts(m.selectedKubeconfig)
	if err != nil {
		m.step = stepError
		m.errMsg = fmt.Sprintf("Failed to parse kubeconfig: %v", err)
		return
	}
	m.contexts = ctxs
	m.ctxCursor = 0
	m.ctxFiltered = allIndices(len(m.contexts))
	m.searchMode = false
	m.searchQuery = ""
}

// Update handles keyboard events for the overlay.
func (m KubeSwitchModel) Update(msg tea.Msg) (KubeSwitchModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.step {
		case stepSelectKubeconfig:
			return m.updateKubeconfigSelect(msg)
		case stepSelectContext:
			return m.updateContextSelect(msg)
		case stepError:
			return m.updateError(msg)
		case stepConnecting:
			// No interaction during connecting
			return m, nil
		}
	}
	return m, nil
}

func (m KubeSwitchModel) updateKubeconfigSelect(msg tea.KeyMsg) (KubeSwitchModel, tea.Cmd) {
	if m.searchMode {
		return m.updateSearch(msg, true)
	}

	switch msg.String() {
	case "j", "down":
		if m.kubeCursor < len(m.kubeFiltered)-1 {
			m.kubeCursor++
		}
	case "k", "up":
		if m.kubeCursor > 0 {
			m.kubeCursor--
		}
	case "enter":
		if len(m.kubeFiltered) > 0 {
			idx := m.kubeFiltered[m.kubeCursor]
			m.selectedKubeconfig = m.kubeconfigs[idx].Path
			m.loadContexts()
			if m.step != stepError {
				m.step = stepSelectContext
			}
		}
	case "esc":
		m.Close()
	case "/":
		m.searchMode = true
		m.searchQuery = ""
	}
	return m, nil
}

func (m KubeSwitchModel) updateContextSelect(msg tea.KeyMsg) (KubeSwitchModel, tea.Cmd) {
	if m.searchMode {
		return m.updateSearch(msg, false)
	}

	switch msg.String() {
	case "j", "down":
		if m.ctxCursor < len(m.ctxFiltered)-1 {
			m.ctxCursor++
		}
	case "k", "up":
		if m.ctxCursor > 0 {
			m.ctxCursor--
		}
	case "enter":
		if len(m.ctxFiltered) > 0 {
			idx := m.ctxFiltered[m.ctxCursor]
			ctxName := m.contexts[idx].Name
			m.step = stepConnecting
			return m, func() tea.Msg {
				return kubeSwitchExecuteMsg{
					Kubeconfig: m.selectedKubeconfig,
					Context:    ctxName,
				}
			}
		}
	case "esc", "backspace":
		// Go back to kubeconfig selection (if we didn't auto-skip)
		if len(m.kubeconfigs) > 1 {
			m.step = stepSelectKubeconfig
			m.searchMode = false
			m.searchQuery = ""
		} else {
			m.Close()
		}
	case "/":
		m.searchMode = true
		m.searchQuery = ""
	}
	return m, nil
}

func (m KubeSwitchModel) updateError(msg tea.KeyMsg) (KubeSwitchModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Retry: go back to kubeconfig selection
		m.step = stepSelectKubeconfig
		m.errMsg = ""
	case "esc":
		m.Close()
	}
	return m, nil
}

func (m KubeSwitchModel) updateSearch(msg tea.KeyMsg, isKube bool) (KubeSwitchModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchQuery = ""
		if isKube {
			m.kubeFiltered = allIndices(len(m.kubeconfigs))
			m.kubeCursor = 0
		} else {
			m.ctxFiltered = allIndices(len(m.contexts))
			m.ctxCursor = 0
		}
	case "enter":
		m.searchMode = false
	case "backspace":
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
			m.applyFilter(isKube)
		}
	default:
		if len(msg.Runes) > 0 {
			m.searchQuery += string(msg.Runes)
			m.applyFilter(isKube)
		}
	}
	return m, nil
}

func (m *KubeSwitchModel) applyFilter(isKube bool) {
	if isKube {
		if m.searchQuery == "" {
			m.kubeFiltered = allIndices(len(m.kubeconfigs))
		} else {
			names := make([]string, len(m.kubeconfigs))
			for i, kc := range m.kubeconfigs {
				names[i] = kc.Path
			}
			matches := fuzzy.Find(m.searchQuery, names)
			m.kubeFiltered = make([]int, len(matches))
			for i, match := range matches {
				m.kubeFiltered[i] = match.Index
			}
		}
		m.kubeCursor = 0
	} else {
		if m.searchQuery == "" {
			m.ctxFiltered = allIndices(len(m.contexts))
		} else {
			names := make([]string, len(m.contexts))
			for i, c := range m.contexts {
				names[i] = c.Name
			}
			matches := fuzzy.Find(m.searchQuery, names)
			m.ctxFiltered = make([]int, len(matches))
			for i, match := range matches {
				m.ctxFiltered[i] = match.Index
			}
		}
		m.ctxCursor = 0
	}
}

// View renders the overlay.
func (m KubeSwitchModel) View(width, height int) string {
	overlayW := min(width*60/100, 80)
	overlayH := min(height*60/100, 20)
	if overlayW < 30 {
		overlayW = 30
	}
	if overlayH < 8 {
		overlayH = 8
	}

	innerW := overlayW - 4 // padding
	innerH := overlayH - 4

	var content string
	switch m.step {
	case stepSelectKubeconfig:
		content = m.renderKubeconfigList(innerW, innerH)
	case stepSelectContext:
		content = m.renderContextList(innerW, innerH)
	case stepConnecting:
		content = m.renderConnecting(innerW)
	case stepError:
		content = m.renderError(innerW, innerH)
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.ActiveBorder.GetBorderBottomForeground()).
		Padding(1).
		Width(overlayW).
		Height(overlayH).
		Render(content)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func (m KubeSwitchModel) renderKubeconfigList(w, h int) string {
	var b strings.Builder
	title := m.theme.Title.Render("Switch Kubeconfig [1/2]")
	b.WriteString(title + "\n\n")

	maxItems := h - 4 // title + footer
	if m.searchMode {
		maxItems-- // search bar
	}

	for i, idx := range m.kubeFiltered {
		if i >= maxItems {
			break
		}
		kc := m.kubeconfigs[idx]
		prefix := "  "
		if i == m.kubeCursor {
			prefix = m.theme.Selected.Render("> ")
		}
		name := kc.Path
		suffix := fmt.Sprintf(" (%d contexts)", kc.ContextCount)
		if kc.Path == m.currentKubeconfig {
			suffix += " (current)"
		}
		line := prefix + name + suffix
		if i == m.kubeCursor {
			line = m.theme.Selected.Render("> " + name + suffix)
		}
		b.WriteString(line + "\n")
	}

	if m.searchMode {
		b.WriteString("\n/" + m.searchQuery + "█")
	}
	b.WriteString("\n" + m.theme.Status.Render("j/k navigate · Enter select · / search · Esc cancel"))
	return b.String()
}

func (m KubeSwitchModel) renderContextList(w, h int) string {
	var b strings.Builder
	title := m.theme.Title.Render("Select Context [2/2]")
	b.WriteString(title + "\n\n")

	maxItems := h - 4
	if m.searchMode {
		maxItems--
	}

	for i, idx := range m.ctxFiltered {
		if i >= maxItems {
			break
		}
		c := m.contexts[idx]
		prefix := "  "
		name := c.Name
		suffix := ""
		if c.Cluster != "" {
			suffix = fmt.Sprintf(" (cluster: %s)", c.Cluster)
		}
		if c.Name == m.currentContext && m.selectedKubeconfig == m.currentKubeconfig {
			suffix += " (current)"
		}
		if i == m.ctxCursor {
			b.WriteString(m.theme.Selected.Render("> "+name+suffix) + "\n")
		} else {
			b.WriteString(prefix + name + suffix + "\n")
		}
	}

	if m.searchMode {
		b.WriteString("\n/" + m.searchQuery + "█")
	}
	b.WriteString("\n" + m.theme.Status.Render("j/k navigate · Enter switch · / search · Esc back"))
	return b.String()
}

func (m KubeSwitchModel) renderConnecting(w int) string {
	return lipgloss.Place(w, 3, lipgloss.Center, lipgloss.Center,
		m.theme.Title.Render("Connecting..."))
}

func (m KubeSwitchModel) renderError(w, h int) string {
	var b strings.Builder
	b.WriteString(m.theme.Error.Render("Connection Failed") + "\n\n")
	b.WriteString(m.errMsg + "\n\n")
	b.WriteString(m.theme.Status.Render("Enter retry · Esc cancel"))
	return b.String()
}

func allIndices(n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	return idx
}
```

- [ ] **Step 2: Run compile check**

Run: `cd /Users/mio/korthex && go build ./internal/ui/...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/ui/kubeswitch.go
git commit -m "feat(ui): add KubeSwitchModel overlay for kubeconfig/context selection"
```

---

### Task 6: UI — Wire Overlay into AppModel + Slash Command + Hotkey

**Files:**
- Modify: `internal/ui/app.go:20-41` (add kubeSwitch field)
- Modify: `internal/ui/app.go:44-80` (initialize in NewAppModel)
- Modify: `internal/ui/app.go:100-401` (routing)
- Modify: `internal/ui/chat.go:30-34` (add /kubeconfig command)
- Modify: `internal/ui/chat.go:346-364` (dispatch command)
- Modify: `internal/ui/help.go:56-115` (add Ctrl+K help entry)

- [ ] **Step 1: Add kubeSwitch to AppModel struct**

In `internal/ui/app.go`, add field to AppModel:

```go
type AppModel struct {
	resource      ResourceModel
	logviewer     LogViewerModel
	chat          ChatModel
	statusbar     StatusBarModel
	help          HelpModel
	podDetail     PodDetailModel
	historySearch HistorySearchModel
	kubeSwitch    KubeSwitchModel

	logBuffer *RingBuffer
	// ... rest unchanged
}
```

In `NewAppModel`, add initialization:

```go
	return AppModel{
		resource:  NewResourceModel(k, theme),
		// ... existing fields ...
		kubeSwitch:    NewKubeSwitchModel(theme),
		// ... rest unchanged
	}
```

- [ ] **Step 2: Add /kubeconfig slash command**

In `internal/ui/chat.go`, add to `slashCommands`:

```go
var slashCommands = []slashCommand{
	{Name: "history", Description: "Search conversation history"},
	{Name: "clear", Description: "Clear chat messages"},
	{Name: "help", Description: "Show keyboard shortcuts"},
	{Name: "kubeconfig", Description: "Switch kubeconfig / context"},
}
```

In `executeSlashCommand`, add case:

```go
	case "kubeconfig":
		return m, func() tea.Msg { return showKubeSwitchMsg{} }
```

- [ ] **Step 3: Add overlay routing in AppModel.Update**

In `internal/ui/app.go`, in the KeyMsg handler, add kubeSwitch overlay intercept **after** help overlay check and **before** history search:

```go
		// If kubeSwitch overlay is visible, route all keys there
		if m.kubeSwitch.Visible() {
			var cmd tea.Cmd
			m.kubeSwitch, cmd = m.kubeSwitch.Update(msg)
			return m, cmd
		}
```

Add `Ctrl+K` to global hotkeys (in the `switch msg.String()` block, after `"?"` case):

```go
		case "ctrl+k":
			// Don't open during agent execution
			if !m.chat.isRunning {
				kp, ctx := m.k8sClient.ContextInfo()
				kubeDir := filepath.Join(homeDir(), ".kube")
				envKube := os.Getenv("KUBECONFIG")
				m.kubeSwitch.Show(kp, ctx, kubeDir, envKube)
			}
			return m, nil
```

Add imports for `"os"` and `"path/filepath"` if not present.

Add a `homeDir` helper (or reuse from config — but ui can't import config directly for this. Better to inline):

```go
func kubeHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(home, ".kube")
}
```

- [ ] **Step 4: Handle showKubeSwitchMsg**

In `AppModel.Update`, add case in the custom messages section:

```go
	case showKubeSwitchMsg:
		if !m.chat.isRunning {
			kp, ctx := m.k8sClient.ContextInfo()
			envKube := os.Getenv("KUBECONFIG")
			m.kubeSwitch.Show(kp, ctx, kubeHomeDir(), envKube)
		}
		return m, nil
```

- [ ] **Step 5: Handle kubeSwitchExecuteMsg — perform reconnect**

```go
	case kubeSwitchExecuteMsg:
		// Cancel running agent if any
		if m.chat.isRunning && m.chat.cancelFunc != nil {
			m.chat.cancelFunc()
			m.chat.isRunning = false
			m.chat.cancelFunc = nil
		}
		m.kubeSwitch.SetConnecting()
		return m, func() tea.Msg {
			if err := m.k8sClient.Reconnect(msg.Kubeconfig, msg.Context); err != nil {
				return kubeSwitchCompleteMsg{Err: err}
			}
			return kubeSwitchCompleteMsg{ContextName: msg.Context}
		}
```

- [ ] **Step 6: Handle kubeSwitchCompleteMsg — reset panels**

```go
	case kubeSwitchCompleteMsg:
		if msg.Err != nil {
			m.kubeSwitch.ShowError(msg.Err)
			return m, nil
		}
		// Success: close overlay, reset panels
		m.kubeSwitch.Close()

		// Reset resource browser and log viewer (Chat preserved)
		reloadCmd := m.resource.Reset()
		m.logviewer.Reset()

		// Update status bar
		m.statusbar.SetContext(msg.ContextName)
		m.statusbar.SetNamespace("")

		// Update agent cluster context
		m.agent.SetClusterContext(agent.ClusterContext{ContextName: msg.ContextName})

		// Ensure full layout
		m.layout = LayoutFull
		m.statusbar.SetLayout(m.layout)

		// Add status message to chat
		m.chat.addMessage(ChatMessage{
			Role:    "status",
			Content: fmt.Sprintf("Switched to context: %s", msg.ContextName),
		})

		return m, reloadCmd
```

- [ ] **Step 7: Handle EventKubeSwitch from agent**

In the `AgentEventMsg` handler section, add:

```go
		// If agent triggers kubeconfig switch
		if msg.Type == agent.EventKubeSwitch && msg.SwitchContext != "" {
			kp := msg.SwitchKubeconfig
			if kp == "" {
				kp2, _ := m.k8sClient.ContextInfo()
				kp = kp2
			}
			return m, func() tea.Msg {
				return kubeSwitchExecuteMsg{
					Kubeconfig: kp,
					Context:    msg.SwitchContext,
				}
			}
		}
```

- [ ] **Step 8: Add overlay rendering in View**

In `AppModel.View()`, add kubeSwitch overlay rendering before help overlay check:

```go
	// KubeSwitch overlay takes precedence
	if m.kubeSwitch.Visible() {
		return m.kubeSwitch.View(m.width, m.height)
	}
```

- [ ] **Step 9: Add Ctrl+K to help overlay**

In `internal/ui/help.go`, add to the Global section:

```go
		{"Ctrl+K", "Switch kubeconfig/context"},
```

- [ ] **Step 10: Run compile check**

Run: `cd /Users/mio/korthex && go build ./...`
Expected: No errors

- [ ] **Step 11: Run full test suite**

Run: `cd /Users/mio/korthex && go test ./... -race`
Expected: All tests PASS

- [ ] **Step 12: Commit**

```bash
git add internal/ui/app.go internal/ui/chat.go internal/ui/help.go
git commit -m "feat(ui): wire KubeSwitch overlay with Ctrl+K hotkey, /kubeconfig command, and agent EventKubeSwitch"
```

---

### Task 7: App — Config Manager Injection for Persistence

**Files:**
- Modify: `internal/app/app.go:28-36` (add configManager field)
- Modify: `internal/app/app.go:40-142` (inject manager)

- [ ] **Step 1: Add configManager to App struct**

In `internal/app/app.go`:

```go
type App struct {
	config        *config.Config
	configManager config.Manager
	k8sClient     k8s.Client
	llmProvider   llm.Provider
	agent         agent.Agent
	logFile       *os.File
	historyStore  history.Store
	sessionID     string
}
```

- [ ] **Step 2: Update New() to accept and store Manager**

Change `New` signature:

```go
func New(cfg *config.Config, cfgManager config.Manager) (*App, error) {
```

Store it in the returned struct:

```go
	return &App{
		config:        cfg,
		configManager: cfgManager,
		// ... rest unchanged
	}
```

- [ ] **Step 3: Pass config manager to UI for persistence**

In `Run()`, pass manager:

```go
	appModel := ui.NewAppModel(a.agent, a.k8sClient, a.config, a.historyStore, a.configManager)
```

Update `ui.NewAppModel` signature to accept it:

In `internal/ui/app.go`:

```go
func NewAppModel(a agent.Agent, k k8s.Client, cfg *config.Config, historyStore history.Store, cfgManager config.Manager) AppModel {
```

Add field to AppModel:

```go
	configManager config.Manager
```

Store it in the constructor and use it in `kubeSwitchCompleteMsg` handler to persist:

```go
	case kubeSwitchCompleteMsg:
		if msg.Err != nil {
			m.kubeSwitch.ShowError(msg.Err)
			return m, nil
		}
		m.kubeSwitch.Close()

		// Persist new kubeconfig to config.yaml
		if m.configManager != nil {
			m.config.Kubernetes.Kubeconfig = msg.Kubeconfig
			m.config.Kubernetes.DefaultContext = msg.ContextName
			_ = m.configManager.Save(m.config) // best-effort persist
		}
		// ... rest of reset logic unchanged
```

Wait — `kubeSwitchCompleteMsg` doesn't carry the kubeconfig path. Fix: add it.

Update `kubeSwitchCompleteMsg`:

```go
type kubeSwitchCompleteMsg struct {
	Kubeconfig  string // kubeconfig path used
	ContextName string // new context name on success
	Err         error  // non-nil on failure
}
```

Update the `kubeSwitchExecuteMsg` handler to pass kubeconfig:

```go
		return m, func() tea.Msg {
			if err := m.k8sClient.Reconnect(msg.Kubeconfig, msg.Context); err != nil {
				return kubeSwitchCompleteMsg{Err: err}
			}
			return kubeSwitchCompleteMsg{Kubeconfig: msg.Kubeconfig, ContextName: msg.Context}
		}
```

- [ ] **Step 4: Update cmd/korthex/main.go to pass Manager**

In `cmd/korthex/main.go`, change the App creation call:

```go
	a, err := app.New(cfg, mgr)
```

- [ ] **Step 5: Run compile check**

Run: `cd /Users/mio/korthex && go build ./...`
Expected: No errors

- [ ] **Step 6: Run full test suite**

Run: `cd /Users/mio/korthex && go test ./... -race`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/app/app.go internal/ui/app.go internal/ui/messages.go cmd/korthex/main.go
git commit -m "feat(app): inject config.Manager for kubeconfig persistence on switch"
```

---

### Task 8: Documentation Updates

**Files:**
- Modify: `internal/config/CLAUDE.md`
- Modify: `internal/config/README.md`
- Modify: `internal/k8s/CLAUDE.md`
- Modify: `internal/k8s/README.md`
- Modify: `internal/ui/CLAUDE.md`
- Modify: `internal/ui/README.md`
- Modify: `internal/agent/CLAUDE.md`
- Modify: `internal/app/CLAUDE.md`
- Modify: `internal/app/README.md`
- Modify: root `CLAUDE.md`

- [ ] **Step 1: Update config module docs**

`internal/config/CLAUDE.md` — add to Files table:
```
| `config.go` | DiscoverKubeconfigs(), ParseContexts(): runtime kubeconfig discovery | 不要在这里做 K8s 连接测试 |
```

`internal/config/README.md` — add section:
```markdown
### Kubeconfig Discovery

- `DiscoverKubeconfigs(kubeDir, envKubeconfig)`: scans for kubeconfig files from $KUBECONFIG env, ~/.kube/config* files
- `ParseContexts(path)`: parses contexts from a kubeconfig file without creating a REST client
```

- [ ] **Step 2: Update k8s module docs**

`internal/k8s/CLAUDE.md` — add rule:
```
| 10 | **Reconnect 先验新再断旧** — Reconnect() 先创建新 clientset 并 ServerVersion() 验证，通过后才断开旧连接 | 切换失败时不丢失当前连接 |
```

`internal/k8s/README.md` — add section:
```markdown
### Hot Reconnect

`Reconnect(kubeconfig, context)` performs safe cluster switching:
1. Create new clientset + validate via ServerVersion()
2. On success: teardown old informers, swap clientset, reinitialize all sub-clients
3. On failure: return error, old connection intact

`ContextInfo()` returns the current kubeconfig path and context name.
```

- [ ] **Step 3: Update ui module docs**

`internal/ui/CLAUDE.md` — add rule:
```
| 21 | **KubeSwitch Overlay** — Ctrl+K 或 /kubeconfig 触发。两步选择 (kubeconfig → context)。切换后重置 Resource/LogViewer，保留 Chat。Agent 运行时不响应 Ctrl+K | 避免切换中断分析 |
```

`internal/ui/README.md` — add to Files table and keyboard shortcuts.

- [ ] **Step 4: Update agent module docs**

`internal/agent/CLAUDE.md` — add to Phase 2 whitelist description:
```
| 12 | **switch_kubeconfig 工具** — AI 可主动切换集群。通过 EventKubeSwitch 事件通知 UI 执行实际切换。SafetyAllowed (客户端配置变更，不修改集群) | 自然语言集群切换 |
```

- [ ] **Step 5: Update app module docs**

`internal/app/CLAUDE.md` — add rule:
```
| 7 | **Config Manager 注入** — App 持有 config.Manager 引用，传递给 UI 用于切换后持久化 | 运行时配置变更需要持久化 |
```

`internal/app/README.md` — add section about runtime reconfiguration.

- [ ] **Step 6: Update root CLAUDE.md**

Add to Phase 1 Implementation Status decisions table:
```
| **Kubeconfig 热切换** | Ctrl+K / `/kubeconfig` 触发 Overlay，两步选择 (kubeconfig → context)。`Reconnect()` 先验新再断旧。切换后重置 Resource/LogViewer，保留 Chat。AI 可通过 `switch_kubeconfig` 工具主动触发 |
```

- [ ] **Step 7: Commit**

```bash
git add internal/config/CLAUDE.md internal/config/README.md internal/k8s/CLAUDE.md internal/k8s/README.md internal/ui/CLAUDE.md internal/ui/README.md internal/agent/CLAUDE.md internal/app/CLAUDE.md internal/app/README.md CLAUDE.md
git commit -m "docs: update module docs for kubeconfig switching feature"
```

---

### Task 9: Integration Verification

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/mio/korthex && go test ./... -race -cover`
Expected: All tests PASS, no race conditions

- [ ] **Step 2: Run linter**

Run: `cd /Users/mio/korthex && make lint`
Expected: No new lint errors

- [ ] **Step 3: Build binary**

Run: `cd /Users/mio/korthex && make build`
Expected: `bin/korthex` built successfully

- [ ] **Step 4: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address lint and test issues from kubeconfig switching"
```
