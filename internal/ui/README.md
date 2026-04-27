# internal/ui - TUI Components

> **Parent docs:** [CLAUDE.md (root)](../../CLAUDE.md) | [SPEC.md Section 3.6](../../SPEC.md) | [PRD Section 5.1-5.5](../../PRD.md)
>
> **Module rules:** [CLAUDE.md (this module)](./CLAUDE.md)
>
> **Depends on:** [agent](../agent/README.md), [k8s](../k8s/README.md), [config](../config/README.md), [logparse](../../pkg/logparse/README.md) | **Depended by:** [app](../app/README.md)

## Responsibility

Bubble Tea TUI rendering, keyboard input routing, layout management, and visual styling. Contains all Bubble Tea Model/Update/View logic. Owns the Ring Buffer for log display.

## Architecture

Uses the **Hierarchical Model Pattern** from Bubble Tea's Elm Architecture:

```
AppModel (root router & compositor)
├── ResourceModel   (resource browser panel)
├── LogViewerModel  (log viewer panel)
├── ChatModel       (AI chat panel)
└── StatusBarModel  (status bar)
```

## Key Types

| Type | Purpose |
|------|---------|
| `PanelID` | Enum: PanelResource, PanelLogViewer, PanelChat |
| `LayoutMode` | Enum: LayoutFull (3-panel), LayoutChatFocus, LayoutLogFocus |
| `AppModel` | Root Bubble Tea model owning all child panels |
| `RingBuffer` | Thread-safe bounded log line buffer (mutex-based) |

## Files

| File | Responsibility |
|------|---------------|
| `app.go` | Root AppModel: Init/Update/View, focus cycling (Tab/Shift+Tab), layout switching (F1/F2/F3), global key handling, message routing. `Init()` calls `chat.Init()` to start cursor blink. Routes `cursor.BlinkMsg` to chat. Contains `truncateContent`/`truncateAnsiLine` helpers and `lipgloss.Place` hard-cap |
| `resource.go` | ResourceModel: hierarchical list navigation (Namespace -> Deployment -> Pod -> Container). j/k/Enter/Esc navigation. Emits `NavigateToLogsMsg` on `l` press |
| `logviewer.go` | LogViewerModel: `bubbles.Viewport` for scrollable logs. Ring buffer feeding, severity colorization, `/pattern` search, `Ctrl+N/P` next/prev match, `h`/`l` horizontal scroll, `0` reset scroll, `F` follow mode, `s` export to `~/.korthex/logs/`, `shiftLineLeft` ANSI-aware horizontal offset |
| `chat.go` | ChatModel: `bubbles/textinput` for input (cursor blink, left/right movement, Home/End). Triggers `agent.Execute` on Enter, renders AgentEvents with role-based styling. Query history recall via up/down arrows. `Ctrl+U`/`PgUp` and `Ctrl+D`/`PgDn` for vertical scrolling, `autoScroll` flag for smart auto-scroll behavior, `SCROLLED` header indicator |
| `statusbar.go` | StatusBarModel: current context, namespace, connection status, LLM provider/model, layout mode indicator, drop counter |
| `styles.go` | Lipgloss theme definitions (dark/light/dracula/nord), severity color map (ERROR=red, WARN=yellow, INFO=blue, DEBUG=gray), active/inactive panel borders |
| `layout.go` | Layout calculator: given terminal width/height + LayoutMode, compute each panel's dimensions. Uses `lipgloss.JoinVertical/JoinHorizontal` |
| `ringbuffer.go` | Thread-safe ring buffer: `Append(entry)` shifts oldest when full, `Slice()` returns clone, mutex-based |
| `help.go` | Help overlay showing keybinding reference table |
| `messages.go` | Custom `tea.Msg` types: AgentEventMsg, NavigateToLogsMsg, LogsLoadedMsg, ClusterConnectedMsg, ErrorMsg, MarkdownRenderedMsg |
| `poddetail.go` | Pod detail overlay: structured Pod info (status, containers with CPU/memory, conditions checkmarks, recent events). Metrics API discovery + graceful degradation (show `-` when unavailable) |
| `bookmark.go` | Bookmark management: BookmarkEntry (LineIndex, Preview, CreatedAt), toggle `m`, list overlay `'`, next/prev `n`/`N`, auto-cleanup on Ring Buffer eviction. Max 50 |
| `history.go` | History search overlay: triggered by `/history` in Chat. TextInput for FTS5 query, result list with session summary preview, Enter to load context, Esc to close |
| `markdown.go` | Glamour rendering wrapper: maps Korthex themes (dark/light/dracula/nord) to glamour StyleConfig. Caches rendered output per message. Async render via `tea.Cmd` returning `MarkdownRenderedMsg`. Falls back to raw text on render error |
| `ringbuffer_test.go` | Concurrent access tests, capacity enforcement tests |

## Keyboard Routing

Priority order in `AppModel.Update()`:

1. **WindowSizeMsg**: always handled, recalculate layout, propagate to children
2. **Global hotkeys** (regardless of focus):
   - `Tab`/`Shift+Tab`: cycle focus
   - `F1`/`F2`/`F3`: switch layout
   - `:`: focus Chat panel
   - `?`: toggle help overlay
   - `q`: quit (only when Chat is NOT focused)
   - `Ctrl+C`: cancel running agent operation
