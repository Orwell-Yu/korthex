# Wave 3G: internal/ui — Log Viewer + AI Chat Panels

> **Prerequisite:** Wave 3F (ui-base) completed, feat/ui-base merged or available
> **Branch:** feat/ui-panels (based on feat/ui-base or main after merge)
> **Parallel with:** wave3_agent.md (Instance E) — but must wait for wave3_ui_base.md (Instance F) to finish first
> **Files owned:** `internal/ui/logviewer.go`, `internal/ui/chat.go` only
> **Estimated time:** 5-7 hours

---

You are developing the Log Viewer and AI Chat panels for the Korthex TUI. The infrastructure (styles, layout, ringbuffer, messages, app.go shell) has already been implemented by a previous developer.

## Step 1: Read Documentation

Read these files **in order**:
1. `internal/ui/CLAUDE.md` — module rules
2. `internal/ui/README.md` — panel architecture, keyboard routing, agent integration pattern
3. `SPEC.md` Section 4.6 — **Message Flow Diagram** (especially Pipeline 1: Agent streaming, Pipeline 2: Log streaming)
4. `PRD.md` Section 5.2 — Log Viewer features and display format
5. `PRD.md` Section 5.3 — AI Chat Interface, user flows, error handling
6. `PRD.md` Section 5.5 — Keyboard shortcuts (Log Viewer and Chat sections)

Also read the existing code:
- `internal/ui/styles.go` — available styles and severity colors
- `internal/ui/ringbuffer.go` — RingBuffer API (Append, Slice, Len, Clear)
- `internal/ui/messages.go` — all custom Msg types
- `internal/ui/app.go` — how AppModel routes messages to panels (find the placeholder logviewer/chat)

## Step 2: Implement

### File 1: `internal/ui/logviewer.go`

Replace the placeholder with full `LogViewerModel`:

**Core components:**
- `bubbles.Viewport` for scrollable content
- Reference to `*RingBuffer` (shared with AppModel)
- `logparse.Parser` for parsing incoming log lines
- Search state: current pattern, match positions, current match index

**Rendering:**
- Each log line: `[timestamp] pod/name SEVERITY message`
- Severity colorization using `styles.go` color map
- Search matches highlighted (inverted colors)
- Follow mode indicator: `[FOLLOW]` in top-right when active

**Keys** (when focused):
- `j`/`k` or `↑`/`↓`: scroll one line
- `g`: jump to top
- `G`: jump to bottom
- `Ctrl+D`/`Ctrl+U` or `PageDown`/`PageUp`: scroll page (1000 lines)
- `/`: enter search mode → text input → regex search in buffer
- `Ctrl+N`: next search match
- `Ctrl+P`: previous search match (or `N` for next, `Shift+N` for prev)
- `Esc`: exit search mode
- `F`: toggle follow mode (auto-scroll to bottom on new lines)
- `f`: enter filter mode (only show lines matching regex)
- `s`: export current buffer to file (`~/.korthex/logs/{ns}_{deploy}_{timestamp}.log`)
- `y`: copy selected line to clipboard

**Message handling:**
- `LogsLoadedMsg` → parse each LogLine with `logparse.Parser.Parse()` → Append to RingBuffer → rebuild viewport content
- `LogLineMsg` → same, but single line (manual streaming)
- When follow mode is on: auto-scroll viewport to bottom after append

**Viewport content rebuild:**
- Read from `RingBuffer.Slice()` (clone)
- Apply filter regex if active
- Apply severity coloring
- Apply search highlighting
- Join into string → set as viewport content

### File 2: `internal/ui/chat.go`

Replace the placeholder with full `ChatModel`:

**Core components:**
- `bubbles.TextInput` for user query input
- `bubbles.Viewport` for message history display
- Message buffer: `[]ChatMessage` (user queries, AI responses, tool calls, errors)
- Agent running state: `isRunning bool` + `cancelFunc context.CancelFunc`

**ChatMessage type:**
```go
type ChatMessage struct {
    Role    string  // "user", "assistant", "tool", "error", "status"
    Content string
}
```

**Rendering:**
- User messages: right-aligned or prefixed with "You: "
- AI text: prefixed with "AI: ", streamed incrementally
- Tool calls: styled as `$ kubectl logs ...` (using CommandDisplay from AgentEvent)
- Tool results: brief summary
- Errors: red text
- "Thinking..." spinner while agent is running

**Keys** (when focused):
- Text input captures all printable keys
- `Enter`: submit query (if not empty and agent not running)
- `Esc`: blur text input (return focus control to AppModel)
- `Ctrl+C`: cancel running agent operation

**Enter handler** — trigger agent execution:
```go
// Return tea.Cmd that:
func executeAgent(p *tea.Program, agent Agent, query string) tea.Cmd {
    return func() tea.Msg {
        ctx, cancel := context.WithCancel(context.Background())
        // store cancel for Ctrl+C
        ch := make(chan agent.AgentEvent, 16)
        go func() {
            defer close(ch)
            agent.Execute(ctx, query, ch)
        }()
        go func() {
            for ev := range ch {
                p.Send(AgentEventMsg(ev))
            }
        }()
        return agentStartedMsg{cancel: cancel}
    }
}
```

**AgentEvent handling:**
| Event Type | Chat Rendering |
|------------|---------------|
| `EventStreamDelta` | Append delta to current AI message (incremental) |
| `EventToolCall` | Add `$ {CommandDisplay}` line |
| `EventToolResult` | Add brief result summary |
| `EventLogsReady` | Add "N log lines loaded in Log Viewer" status |
| `EventSummary` | Add final summary text |
| `EventError` | Add error in red |
| `EventComplete` | Set `isRunning=false`, re-enable input |

**Scroll:** viewport auto-scrolls to bottom on new messages

## Step 3: Update app.go

Replace the placeholder logviewer and chat models in `AppModel` with the real implementations:
- In the `AppModel` struct: change `logviewer` and `chat` from placeholder to `LogViewerModel` and `ChatModel`
- In `NewAppModel`: construct real panels
- In `Update`: remove placeholder routing, use real panel Update methods
- Ensure `AgentEventMsg` routing: always to chat, and to logviewer if `EventLogsReady`
- Ensure `NavigateToLogsMsg`: start log stream via `tea.Cmd` → `k8s.Logs().StreamLogs()` → `p.Send(LogLineMsg)`

## Step 4: Verify

```bash
go test -v -race ./internal/ui/...
go build ./...
```

Full TUI should work: navigate resources → press `l` for logs → press `:` for AI chat → type query → see results.

## Critical Rules

- **Never modify Model fields in goroutine**: agent execution and log streaming use `p.Send()` pattern
- **AgentEventMsg routes by type**: EventLogsReady → logviewer (always), all events → chat (always). Not by focus.
- **RingBuffer.Slice() for viewport rebuild**: always get a fresh clone, apply coloring on the clone
- **Follow mode**: auto-scroll only when follow is ON. User scrolling up disables follow.
- **Ctrl+C only cancels agent**: does not quit the app. Call `cancelFunc()` stored from agentStartedMsg
- **Search is regex-based**: use `regexp.Compile` with error handling (invalid regex → show error in status, don't crash)
- **Export path**: `~/.korthex/logs/{namespace}_{deployment}_{timestamp}.log`
