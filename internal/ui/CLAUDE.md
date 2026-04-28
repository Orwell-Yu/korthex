# internal/ui — CLAUDE.md

> Bubble Tea TUI: three panels (Resource Browser, Log Viewer, AI Chat), keyboard routing, layout management, styling.

## Parent Reference

- **When confused about architecture:** Read [`../../CLAUDE.md`](../../CLAUDE.md) (root project rules, especially Rule #7: Bubble Tea 并发模型)
- **When confused about interface design:** Read [`../../SPEC.md`](../../SPEC.md) Section 3.6 (UI types)
- **When confused about keyboard routing:** Read [`../../SPEC.md`](../../SPEC.md) Section 4.4 (TUI 键盘路由)
- **When confused about message flow / async pipelines:** Read [`../../SPEC.md`](../../SPEC.md) Section 4.6 (Message Flow Diagram) -- **THIS IS CRITICAL for understanding concurrency**
- **When confused about layout modes:** Read [`../../PRD.md`](../../PRD.md) Section 5.4 (Layout Modes)
- **When confused about keyboard shortcuts:** Read [`../../PRD.md`](../../PRD.md) Section 5.5 (Keyboard Shortcuts)
- **When confused about log viewer features:** Read [`../../PRD.md`](../../PRD.md) Section 5.2 (Log Viewer)
- **When confused about AI chat features:** Read [`../../PRD.md`](../../PRD.md) Section 5.3 (AI Chat Interface)
- **When confused about agent events:** Read [`../agent/CLAUDE.md`](../agent/CLAUDE.md) (AgentEvent types)
- **When confused about K8s types for display:** Read [`../k8s/CLAUDE.md`](../k8s/CLAUDE.md) (types.go)
- **When confused about log entry rendering:** Read [`../../pkg/logparse/CLAUDE.md`](../../pkg/logparse/CLAUDE.md) (LogEntry, Severity)

## Module Rules

| # | Rule | Reason |
|---|------|--------|
| 1 | **Layer 3: 可以 import agent, k8s, config, logparse, history** | 不能 import app |
| 2 | **永远不在 goroutine 中修改 Model** — 所有异步操作走 tea.Cmd + p.Send() | Root CLAUDE.md Rule #7: Bubble Tea Elm 架构非 goroutine-safe |
| 3 | **全局键优先** — Tab/F1-F3/:/q/? 在 AppModel.Update 中处理，不传递给子面板 | 避免全局键被子面板拦截 |
| 4 | **AgentEventMsg 按类型路由，不按 focus** — EventLogsReady 始终给 logviewer，不管当前 focus 在哪 | 日志必须出现在 Log Viewer，无论用户正在看哪个面板 |
| 5 | **Ring Buffer clone-on-read** — Slice() 返回副本，不返回内部 slice | 避免 UI 渲染时 buffer 被修改 |
| 6 | **q 键: Chat focus 时不退出** — 只在 Resource/LogViewer focus 时 q 退出 | 用户在输入框打字时按 q 不应退出程序 |
| 7 | **Severity 色彩映射固定** — ERROR=红, WARN=黄, INFO=蓝, DEBUG=灰, FATAL=红色加粗 | 色彩认知一致性，不要随意更改 |
| 8 | **Chat 输入用 `bubbles/textinput`** — chat.go 必须使用 `bubbles/textinput` 组件，不手动拼字符串。`AppModel.Init()` 必须调用 `chat.Init()` 启动光标闪烁，`cursor.BlinkMsg` 必须路由到 chat | textinput 内建 CJK/IME 支持、光标移动 (←/→/Home/End)、闪烁动画、剪贴板等，手动实现易出 bug |
| 9 | **非 textinput 的手动输入用 `msg.Runes` + `utf8.DecodeLastRuneInString`** — resource.go 搜索框等手动输入场景仍需遵守 | `msg.String()` 对多字节字符失败；字节切片截断 CJK 字符 |
| 10 | **`tea.WithMouseCellMotion()` 启用** — 捕捉滚轮事件路由到 focused panel（Resource ±1, LogViewer/Chat ±3）。不用 `tea.WithMouseAllMotion()`（追踪移动，干扰 IME）。用户用 Shift+拖选复制文本 | 滚轮滚动比纯键盘更自然；CellMotion 不干扰 IME；Shift+拖选是终端 TUI 的通用标准 |
| 11 | **Spinner 生命周期** — Enter 提交时 `spinner.New()` 重置获取新 ID → 返回 `tea.Batch(executeAgent, spinner.Tick)` → `spinner.TickMsg` 驱动动画 → `isRunning = false` 时停止 | 每次 agent 运行用新 spinner ID，防止前次 stale tick 干扰 |
| 12 | **WindowSizeMsg 传内部尺寸** — `AppModel.Update` 中 WindowSizeMsg 必须对子面板 SetDimensions 传 `max(dim.W-2, 1), max(dim.H-2, 1)`（减去 border），不传 CalculateLayout 的原始外部尺寸 | 子面板的 scrollToBottom 在 Update 时用 height 计算，View 时 renderPanel 也用内部尺寸。两处必须一致，否则滚动偏移差 2 行，导致最后几条消息不可见 |
| 13 | **Log Viewer 水平滚动** — `h`/`l`/`left`/`right` 调整 `scrollX` 偏移, `0`/`home` 重置。`shiftLineLeft()` 是 ANSI-aware 的：保留颜色转义序列，只跳过可见字符。切换日志源时 `scrollX` 归零 | 长日志行在面板宽度内无法完全显示，水平滚动避免信息丢失 |
| 14 | **Chat 面板滚动** — `Ctrl+U`/`PgUp` 上翻半页, `Ctrl+D`/`PgDn` 下翻半页。`autoScroll` 标志位控制：用户手动滚上方时暂停自动滚动, 滚到底部或 EventComplete 时恢复。Header 显示 `SCROLLED` 指示器 | AI 回复长文本时用户需要向上查看完整内容 |
| 15 | **Agent 资源面板联动** — Agent 拥有 `navigate_resource_browser` 专用工具，system prompt 指示它在分析涉及资源时主动调用。导航到 AI 能确定的最具体层级（能定位 pod 就到 pod，只能定位 ns 就到 ns）。`app.go` 拦截该工具的 `EventToolCall` 发射 `NavigateToResourceMsg`，支持 `Name` 字段光标定位。Layout 非 Full 时自动切回 LayoutFull | AI 智能决定导航目标，比机械映射每个工具调用更精准。用户可在左侧面板交互选择 |
| 16 | **Glamour Markdown 渲染: 完成后一次性** — AI 回复通过 `glamour.Render()` 渲染，但仅在 stream 完成后 (EventComplete)。Streaming 期间显示原始文本 + spinner (Phase 1 行为)。每条消息缓存渲染结果，仅 window resize 时重新渲染。渲染失败静默降级为原始文本 | glamour 不支持 partial/streaming markdown (已知限制) |
| 17 | **书签生命周期** — 书签仅存内存，绑定 Ring Buffer。最多 50 个。Ring Buffer 淘汰旧行时自动删除对应书签。`m` 切换, `'` 打开列表覆盖层, `n`/`N` 导航 | 简单可靠, 不持久化 (P2-D7) |
| 18 | **Pod 详情面板是覆盖层** — 与 describe overlay (resource.go) 相同模式。非新 PanelID。Pod 列表中按 `d` 触发，`Esc` 关闭 | 复用现有 overlay 模式, 不改布局 |
| 19 | **历史搜索覆盖层** — Chat 中 `/history` 触发全屏覆盖层。通过 history.Store 做 FTS5 搜索。选中的会话摘要以 `[Previous context from ...]` 块注入当前对话上下文 | overlay 模式一致 |
| 20 | **资源类型切换 (1-5 键)** — 仅在 focus 为 PanelResource 且导航层级在 namespace 内 (非 namespace 列表) 时激活。`Esc` 重置资源类型为 Deployments。Header 显示当前类型 + 数量 | 数字键仅在 namespace drill-down 层级有意义 |
| 21 | **Ctrl+K 全局 Kubeconfig 切换** — `Ctrl+K` 打开 KubeSwitchModel 覆盖层 (任何 focus 下均可触发)。`/kubeconfig` slash command 也触发同一覆盖层。两步选择: kubeconfig 文件 → context。覆盖层内支持 fuzzy 搜索。切换成功后 Resource 面板和 Log Viewer 重置, Chat 历史保留 | 多集群/多 context 操作是 K8s 常见场景, 全局热键保证随时可达 |

## Panel Architecture

```
AppModel (tea.Model)
  ├── ResourceModel   ← bubbles.List or custom table
  │   └── 层级: Namespace → Deployment → Pod → Container
  ├── LogViewerModel  ← bubbles.Viewport
  │   └── RingBuffer → severity coloring → viewport content
  ├── ChatModel       ← bubbles.TextInput + bubbles.Viewport
  │   └── Agent events → message rendering
  └── StatusBarModel  ← static render
      └── context / namespace / provider / layout mode
```

## Files

| File | Do | Don't |
|------|-----|-------|
| `app.go` | Focus cycling, layout switching, global keys, message routing, `truncateContent`/`truncateAnsiLine` for overflow protection, `lipgloss.Place` hard-cap in View() | 不要在这里放面板渲染逻辑 |
| `resource.go` | 层级导航 (j/k/Enter/Esc), 搜索 (/), describe (d), yaml (y) | 不要直接调 k8s API, 通过 tea.Cmd |
| `logviewer.go` | Viewport, severity coloring, search (/pattern, Ctrl+N/P), follow (F), export (s) | 不要同步 IO 操作 |
| `chat.go` | TextInput, message history rendering, agent.Execute via tea.Cmd, spinner lifecycle, query history (↑/↓ recall) | 不要在 Update 中阻塞等待 agent |
| `statusbar.go` | 静态状态渲染 | 不要复杂逻辑 |
| `styles.go` | Lipgloss 主题定义, severity 色彩, 面板边框 (active/inactive) | 不要硬编码颜色值到其他文件 |
| `layout.go` | 面板尺寸计算 (by terminal size + LayoutMode) | 不要假设固定终端大小 |
| `ringbuffer.go` | mutex lock, append, shift oldest if full, Slice() clone | 不要用 lock-free, mutex 足够 |
| `help.go` | 快捷键参考 overlay | 保持与 PRD Section 5.5 和 Phase 2 PRD §11 一致 |
| `messages.go` | AgentEventMsg, NavigateToLogsMsg, LogsLoadedMsg, ErrorMsg | 每个自定义 Msg 类型一个 type |
| `poddetail.go` | Pod 详情覆盖层: conditions, events, container status, metrics 展示 (Metrics API 优雅降级) | 不要创建新 PanelID, 用 overlay 模式 |
| `bookmark.go` | BookmarkEntry 类型, 书签列表覆盖层, toggle/navigate/delete 逻辑, Ring Buffer 联动清理 | 最多 50 个, 不持久化 |
| `history.go` | 历史搜索覆盖层: TextInput 搜索框, FTS5 查询, 会话列表, 上下文注入 | `/history` 触发, Esc 关闭 |
| `kubeswitch.go` | KubeSwitchModel 覆盖层: 两步选择 (kubeconfig file → context), fuzzy 搜索, `kubeSwitchExecuteMsg`/`kubeSwitchCompleteMsg` 消息类型, `openKubeSwitch()` 入口方法 | `Ctrl+K` 或 `/kubeconfig` 触发, Esc 关闭 |
| `markdown.go` | Glamour 渲染封装: 主题映射 (dark/light/dracula/nord → StyleConfig), 渲染缓存, 异步 tea.Cmd 渲染, 失败降级 | 不在 streaming 期间调用 glamour |

## Keyboard Routing Priority

```
1. tea.WindowSizeMsg     → always: recalculate layout, propagate
2. Global hotkeys        → Tab, Shift+Tab, F1-F3, :, ?, q, Ctrl+C, Ctrl+K (kubeswitch)
3. Focused panel         → delegate to ResourceModel/LogViewerModel/ChatModel
4. Custom messages       → route by type (not by focus)
```

> Full routing pseudocode → [`../../SPEC.md`](../../SPEC.md) Section 4.4

## Agent Integration Pattern (chat.go)

```
Enter pressed → tea.Cmd:
  goroutine {
    agent.Execute(ctx, query, eventCh)
  }
  goroutine {
    for event := range eventCh {
      program.Send(AgentEventMsg(event))  // ← Bubble Tea sanctioned pattern
    }
  }

AppModel.Update(AgentEventMsg):
  → chat.Update(msg)                    // always: display in chat
  → if EventLogsReady: logviewer.Update  // always: push to log viewer
  → if EventToolCall for navigate_resource_browser:
       resource.Update(NavigateToResourceMsg)  // sync left panel with Name highlight
       ensure LayoutFull                       // make resource panel visible
  → if EventKubeSwitch:
       k8s.Reconnect → reset Resource/LogViewer → persist config
       chat history preserved across switch
```

## Cross-Module Dependencies

| This module | → | Dependency | Via |
|-------------|---|-----------|-----|
| ui | imports | agent | `agent.Agent`, `agent.AgentEvent`, `agent.AgentEventType` |
| ui | imports | k8s | `k8s.Client` (manual pod log browsing), `k8s.LogLine` |
| ui | imports | config | `config.UIConfig` (theme, limits) |
| ui | imports | logparse | `logparse.LogEntry`, `logparse.Severity` (rendering) |
| ui | imports | history | `history.Store` (搜索覆盖层) |

## Three Async Pipelines (CRITICAL — read SPEC §4.6)

三条异步管道同时工作，全部通过 `p.Send()` 进入 Bubble Tea 事件循环：

| Pipeline | Source | Msg Type | Trigger | 取消方式 |
|----------|--------|----------|---------|---------|
| **Agent 流式** | `agent.Execute()` goroutine | `AgentEventMsg` | User Enter in Chat | 独立 `ctx.Cancel()` via Ctrl+C |
| **日志流** | `k8s.StreamLogs()` goroutine | `LogLineMsg` | Press `l` on Pod / AI EventLogsReady | 独立 `ctx.Cancel()` on navigate away |
| **Informer 事件** | Informer EventHandler | `InformerUpdateMsg` | K8s Watch stream (always-on) | App shutdown |
| **Spinner 动画** | `bubbles/spinner` | `spinner.TickMsg` | User Enter in Chat (与 Agent 流式同时启动) | `isRunning = false` 时停止 tick 链 |

**并发规则:**
- 三条管道可同时运行，互不阻塞
- Agent 的 EventLogsReady 和手动日志流可能同时向 LogViewer 推数据 → logviewer 必须能合并处理
- 每条管道持有独立 `context.WithCancel`，Ctrl+C 只取消 agent，不影响其他管道
- `p.Send()` 是 thread-safe 的，可从任意 goroutine 调用

> **Full diagram:** [`../../SPEC.md`](../../SPEC.md) Section 4.6 — 包含完整的 Msg 类型总表、流转图、p.Send() 获取方式

## Testing Checklist

- [ ] RingBuffer: append 超过 capacity → oldest 被淘汰
- [ ] RingBuffer: concurrent Append + Slice → no race (go test -race)
- [ ] Layout: LayoutFull 在 80x24 终端的面板尺寸
- [ ] Layout: LayoutLogFocus 全屏日志
- [ ] Focus: Tab cycling Resource → LogViewer → Chat → Resource
- [ ] Focus: q in Chat → 不退出; q in Resource → 退出
- [ ] AgentEventMsg routing: EventLogsReady 同时到 chat 和 logviewer
- [ ] teatest: 基本启动 + 渲染 + 键盘输入
