# Korthex - Project CLAUDE.md

> AI-native Kubernetes TUI tool: "Speak to your cluster, see everything."

## Critical Rules (Non-Negotiable)

| # | Rule | Reason |
|---|------|--------|
| 1 | **子模块有独立 CLAUDE.md** — 进入任何子目录前，先读取该目录的 CLAUDE.md，遵守模块特定规则 | 每个模块有自己的接口约束、依赖规则、安全边界 |
| 2 | **Layer 规则禁止违反** — Layer N 只能 import Layer < N，同 Layer 禁止互相 import（见下方依赖层级图） | 违反会引入循环依赖，破坏并行开发能力 |
| 3 | **client-go 类型不泄漏** — 只有 `internal/k8s/` 可以 import `k8s.io/client-go`，其他模块只使用 `k8s/types.go` 中的 Korthex 内部类型 | 隔离 client-go API 变更，保持接口稳定 |
| 4 | **LLM SDK 类型不泄漏** — 只有 `internal/llm/` 中的 adapter 文件可以 import LLM SDK，agent 和 ui 只使用 `llm/provider.go` 中的统一类型 | 三个 SDK API 不兼容，统一层必须严格封装 |
| 5 | **Phase 1-2 只读** — 所有 K8s 操作必须是只读的（get, list, logs, describe, events），禁止 delete/apply/scale 等写操作 | Phase 1-2 安全策略，写操作在 Phase 3 引入（含三次确认） |
| 6 | **接口优先** — 新增功能先定义接口 (interface)，再写 mock，再写调用方 test，最后实现。禁止跳过接口直接写实现 | 保障并行开发能力和可测试性 |
| 7 | **Bubble Tea 并发模型** — 永远不在 goroutine 中直接修改 Model 字段。所有异步操作通过 `tea.Cmd` + `p.Send()` 模式 | Bubble Tea 的 Elm 架构不是 goroutine-safe 的 |
| 8 | **环境变量优先** — API Key 优先从环境变量读取，config.yaml 仅作 fallback，永远不要在代码中硬编码 key | 安全性 + twelve-factor app 规范 |

## Reference Documents (阅读顺序)

| Document | Location | When to Read |
|----------|----------|-------------|
| **PRD** | [`PRD.md`](./PRD.md) | 理解产品需求、功能规格、用户场景 |
| **SPEC** | [`SPEC.md`](./SPEC.md) | 理解技术架构、接口定义、模块设计、开发计划 |
| **Module CLAUDE.md** | 各 `internal/*/CLAUDE.md` 和 `pkg/*/CLAUDE.md` | 进入具体模块开发前必读 |
| **Module README** | 各 `internal/*/README.md` 和 `pkg/*/README.md` | 理解模块接口、文件职责、测试策略 |
| **Phase 2 PRD** | [`Phase 2 PRD`](./docs/superpowers/specs/2026-04-24-phase2-prd-design.md) | Phase 2 功能规格、设计决策、实现评估 |
| **Default Config** | [`configs/default.yaml`](./configs/default.yaml) | 理解配置项及默认值 |

> **When confused:** 先看当前模块的 CLAUDE.md → 不够? 看 SPEC.md 对应章节 → 还不够? 看 PRD.md 对应场景

## Architecture Overview

### Dependency Layer (Layer 规则)

```
Layer 4 (Integration):    cmd/korthex + internal/app     ← imports everything
Layer 3 (Presentation):   internal/ui                     ← imports agent, k8s, config, logparse, history
Layer 2 (Intelligence):   internal/agent                  ← imports k8s, llm, logparse, config, history, redact
Layer 1 (Core):           internal/k8s, internal/llm, internal/history  ← imports config only
Layer 0 (Foundation):     internal/config, pkg/logparse, pkg/redact     ← imports nothing internal
```

### Module Map

| Module | Path | Responsibility | CLAUDE.md |
|--------|------|---------------|-----------|
| **config** | `internal/config/` | 配置加载/验证/持久化、Setup Wizard | [CLAUDE.md](./internal/config/CLAUDE.md) |
| **logparse** | `pkg/logparse/` | 日志解析: timestamp/severity/JSON | [CLAUDE.md](./pkg/logparse/CLAUDE.md) |
| **redact** | `pkg/redact/` | 日志脱敏: regex 引擎, 默认+自定义规则 | [CLAUDE.md](./pkg/redact/CLAUDE.md) |
| **k8s** | `internal/k8s/` | K8s 集群交互: Informer/Cache、日志流 | [CLAUDE.md](./internal/k8s/CLAUDE.md) |
| **llm** | `internal/llm/` | 多 Provider LLM 通信: 统一接口 + SDK adapter | [CLAUDE.md](./internal/llm/CLAUDE.md) |
| **history** | `internal/history/` | 对话历史: SQLite 持久化, FTS5 搜索 | [CLAUDE.md](./internal/history/CLAUDE.md) |
| **agent** | `internal/agent/` | Agentic Loop: NL → Tool → 执行 → 反馈 | [CLAUDE.md](./internal/agent/CLAUDE.md) |
| **ui** | `internal/ui/` | Bubble Tea TUI: 面板、键盘、布局 | [CLAUDE.md](./internal/ui/CLAUDE.md) |
| **app** | `internal/app/` + `cmd/korthex/` | 依赖注入、生命周期管理 | [CLAUDE.md](./internal/app/CLAUDE.md) |

