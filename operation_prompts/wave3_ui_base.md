# Wave 3F: internal/ui — TUI Infrastructure + Resource Browser

> **Prerequisite:** Wave 2 (k8s + llm) merged to main
> **Branch:** feat/ui-base
> **Parallel with:** wave3_agent.md (Instance E)
> **Files owned:** `internal/ui/*.go` (nothing else)
> **Estimated time:** 6-8 hours

---

You are developing the TUI infrastructure layer and Resource Browser panel for the Korthex project, using Bubble Tea (Elm Architecture).

## Step 1: Read Documentation

Read these files **in order** before writing any code:
1. `internal/ui/CLAUDE.md` — module rules (never modify Model in goroutine, global keys first, clone-on-read, severity colors)
2. `internal/ui/README.md` — panel architecture, keyboard routing priority, agent integration pattern
3. `SPEC.md` Section 3.6 — UI type definitions (in skeleton)
4. `SPEC.md` Section 4.4 — TUI keyboard routing pseudocode
5. `SPEC.md` Section 4.6 — **Message Flow Diagram** (three async pipelines, Msg types table, concurrency constraints)
6. `PRD.md` Section 5.1 — Resource Browser interaction (j/k/Enter/Esc/l/d/y)
7. `PRD.md` Section 5.4 — Layout Modes (Full TUI, Chat Focus, Log Focus)
8. `PRD.md` Section 5.5 — Keyboard Shortcuts table

## Step 2: Implement

### File 1: `internal/ui/styles.go`

Lipgloss theme definitions:
- 4 themes: dark (default), light, dracula, nord
- Severity color map: ERROR=red, WARN=yellow, INFO=blue, DEBUG=gray, FATAL=bold red
- Panel border styles: active (bright, e.g. `#7C3AED`) vs inactive (dim, e.g. `#555`)
- Text styles: title, subtitle, status, error, selected row
- Export a `Theme` struct or functions to get styles by theme name

### File 2: `internal/ui/layout.go`

Layout calculator:
```go
type PanelDimensions struct {
    ResourceW, ResourceH int
    LogViewerW, LogViewerH int
    ChatW, ChatH int
    StatusBarH int  // always 1
}

func CalculateLayout(width, height int, mode LayoutMode) PanelDimensions
```

- `LayoutFull`: resource panel (left 25%) | log viewer (right top 60%) / chat (right bottom 40%) / status bar (bottom 1 line)
- `LayoutChatFocus`: chat (full width top 70%) / log viewer collapsed (bottom 30%) / no resource panel
- `LayoutLogFocus`: log viewer full screen / status bar bottom
- Minimum terminal: 80x24 — if smaller, show warning
- Use `lipgloss.JoinVertical` / `lipgloss.JoinHorizontal` for composition

### File 3: `internal/ui/ringbuffer.go`

Thread-safe bounded ring buffer:
```go
type RingBuffer struct {
    mu       sync.Mutex
    entries  []logparse.LogEntry
    capacity int
    head     int
    count    int
}

func NewRingBuffer(capacity int) *RingBuffer
func (rb *RingBuffer) Append(entry logparse.LogEntry)   // shift oldest when full
func (rb *RingBuffer) Slice() []logparse.LogEntry        // clone-on-read
func (rb *RingBuffer) Len() int
func (rb *RingBuffer) Clear()
```

- `Append`: if `count == capacity`, overwrite oldest entry (circular buffer)
- `Slice`: return a **copy** of all entries in order — never return internal slice reference
- All methods are mutex-protected

### File 4: `internal/ui/ringbuffer_test.go`

- Append below capacity → Slice returns all in order
- Append beyond capacity → oldest entries evicted, order preserved
- Concurrent Append + Slice from multiple goroutines → no race (`go test -race`)
- Len() accuracy
- Clear() resets to empty

### File 5: `internal/ui/messages.go`

Already in skeleton. Verify completeness. Ensure all types from SPEC §4.6 Msg table are defined:
- `AgentEventMsg` (wraps `agent.AgentEvent`)
- `NavigateToLogsMsg` (namespace, podName, container)
- `LogsLoadedMsg` (logLines []k8s.LogLine)
- `LogLineMsg` (single log line for manual streaming)
- `InformerUpdateMsg` (namespace, resource type)
- `ClusterConnectedMsg` (namespaces []string)
- `ErrorMsg` (Err error)