3. **Delegated keys**: forwarded to focused panel's `Update()`
4. **Custom messages**: routed by type (AgentEventMsg -> chat + logviewer, NavigateToLogsMsg -> start log stream)

## Focus Visual Indicator

- Focused panel: bright border (e.g., `lipgloss.Color("#7C3AED")`)
- Unfocused panel: dim border (e.g., `lipgloss.Color("#555")`)

## Agent Integration (chat.go)

```
User presses Enter with query text:
  -> Reset spinner (spinner.New with fresh ID)
  -> Set isRunning = true
  -> Return tea.Batch(executeAgent(query), spinner.Tick):
     1. executeAgent: goroutine calls agent.Execute(ctx, query, eventCh)
        - Another goroutine reads eventCh, calls program.Send(AgentEventMsg)
     2. spinner.Tick: starts tick chain (~83ms/frame MiniDot animation)
  -> AppModel.Update routes:
     - AgentEventMsg -> chat.Update + logviewer.Update (if LogsReady)
     - spinner.TickMsg -> chat.Update (advances frame if isRunning)
  -> First EventStreamDelta removes "Thinking..." message
  -> EventComplete sets isRunning=false, ticks stop naturally
```

This uses Bubble Tea's sanctioned `p.Send()` pattern for external event sources.
The `bubbles/spinner` component provides ID-based tick deduplication to prevent
stale ticks from prior agent runs.

## Dependencies

- **External**: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/bubbles`, `charmbracelet/glamour`
- **Internal**: `agent` (Agent interface), `k8s` (Client for manual browsing), `config` (UIConfig), `logparse` (LogEntry for rendering), `history` (Store for search overlay)

## Testing Strategy

- **Ring buffer**: concurrent write/read tests, capacity enforcement, clone-on-read verification
- **Layout**: verify panel dimension calculations for various terminal sizes and modes
- **TUI integration**: `charmbracelet/x/exp/teatest` for headless Bubble Tea testing
- **Snapshot tests**: verify View() output for known states

## CJK / IME Input Pattern

All `tea.KeyMsg` default handlers use `msg.Runes` instead of `msg.String()`:
- `msg.String()` returns key name (e.g., `"a"`) whose byte-length check (`len(s) == 1`) fails for CJK (3 bytes)
- `msg.Runes` contains the actual rune slice, correctly handling single-char, multi-char IME composition, and paste
- Backspace uses `utf8.DecodeLastRuneInString` instead of byte-level `s[:len(s)-1]`
- `tea.WithMouseAllMotion()` is NOT used — it interferes with IME input

Pattern (used in resource.go, chat.go, logviewer.go):
```go
case "backspace":
    if len(m.input) > 0 {
        _, size := utf8.DecodeLastRuneInString(m.input)
        m.input = m.input[:len(m.input)-size]
    }
default:
    if len(msg.Runes) > 0 {
        m.input += string(msg.Runes)
    }
```

## Overflow Protection (app.go)

Two-layer defense prevents content from exceeding terminal dimensions:
1. **`renderPanel()`** calls `truncateContent(content, innerW, innerH)` — clips lines to panel height and truncates wide lines with ANSI-escape-aware `truncateAnsiLine`
2. **`View()`** wraps final composed output with `lipgloss.Place(m.width, m.height, ...)` — hard-caps the entire TUI frame to terminal size

**Critical: inner dimension normalization.** `WindowSizeMsg` handler sets child panel dimensions to `max(dim - 2, 1)` (subtracting border), NOT the raw `CalculateLayout` values. This ensures `scrollToBottom()` during Update uses the same height that `View()` rendering uses. Without this, scroll offset is 2 lines too low and the last messages (AI summary) are invisible.

Minimum terminal size: 60x16 (lowered from 80x24 for small-terminal support).

## Phase 2 Keyboard Shortcuts

| Key | Context | Action |
|-----|---------|--------|
| `1`-`5` | Resources (ns level) | Switch resource type (Deploy/SS/DS/Job/CJ) |
| `d` | Resources (Pod level) | Open Pod detail panel |
| `m` | Log Viewer | Toggle bookmark on current line |
| `'` | Log Viewer | Open bookmark list |
| `n` / `N` | Log Viewer | Jump to next/previous bookmark |
| `/history` | Chat | Open history search overlay |

## Glamour Integration

- **Library**: `charmbracelet/glamour` (Markdown terminal renderer, same Charm ecosystem)
- **Strategy**: Complete-then-render (glamour does not support streaming/partial markdown)
- **Flow**: Streaming → raw text + spinner → `EventComplete` → async `tea.Cmd` calls `glamour.Render()` → `p.Send(MarkdownRenderedMsg{})` replaces raw text
- **Theme mapping**: Each Korthex theme maps to a `glamour.TermRendererOption` (dark → `WithAutoStyle()`, light → `WithStylePath("light")`, dracula/nord → custom `StyleConfig`)
- **Cache**: Rendered result cached per message ID, re-render only on window resize
- **Fallback**: Render error → display raw markdown, no user-visible error

## Phase 2+ Extension Points

- Add log visualization panel (Phase 3: severity distribution chart, heatmap)
- Add collaborative mode panel (Phase 4)