### Module Dependency Graph

```
     config ←── (no deps)    logparse ←── (no deps)    redact ←── (no deps)
        │                        │                         │
   ┌────┴────┐                   │                         │
   ▼         ▼                   │                         │
  k8s       llm    history       │                         │
   │         │       │           │                         │
   └────┬────┘       │           │                         │
        ▼            │           │                         │
      agent  ←───────┴───────────┴─────────────────────────┘
        │
        ▼
       ui  ←── (also imports history)
        │
        ▼
       app
```

## Tech Stack

| Category | Technology | Version/Notes |
|----------|-----------|---------------|
| Language | Go | 1.24 (locked in go.mod, see SPEC §6.9 for rationale) |
| TUI | Bubble Tea + Lipgloss + Bubbles | charmbracelet ecosystem |
| K8s Client | client-go | 官方 SDK, Informer/Cache 模式 |
| LLM (OpenAI) | openai-go | v3, Official |
| LLM (Anthropic) | anthropic-sdk-go | v1.37+, Official |
| LLM (Gemini) | google.golang.org/genai | GA 2025, Official |
| Config | Viper | YAML + env var override |
| Retry | cenkalti/backoff/v4 | 指数退避 (k9s 同款) |
| Fuzzy Search | sahilm/fuzzy | 资源模糊搜索 (k9s 同款) |
| SQLite (Pure Go) | modernc.org/sqlite | BSD-3-Clause, 对话历史持久化 (no CGO) |
| Markdown Render | charmbracelet/glamour | MIT, AI Chat Markdown 终端渲染 |
| Lint | golangci-lint | 见 .golangci.yml |
| Release | GoReleaser | 多平台 binary (源码构建) |

## Development Workflow

### Build & Run

```bash
make build          # → bin/korthex
make run            # build + run
make test           # go test -race -cover
make lint           # golangci-lint
make fmt            # gofmt + goimports
make ci             # lint + test + build (CI 全流程)
```

### Environment Variables

```bash
# LLM API Key (优先级最高)
export KORTHEX_LLM_API_KEY="sk-..."

# 或 provider 特定变量 (也会被自动识别)
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export GEMINI_API_KEY="..."

# K8s (通常已配置)
export KUBECONFIG="~/.kube/config"
```

### Testing

```bash
make test           # 全量测试 (含 race detector)
make test-short     # 跳过集成测试
go test ./internal/k8s/... -v       # 单模块测试
go test ./pkg/logparse/... -v -run TestDetectSeverity  # 单测试
```

### Adding a New Module or Feature

1. 先读 SPEC.md 对应章节，确认架构约束
2. 定义接口 (interface) → 写 mock → 写调用方 test → 实现
3. 确保不违反 Layer 规则
4. 更新模块 README.md 和 CLAUDE.md

## Service Dependencies (Phase 1)

```
Korthex Binary
  ├── K8s API Server (via client-go, kubeconfig)
  │     └── Informer Watch Stream (long-lived connection)
  │     └── GetLogs API (on-demand)
  │
  └── LLM API (one of, via official SDK)
        ├── OpenAI API (api.openai.com)
        ├── Anthropic API (api.anthropic.com)
        ├── Gemini API (generativelanguage.googleapis.com)
        └── Custom OpenAI-compatible endpoint (user-provided URL)
```

## Phase Roadmap Summary

| Phase | Focus | Key Deliverables |
|-------|-------|-----------------|
| **Phase 1** (done) | Log Intelligence | TUI + AI Chat + Log Viewer + Multi-provider LLM |
| **Phase 2** (current) | Deep Analysis | 日志分析增强, 对话持久化, 脱敏规则, 资源类型扩展, Markdown 渲染 |
| Phase 3 | Cluster Management | AI 辅助写操作 (scale/restart/delete), 三次确认 |
| Phase 4 | Advanced | 多集群, Plugin 系统, MCP Server, 外部日志源 |