### File 6: `internal/ui/help.go`

Help overlay (toggled by `?`):
- Render a centered overlay box listing all keyboard shortcuts from PRD §5.5
- Grouped by context: Global, Resource Browser, Log Viewer, AI Chat
- Dismiss with `?` or `Esc`

### File 7: `internal/ui/statusbar.go`

`StatusBarModel`:
- Display: K8s context name | namespace | LLM provider:model | layout mode | drop counter
- Always 1 line at bottom
- Implement `Init()`, `Update()`, `View()` (mostly static rendering)

### File 8: `internal/ui/resource.go`

`ResourceModel` — hierarchical resource browser:

**Navigation state machine:**
```
Level 0: Namespace list
Level 1: Deployment list (within selected namespace)
Level 2: Pod list (within selected deployment's namespace)
Level 3: Container list (within selected pod)
```

**Keys:**
- `j`/`k` or `↑`/`↓`: move selection
- `Enter`: drill into next level
- `Esc`/`Backspace`: go back one level
- `/`: enter search/filter mode (fuzzy filter current list)
- `l`: emit `NavigateToLogsMsg` for selected pod
- `d`: emit describe request
- `y`: copy selected resource name to clipboard (if possible, else noop)

**Data loading:** use `tea.Cmd` to call `k8s.Client.Resources()` methods asynchronously, never block Update

### File 9: `internal/ui/app.go`

`AppModel` — root Bubble Tea model:

```go
type AppModel struct {
    resource   ResourceModel
    logviewer  tea.Model    // placeholder for now
    chat       tea.Model    // placeholder for now
    statusbar  StatusBarModel

    focus      PanelID
    layout     LayoutMode
    width, height int

    agent      agent.Agent
    k8sClient  k8s.Client
    config     *config.Config
    program    *tea.Program  // for p.Send() from goroutines
}
```

**Init():** return tea.Cmd that loads initial namespace list

**Update(msg):** Follow keyboard routing priority from SPEC §4.4:
1. `tea.WindowSizeMsg` → recalculate layout, propagate to all children
2. Global hotkeys (regardless of focus):
   - `Tab`/`Shift+Tab` → cycle focus
   - `F1`/`F2`/`F3` → switch LayoutMode
   - `:` → focus Chat panel
   - `?` → toggle help overlay
   - `q` → quit (only when Chat is NOT focused)
   - `Ctrl+C` → cancel running agent operation
3. Delegate remaining keys to focused panel
4. Custom messages: route by type (AgentEventMsg → chat + logviewer, NavigateToLogsMsg → start log stream + switch focus)

**View():** use `layout.go` to compute dimensions, render each panel, compose with lipgloss

**Placeholder panels:** For logviewer and chat, create minimal `tea.Model` implementations that just render "Log Viewer — press l on a pod" and "AI Chat — press : to start". These will be replaced by wave3_ui_panels.

**SetProgram(p):** Store `*tea.Program` reference for goroutines to call `p.Send()`

Constructor: `func NewAppModel(a agent.Agent, k k8s.Client, cfg *config.Config) AppModel`

## Step 3: Verify

```bash
go test -v -race ./internal/ui/...
go build ./...
```

TUI should start with `go run ./cmd/korthex` (with valid kubeconfig), display namespace list, and allow navigation.

## Critical Rules

- **Never modify Model in goroutine**: all async work via `tea.Cmd` + `p.Send()`
- **Global keys handled first in AppModel.Update**: Tab, F1-F3, :, q, ?, Ctrl+C — never passed to children
- **`q` does NOT quit when Chat is focused**: user might be typing
- **AgentEventMsg routed by type, not focus**: EventLogsReady always goes to logviewer regardless of current focus
- **RingBuffer.Slice() returns clone**: never expose internal slice
- **Severity colors are fixed**: ERROR=red, WARN=yellow, INFO=blue, DEBUG=gray, FATAL=bold red