## Phase 1 Implementation Status

All 7 modules implemented and wired together. Key technical decisions made during implementation:

| Decision | Detail |
|----------|--------|
| **SIGTERM-only** | 只监听 SIGTERM，SIGINT 交给 Bubble Tea 原生处理 (Ctrl+C = cancel agent / quit) |
| **klog 重定向** | client-go 的 klog/v2 输出重定向到 `/tmp/korthex/korthex.log`，防止 TUI 污染 |
| **KUBECONFIG 多路径** | 用 `filepath.SplitList` 解析冒号分隔的多路径，取第一个 |
| **CJK 输入** | 所有文本输入使用 `msg.Runes` 获取输入字符，`utf8.DecodeLastRuneInString` 处理退格 |
| **UI 内容截断** | `renderPanel()` 中硬截断内容行数和行宽，防止终端换行导致溢出 |
| **启动 Logo** | 在 `app.New()` 前显示 ASCII Logo + 版本，初始化完成后切换到 alt-screen TUI |
| **鼠标滚轮** | `tea.WithMouseCellMotion()` 启用滚轮（不追踪移动，不影响 IME）。滚轮路由到 focused panel：Resource ±1 行，LogViewer/Chat ±3 行。文本复制用 Shift+拖选 |
| **退出清屏** | `p.Run()` 返回后 `\033[2J\033[H` 清除 main screen buffer，防止 splash 残留 |
| **启动动画** | `startSplash()` 显示 ASCII logo + Braille 角色 art + spinner 动画，初始化完成后停止 |
| **Thinking 动画** | `bubbles/spinner` MiniDot (⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏) 驱动 Chat 面板 Thinking 动画，每次 agent 启动时 reset spinner 获取新 ID 防止 stale tick |
| **内部尺寸对齐** | `WindowSizeMsg` 中对子面板传 `dim - 2`（减 border）而非原始布局值，确保 Update/View 时 scroll 计算一致 |
| **大集群 filter** | list 工具 (namespaces/deployments/pods) 支持 `filter` 参数做名称子串匹配，避免 346+ 命名空间全量列表超出 tool result 压缩阈值 |
| **水平滚动** | Log Viewer 面板 `h`/`l` 左右滚动、`0` 重置到行首，ANSI-aware 偏移保留颜色，footer 显示 `[col +N]` |
| **Chat 滚动** | Chat 面板 `Ctrl+U`/`PgUp` 向上翻半页、`Ctrl+D`/`PgDn` 向下翻半页。用户滚动上方时暂停自动滚动，header 显示 `SCROLLED` 指示器；EventComplete 时恢复自动滚动并跳到底部 |
| **日志导出** | Log Viewer 面板 `s` 键将当前日志导出到 `~/.korthex/logs/{ns}_{pod}_{timestamp}.log` |
| **资源面板联动** | Agent 拥有 `navigate_resource_browser` 专用工具，system prompt 指示它在分析涉及资源时主动调用，导航到最具体可确定层级。`app.go` 拦截该工具的 `EventToolCall` 发射 `NavigateToResourceMsg`（含 Name 光标定位）。取代之前对 get_*/get_pods 等工具的机械映射 |
| **Log Viewer buffer 感知** | Agent 拥有 `get_log_viewer_state` 和 `search_visible_logs` 两个工具，可读取 Log Viewer 已加载的日志。`LogBufferReader` 接口在 agent 层定义，`*ui.RingBuffer` 隐式满足。通过 `SetLogBufferReader()` 后期注入。System prompt 指示 AI 在调 kubectl_logs 前先检查 buffer 状态 |
| **深度分析提示** | system prompt 明确要求 AI 做深度 root cause 分析，禁止说 "你可以在 Log Viewer 查看" 等甩锅话术 |

## Phase 2 Implementation Status

Phase 2 design complete. Scope: [Phase 2 PRD](./docs/superpowers/specs/2026-04-24-phase2-prd-design.md). Three capability domains:

| Domain | Key Features | New Modules |
|--------|-------------|-------------|
| **Data Safety** | 正则脱敏引擎 (6 内置规则 + 自定义), 日志书签 | `pkg/redact` (Layer 0) |
| **Enhanced Browser** | StatefulSet/DaemonSet/Job/CronJob, Pod 详情面板, Resource Registry | k8s 扩展 |
| **Deep Analysis** | AI 分析增强 (severity_stats, compare_logs, trace_logs), 对话历史 (SQLite + FTS5), Markdown 渲染 (glamour) | `internal/history` (Layer 1) |

## License

Apache 2.0 — 与 K8s 生态一致，企业友好。所有依赖 License 兼容性见 `NOTICE` 文件。
