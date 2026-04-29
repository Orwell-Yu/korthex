# Korthex - Technical Specification

> **Korthex** = K8s + Cortex — The Intelligent Brain of Your Kubernetes Cluster
>
> Technical Spec for Phase 1: Log Intelligence

**Version:** v0.1
**Date:** 2026-04-22
**Based on:** [PRD v0.1](./PRD.md)

---

## Document Hierarchy

```
CLAUDE.md          ← 项目规则和架构约束 (开发前必读)
  ├── SPEC.md      ← 本文档: 技术规划 (架构、接口、开发计划)
  ├── PRD.md       ← 产品需求文档 (功能规格、用户场景)
  │
  ├── internal/config/
  │   ├── CLAUDE.md  ← 模块规则 (Layer 0, 无依赖)
  │   └── README.md  ← 接口/文件/测试 (详细参考)
  ├── pkg/logparse/
  │   ├── CLAUDE.md  ← 模块规则 (Layer 0, 纯 stdlib)
  │   └── README.md
  ├── internal/k8s/
  │   ├── CLAUDE.md  ← 模块规则 (Layer 1, client-go 封装)
  │   └── README.md
  ├── internal/llm/
  │   ├── CLAUDE.md  ← 模块规则 (Layer 1, SDK adapter)
  │   └── README.md
  ├── internal/agent/
  │   ├── CLAUDE.md  ← 模块规则 (Layer 2, Agentic Loop)
  │   └── README.md
  ├── internal/ui/
  │   ├── CLAUDE.md  ← 模块规则 (Layer 3, Bubble Tea)
  │   └── README.md
  └── internal/app/
      ├── CLAUDE.md  ← 模块规则 (Layer 4, Composition Root)
      └── README.md
```

**How to navigate:** CLAUDE.md (rules) → SPEC.md (architecture) → Module CLAUDE.md (module rules) → Module README (implementation details) → PRD.md (product context)

---

## Table of Contents

1. [Technology Maturity Assessment](#1-technology-maturity-assessment)
2. [Architecture & Module Design](#2-architecture--module-design)
3. [Key Interface Contracts](#3-key-interface-contracts)
4. [Critical Implementation Details](#4-critical-implementation-details)
   - 4.1 Agentic Loop | 4.2 Label Selector Discovery | 4.3 Log Streaming Pipeline
   - 4.4 TUI Keyboard Routing | 4.5 Setup Wizard | **4.6 Message Flow Diagram (async pipelines)**
5. [Extensibility Design](#5-extensibility-design)
6. [Coding Standards & Development Harness](#6-coding-standards--development-harness)
   - 6.1-6.7 Lint/Test/CI/Makefile | **6.8 Logging Strategy** | **6.9 Go Version Policy**
7. [Development Order & Parallelism](#7-development-order--parallelism)
8. [Risk Assessment & Mitigations](#8-risk-assessment--mitigations)
9. [Phase 2 Architecture Deltas](#9-phase-2-architecture-deltas)
   - 9.1 New Modules | 9.2 Updated Layer Diagram | 9.3 New Interfaces
   - 9.4 New Agent Tools | 9.5 New Dependencies | 9.6 Config | 9.7 Import Rules

---

## 1. Technology Maturity Assessment

### 1.1 Core Tech Stack Maturity

| Technology | Maturity | Stars/Version | Risk Level | Notes |
|------------|----------|---------------|------------|-------|
| **Go 1.24** | Production | - | Very Low | K8s 生态主力语言，range over func 支持，client-go v0.30 兼容，bubbletea v1.3+ / genai v1.54+ 要求 1.24 |
| **Bubble Tea** | Production | 25k+ stars | Low | charmbracelet 维护，Elm 架构，组件化 |
| **Lipgloss** | Production | 8k+ stars | Low | Bubble Tea 配套样式库 |
| **Bubbles** | Production | 5k+ stars | Low | Table/Viewport/TextInput 等预制组件 |
| **client-go** | Production | Official K8s SDK | Very Low | K8s 官方 Go client，kubectl/k9s 同款 |
| **OpenAI Go SDK** | Production | v3, Official | Low | 2024 年起官方维护 |
| **Anthropic Go SDK** | Production | v1.37+, Official | Low | 官方维护，类型安全 |
| **Gemini Go SDK** | Production | GA 2025, Official | Low | `google.golang.org/genai`，最新 GA |
| **Viper** | Production | 27k+ stars | Very Low | Go 标准配置库 |
| **cenkalti/backoff** | Production | 5k+ stars | Very Low | k9s 同款指数退避 |
| **sahilm/fuzzy** | Stable | 400+ stars | Low | k9s 同款模糊搜索 |

### 1.2 Open Source Reference Projects

| Project | What to Learn | Risk of Reference |
|---------|---------------|-------------------|
| **k9s** (33k stars) | Informer/Cache 架构、日志流管道模式、Ring Buffer、资源导航 UX | Very Low — 同 License (Apache-2.0)，Go 同语言 |
| **kubectl-ai** (7.2k stars) | Agentic Loop 设计、多 LLM Provider 支持、MCP Server 模式 | Low — Apache-2.0，同语言 |
| **k8sgpt** (7.5k stars) | Analyzer 架构、CNCF 最佳实践、Provider 抽象 | Low — Apache-2.0 |
| **Gonzo** (2.5k stars) | 日志可视化（热力图、severity 分布）、TUI + AI 结合 | Low — MIT |

### 1.3 Technology Verdict

**所有核心依赖均为 Production-Ready、官方维护。** Go + Bubble Tea + client-go 的组合在 K8s TUI 领域已被 k9s/lazygit 等项目验证。三个 LLM SDK 均为官方出品、支持 Tool-Use / Function-Calling。技术栈难度中等，主要挑战在于 Bubble Tea 的 Elm 架构学习曲线和 client-go Informer 模式的理解，而非技术栈本身的不成熟。

---

## 2. Architecture & Module Design

### 2.1 Dependency Layer Architecture

```
Layer 4 (Integration):    cmd/korthex + internal/app
                               │ wires everything
                               ▼
Layer 3 (Presentation):   internal/ui
                               │ consumes interfaces
                               ▼
Layer 2 (Intelligence):   internal/agent
                               │ orchestrates
                          ┌────┴────┐
                          ▼         ▼
Layer 1 (Core):      internal/k8s  internal/llm
                          │         │
                          ▼         ▼
Layer 0 (Foundation): internal/config    pkg/logparse
                      (zero deps)        (zero deps)
```

**Import Rules (enforced by code review):**
- Layer N 只能 import Layer < N 的包
- 同 Layer 的包之间**禁止互相 import**（k8s 和 llm 不互相依赖）
- `pkg/logparse` 是唯一的 public package，其他全部在 `internal/`
- 只有 `cmd/korthex/main.go` 和 `internal/app/app.go` import 具体实现；其他模块只 import 接口

### 2.2 Module Responsibilities

| Module | Package | Responsibility | Dependencies |
|--------|---------|---------------|--------------|
| **config** | `internal/config` | 配置加载/验证/持久化、Setup Wizard | 无 (stdlib + Viper) |
| **logparse** | `pkg/logparse` | 日志行解析：timestamp/severity/JSON 检测 | 无 (纯 stdlib) |
| **k8s** | `internal/k8s` | K8s 集群交互：Informer/Cache、日志流、事件、Describe | config |
| **llm** | `internal/llm` | 多 Provider LLM 通信：统一接口 + SDK 适配器 | config |
| **agent** | `internal/agent` | Agentic Loop：自然语言 → Tool 调用 → 结果反馈 → 迭代 | k8s, llm, logparse |
| **ui** | `internal/ui` | Bubble Tea TUI：面板渲染、键盘路由、布局管理 | agent, k8s, config, logparse |
| **app** | `internal/app` + `cmd/korthex` | 依赖注入、生命周期管理、CLI 入口 | 全部 |

### 2.3 Complete File Tree

```
korthex/
├── cmd/
│   └── korthex/
│       └── main.go                     # CLI 入口, flag 解析, 依赖注入, tea.Program 启动
│
├── internal/
│   ├── app/
│   │   ├── app.go                      # Application 生命周期, graceful shutdown
│   │   └── README.md
│   │
│   ├── config/
│   │   ├── config.go                   # Config 结构体, Manager (Viper load/save/validate)
│   │   ├── wizard.go                   # 首次运行 Setup Wizard (Bubble Tea mini-program)
│   │   ├── config_test.go              # Table-driven tests: load/validate/env-override
│   │   └── README.md
│   │
│   ├── k8s/
│   │   ├── client.go                   # Client 实现, kubeconfig 加载, rest.Config
│   │   ├── informer.go                 # Informer factory 管理: per-ns, LRU 驱逐, 10min resync
│   │   ├── resources.go                # ResourceLister: namespace/deployment/pod 从 cache 读取
│   │   ├── logs.go                     # LogStreamer: GetLogs, StreamLogs, StreamMultiPodLogs
│   │   ├── events.go                   # EventLister: 事件列表/过滤
│   │   ├── describe.go                 # ResourceDescriber: 组装 describe 输出
│   │   ├── types.go                    # Namespace, Deployment, Pod, Container, Event 结构体
│   │   ├── client_test.go              # Integration tests (fake clientset)
│   │   ├── logs_test.go                # Log streaming tests with fake reader
│   │   └── README.md
│   │
│   ├── llm/
│   │   ├── provider.go                 # Provider 接口, Message, ToolCall, StreamDelta 类型
│   │   ├── registry.go                 # Registry: provider name → constructor
│   │   ├── openai.go                   # OpenAI 适配器 (含 Custom endpoint 支持)
│   │   ├── anthropic.go                # Anthropic 适配器
│   │   ├── gemini.go                   # Gemini 适配器
│   │   ├── prompt.go                   # System prompt 模板 (含集群上下文)
│   │   ├── errors.go                   # 标准化错误类型: RateLimit, Auth, Timeout
│   │   ├── openai_test.go              # Mock HTTP server tests
│   │   ├── anthropic_test.go
│   │   ├── gemini_test.go
│   │   └── README.md
│   │
│   ├── agent/
│   │   ├── agent.go                    # Agentic Loop 核心: 迭代(LLM → Tool → 反馈)
│   │   ├── tools.go                    # ToolExecutor: tool name → k8s 操作映射
│   │   ├── safety.go                   # 安全白名单检查 (Phase 1 只读)
│   │   ├── history.go                  # 对话历史管理: FIFO 截断, tool result 压缩
│   │   ├── prompt.go                   # System prompt 构建器 (ClusterContext 注入)
│   │   ├── agent_test.go               # Agentic loop tests (mock LLM + mock K8s)
│   │   ├── tools_test.go               # Tool 执行 tests
│   │   └── README.md
│   │
│   └── ui/
│       ├── app.go                      # Root AppModel: Init/Update/View, focus 路由, layout 切换
│       ├── resource.go                 # ResourceModel: 层级列表导航 (ns→deploy→pod)
│       ├── logviewer.go                # LogViewerModel: viewport, severity 着色, 搜索, follow
│       ├── chat.go                     # ChatModel: 输入框, 消息历史, agent 集成
│       ├── statusbar.go                # StatusBarModel: context/namespace/provider 状态
│       ├── styles.go                   # Lipgloss 主题: dark/light/dracula/nord, severity 色彩
│       ├── layout.go                   # Layout 计算器: 面板尺寸 by mode
│       ├── ringbuffer.go               # Thread-safe Ring Buffer (mutex-based)
│       ├── help.go                     # Help overlay (快捷键参考)
│       ├── messages.go                 # 自定义 tea.Msg 类型定义
│       ├── ringbuffer_test.go          # Ring buffer 并发访问 tests
│       └── README.md
│
├── pkg/
│   └── logparse/
│       ├── parser.go                   # LogEntry 结构体, Parser 实现, Size()
│       ├── severity.go                 # Severity 检测 (编译后正则表)
│       ├── json.go                     # JSON 日志检测 + 格式化
│       ├── timestamp.go                # 多格式时间戳解析
│       ├── parser_test.go              # Table-driven parse tests
│       ├── severity_test.go
│       ├── timestamp_test.go
│       └── README.md
│
├── configs/
│   └── default.yaml                    # 默认配置模板
│
├── .github/
│   └── workflows/
│       ├── ci.yml                      # golangci-lint + go test + go build
│       └── release.yml                 # GoReleaser on tag push
│
├── .golangci.yml                       # Linter 配置
├── .goreleaser.yml                     # Release 配置
├── Makefile                            # build, test, lint, fmt, run
├── NOTICE                              # 第三方 License 声明
├── LICENSE                             # Apache 2.0
├── SPEC.md                             # 本文档
├── PRD.md                              # 产品需求文档
├── go.mod
├── go.sum
└── README.md
```

---

## 3. Key Interface Contracts

> 接口是并行开发的核心。所有模块通过接口通信，不直接依赖具体实现。开发第一天必须冻结接口定义。

### 3.1 Config Module (`internal/config`)

> Module docs: [CLAUDE.md](./internal/config/CLAUDE.md) | [README.md](./internal/config/README.md)

```go
// ==================== internal/config/config.go ====================

type Config struct {
    Kubernetes KubernetesConfig
    LLM        LLMConfig
    Agent      AgentConfig
    UI         UIConfig
}

type KubernetesConfig struct {
    Kubeconfig     string   // path, e.g. ~/.kube/config (KUBECONFIG env 支持冒号分隔多路径，取第一个)
    DefaultContext string
}

type LLMConfig struct {
    Provider    string   // "openai" | "anthropic" | "gemini" | "custom"
    APIKey      string   // 优先从环境变量读取 (KORTHEX_LLM_API_KEY)
    Model       string
    BaseURL     string   // for custom OpenAI-compatible endpoints
    Temperature float64
    MaxTokens   int
    SendLogs    bool     // false = AI 仅生成命令，不接收日志原文
}

type AgentConfig struct {
    MaxIterations   int  // Agentic Loop 最大迭代次数 (-1 = unlimited, default: -1)
    MaxHistoryTurns int  // 对话上下文保留轮数 (default: 20)
}

type UIConfig struct {
    Theme           string // dark | light | dracula | nord
    LogLinesLimit   int    // Ring Buffer 最大行数 (default: 10000)
    LogPageSize     int    // 每屏日志行数 (default: 1000)
    DefaultLogSince string // 默认时间范围 (default: "1h")
}

// Manager 提供配置的加载/保存/验证能力
type Manager interface {
    Load() (*Config, error)
    Save(cfg *Config) error
    Validate(cfg *Config) error
    ConfigPath() string
}

// ==================== internal/config/wizard.go ====================

// Wizard 驱动首次运行的配置向导
type Wizard interface {
    Run(detected KubeDetection) (*Config, error)
    NeedsSetup() bool
}

type KubeDetection struct {
    KubeconfigPath string
    Contexts       []string
    CurrentContext string
}
```

**环境变量优先级 (高 → 低):**
1. `KORTHEX_LLM_API_KEY` 或 provider 特定变量 (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`)
2. `~/.config/korthex/config.yaml` 中的 `llm.api_key` 字段
3. 无 key → Setup Wizard 提示输入

### 3.2 LogParse Module (`pkg/logparse`)

> Module docs: [CLAUDE.md](./pkg/logparse/CLAUDE.md) | [README.md](./pkg/logparse/README.md)

```go
// ==================== pkg/logparse/parser.go ====================

type Severity int
const (
    SeverityUnknown Severity = iota
    SeverityDebug
    SeverityInfo
    SeverityWarn
    SeverityError
    SeverityFatal
)

type LogEntry struct {
    Timestamp  time.Time  // zero value if unparsable
    Severity   Severity
    PodName    string
    Container  string
    Raw        string     // 原始行 (永不修改)
    IsJSON     bool
    JSONPretty string     // 格式化的 JSON (仅 IsJSON=true 时有值)
}

// Parser 是无状态的日志解析器，可安全并发使用
type Parser interface {
    Parse(podName, container, rawLine string) LogEntry
}

// Size 返回 LogEntry 的近似内存占用 (bytes)
func (e *LogEntry) Size() int

// ==================== pkg/logparse/severity.go ====================
func DetectSeverity(line string) Severity

// ==================== pkg/logparse/json.go ====================
func IsJSONLine(line string) bool
func FormatJSON(line string) string

// ==================== pkg/logparse/timestamp.go ====================
// 支持: ISO 8601, RFC 3339, Java (yyyy-MM-dd HH:mm:ss.SSS), Go default, kubelet --timestamps
func ParseTimestamp(line string) time.Time
```

### 3.3 K8s Module (`internal/k8s`)

> Module docs: [CLAUDE.md](./internal/k8s/CLAUDE.md) | [README.md](./internal/k8s/README.md)

```go
// ==================== internal/k8s/client.go ====================

// Client 是顶层 K8s 接口，聚合所有子接口
type Client interface {
    Connect(kubeconfig, context string) error
    Disconnect()
    IsConnected() bool
    CurrentContext() string

    Resources() ResourceLister   // 资源发现 (from Informer cache)
    Logs() LogStreamer            // 日志操作 (direct API)
    Events() EventLister          // 事件查询
    Describer() ResourceDescriber // Describe 聚合
}

// ==================== internal/k8s/types.go ====================

type Namespace struct {
    Name   string
    Status string
}

type Deployment struct {
    Name      string
    Namespace string
    Replicas  int32
    Ready     int32
    Available int32
    Labels    map[string]string
    Selector  map[string]string  // .spec.selector.matchLabels
}

type Pod struct {
    Name       string
    Namespace  string
    Status     string   // Running, Pending, CrashLoopBackOff, etc.
    Restarts   int32
    Age        time.Duration
    Containers []Container
    Labels     map[string]string
    NodeName   string
}

type Container struct {
    Name  string
    Ready bool
    State string // running, waiting, terminated
}

type Event struct {
    Type      string // Normal, Warning
    Reason    string
    Message   string
    Object    string // e.g., "Pod/order-svc-abc12"
    Count     int32
    FirstSeen time.Time
    LastSeen  time.Time
}

// ==================== internal/k8s/resources.go ====================

type ResourceLister interface {
    ListNamespaces() ([]Namespace, error)
    ListDeployments(namespace string) ([]Deployment, error)
    ListPods(namespace string) ([]Pod, error)
    ListPodsBySelector(namespace string, selector map[string]string) ([]Pod, error)
    FindDeploymentByName(namespace, name string) (*Deployment, error)
    SearchResources(namespace, query string) ([]SearchResult, error)
}

type SearchResult struct {
    Kind      string // "Deployment", "Pod", etc.
    Name      string
    Namespace string
}

// ==================== internal/k8s/logs.go ====================

type LogRequest struct {
    Namespace     string
    PodName       string
    Container     string        // empty = all containers
    Since         time.Duration // 0 = no time filter
    SinceTime     *time.Time
    TailLines     *int64
    Previous      bool
    AddTimestamps bool
}

type LogLine struct {
    PodName   string
    Container string
    Content   string
}

type LogStreamer interface {
    // StreamLogs 打开流式连接，将日志行发送到 channel
    // 阻塞直到 ctx 取消或流结束
    StreamLogs(ctx context.Context, req LogRequest, ch chan<- LogLine) error

    // GetLogs 批量获取日志 (非流式)
    GetLogs(ctx context.Context, req LogRequest) ([]LogLine, error)

    // StreamMultiPodLogs 流式聚合 selector 匹配的所有 Pod 日志
    // 每个容器独立 goroutine，合并到同一 channel
    StreamMultiPodLogs(ctx context.Context, namespace string,
        selector map[string]string, opts LogRequest, ch chan<- LogLine) error
}

// ==================== internal/k8s/events.go ====================

type EventLister interface {
    ListEvents(namespace string) ([]Event, error)
    ListEventsForResource(namespace, kind, name string) ([]Event, error)
}

// ==================== internal/k8s/describe.go ====================

type ResourceDescriber interface {
    Describe(namespace, kind, name string) (string, error)
}
```

**设计原则:**
- 所有返回类型是 Korthex 内部结构体，不暴露 client-go 类型
- ResourceLister 从 Informer cache 读取，零 API 调用
- LogStreamer 使用 client-go `GetLogs().Stream()` 流式读取
- `SearchResources` 支持 fuzzy search，赋能 Agent 的 label selector 智能发现

### 3.4 LLM Module (`internal/llm`)

> Module docs: [CLAUDE.md](./internal/llm/CLAUDE.md) | [README.md](./internal/llm/README.md)

```go
// ==================== internal/llm/provider.go ====================

type Role string
const (
    RoleSystem    Role = "system"
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
)

// Message 是跨 Provider 的统一消息格式
type Message struct {
    Role       Role
    Content    string       // 文本内容 (ToolCalls 存在时可能为空)
    ToolCalls  []ToolCall   // assistant 请求 tool 执行
    ToolCallID string       // RoleTool: 对应哪个 ToolCall
    Name       string       // RoleTool: tool 名称
}

type ToolCall struct {
    ID        string
    Name      string
    Arguments map[string]string  // Phase 1: 所有 7 个 tool 的参数均为 string 类型
    RawArgs   string             // provider 原始 JSON — 保留用于 Phase 2+ 扩展 (见下方设计说明)
}
```

**ToolCall.Arguments 类型设计说明:**

Phase 1 的 7 个 tool 参数全是 string (namespace, podName, labelSelector, since, grepPattern 等)，
`map[string]string` 完全够用。但 Phase 3 扩展 (scale replicas=int, filter 对象等) 时 string 可能不足。

设计决策：
- **Phase 1 保持 `map[string]string`** — 简单、类型安全、无序列化开销
- **`RawArgs` 保留 provider 原始 JSON** — 当 `Arguments` 无法表达复杂类型时，消费者可 fallback 解析 `RawArgs`
- **Phase 3 如需变更** — 可将 `Arguments` 改为 `map[string]any`，但 Phase 1 不做预优化
- **Gemini 适配器注意** — Gemini SDK 返回 `map[string]any`，adapter 中需逐字段 `fmt.Sprintf("%v", v)` 转为 string，同时将原始 map JSON 序列化存入 `RawArgs`

type ToolDefinition struct {
    Name        string
    Description string
    Parameters  []ParameterDef
}

type ParameterDef struct {
    Name        string
    Type        string   // "string", "integer", "boolean"
    Description string
    Required    bool
    Enum        []string // optional
}

// StreamDelta 代表流式响应的增量片段
type StreamDelta struct {
    Content    string
    ToolCall   *ToolCall
    Done       bool
    StopReason string // "end_turn", "tool_use", etc.
}

// Provider 是 LLM 通信的统一抽象
type Provider interface {
    Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error)
    ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, ch chan<- StreamDelta) error
    ModelName() string
    ProviderName() string
    ValidateConnection(ctx context.Context) error
}

// ==================== internal/llm/registry.go ====================

type Registry interface {
    Create(cfg config.LLMConfig) (Provider, error)
    SupportedProviders() []string
}
```

**Provider 适配器职责:**
每个 provider adapter (`openai.go`, `anthropic.go`, `gemini.go`) 负责:
1. 将 `[]ParameterDef` 转换为 provider 原生的 JSON Schema / InputSchema / genai.Schema
2. 将 `[]Message` 转换为 provider 的消息格式（注：Anthropic 的 system prompt 与 messages 分离）
3. 将 provider 的 response 转换回统一 `Message` / `ToolCall`
4. 将 provider 特定的 error 映射为标准化 error (`ErrRateLimit`, `ErrAuth`, `ErrTimeout`)

### 3.5 Agent Module (`internal/agent`)

> Module docs: [CLAUDE.md](./internal/agent/CLAUDE.md) | [README.md](./internal/agent/README.md)

```go
// ==================== internal/agent/agent.go ====================

// Agent 是 AI 对话的核心编排器
type Agent interface {
    Execute(ctx context.Context, userQuery string, ch chan<- AgentEvent) error
    ClearHistory()
    SetClusterContext(ctx ClusterContext)
}

type ClusterContext struct {
    ContextName string
    Namespace   string   // 当前聚焦的 namespace
    Deployments []string // 当前 ns 已知的 deployment 列表
}

// AgentEvent 是 UI 消费的事件流
type AgentEvent struct {
    Type           AgentEventType
    Iteration      int
    MaxIter        int

    // 按 Type 填充:
    Text           string            // EventText, EventSummary, EventError
    ToolName       string            // EventToolCall, EventToolResult
    ToolArgs       map[string]string // EventToolCall
    ToolResult     string            // EventToolResult (可能截断)
    CommandDisplay string            // EventToolCall: 人类可读的 kubectl 等价命令
    LogLines       []k8s.LogLine     // EventLogsReady: 供 Log Viewer 展示
    StreamDelta    string            // EventStreamDelta: LLM 增量文本
}

type AgentEventType int
const (
    EventStreamDelta AgentEventType = iota  // LLM 增量文本
    EventToolCall                            // Agent 正在调用 tool
    EventToolResult                          // Tool 返回结果
    EventLogsReady                           // 日志行就绪，可写入 Log Viewer
    EventSummary                             // 最终分析摘要
    EventError                               // 错误 (可能触发重试)
    EventComplete                            // Agentic loop 完成
)

// ==================== internal/agent/tools.go ====================

type ToolExecutor interface {
    ExecuteTool(ctx context.Context, name string, args map[string]string) (result string, logs []k8s.LogLine, err error)
    ToolDefinitions() []llm.ToolDefinition
}

// ==================== internal/agent/safety.go ====================

// SafetyLevel 表示操作的安全级别
type SafetyLevel int
const (
    SafetyAllowed   SafetyLevel = iota  // Phase 1: 直接执行 (get, list, logs, describe, events)
    SafetyDangerous                      // Phase 3: 三次确认 (scale, rollout, restart)
    SafetyCritical                       // Phase 3: 三次确认 + 输入资源名 (delete, drain)
    SafetyDenied                         // 当前 Phase 不支持
)

type SafetyChecker interface {
    Check(toolName string, args map[string]string) (level SafetyLevel, reason string)
}
```

**SafetyLevel 设计说明:**

Phase 1 中 `Check()` 只返回 `SafetyAllowed` (7 个只读 tool) 或 `SafetyDenied` (其他一切)。
Phase 3 引入 `SafetyDangerous` 和 `SafetyCritical` 时，agent 和 ui 不需要改接口 —
agent 根据 level 决定是直接执行还是要求确认，ui 根据 level 渲染不同的确认 UI。

### 3.6 UI Module (`internal/ui`)

> Module docs: [CLAUDE.md](./internal/ui/CLAUDE.md) | [README.md](./internal/ui/README.md)

```go
// ==================== internal/ui/app.go ====================

type PanelID int
const (
    PanelResource  PanelID = iota
    PanelLogViewer
    PanelChat
)

type LayoutMode int
const (
    LayoutFull      LayoutMode = iota  // 三面板: resource | logs / chat
    LayoutChatFocus                     // chat 主导, logs 折叠
    LayoutLogFocus                      // 全屏 logs
)

// AppModel 是根 Bubble Tea Model，拥有所有子面板
type AppModel struct {
    resource   ResourceModel
    logviewer  LogViewerModel
    chat       ChatModel
    statusbar  StatusBarModel

    focus      PanelID
    layout     LayoutMode
    width      int
    height     int

    agent      agent.Agent    // 依赖注入
    k8sClient  k8s.Client
    config     *config.Config
}

func NewAppModel(a agent.Agent, k k8s.Client, cfg *config.Config) AppModel
func (m AppModel) Init() tea.Cmd
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m AppModel) View() string

// ==================== internal/ui/messages.go ====================

type AgentEventMsg agent.AgentEvent     // Agent 事件
type NavigateToLogsMsg struct { ... }    // 导航到日志
type LogsLoadedMsg struct { ... }        // 日志加载完成
type ClusterConnectedMsg struct { ... }  // 集群连接完成
type ErrorMsg struct { Err error }       // 错误

// ==================== internal/ui/ringbuffer.go ====================

// RingBuffer 是 thread-safe 的有界日志缓冲区
type RingBuffer struct { ... }

func NewRingBuffer(capacity int) *RingBuffer
func (rb *RingBuffer) Append(entry logparse.LogEntry)   // 满时淘汰最老条目
func (rb *RingBuffer) Slice() []logparse.LogEntry        // Clone on Read
func (rb *RingBuffer) Len() int
func (rb *RingBuffer) Clear()
```

### 3.7 Phase 1 Tool Definitions

| Tool Name | Parameters | K8s API | Returns |
|-----------|-----------|---------|---------|
| `kubectl_get_namespaces` | filter (opt) | Informer Cache | namespace 名 + 状态列表 (支持名称子串过滤) |
| `kubectl_get_deployments` | namespace (required), filter (opt) | Informer Cache | deployment 列表 (含 replica 数, 支持名称子串过滤) |
| `kubectl_get_pods` | namespace (req), labelSelector (opt), filter (opt) | Informer Cache | pod 列表 (含 status/restarts/age, 支持名称子串过滤) |
| `kubectl_logs` | namespace (req), podName (req), container (opt), since (opt), sinceTime (opt), tailLines (opt), previous (opt), grepPattern (opt) | `GetLogs().Stream()` | 日志文本 (含 grep 过滤) |
| `kubectl_logs_selector` | namespace (req), labelSelector (req), since (opt), sinceTime (opt), tailLines (opt), previous (opt), grepPattern (opt) | 查 pod + GetLogs | 多 Pod 聚合日志文本 |
| `kubectl_describe` | namespace (req), kind (req), name (req) | Get + List Events | Describe 格式输出 |
| `kubectl_get_events` | namespace (req), fieldSelector (opt) | `Events().List()` | 事件列表 |

> `grepPattern` 在 Go 端 client-side 实现（非 server-side），支持正则。
> `filter` 是 client-side 名称子串匹配（大小写不敏感），用于大集群场景避免全量列表超出 context window。

**Log Viewer 联动:** `kubectl_logs` 和 `kubectl_logs_selector` 返回的 `[]k8s.LogLine` 通过 `EventLogsReady` 自动推送到 TUI Log Viewer 面板。System prompt (`prompt.go`) 明确告知 LLM 此行为，指示 LLM 不要在 Chat 中粘贴日志原文，而是调用 log 工具让用户在 Log Viewer 中查看。

---

## 4. Critical Implementation Details

### 4.1 Agentic Loop (核心算法)

```
Execute(ctx, userQuery, ch):
    appendUserMessage(userQuery)

    for iteration = 1 to maxIterations:

        // 1. 调用 LLM (携带完整历史 + tool definitions)
        response = llm.Chat(ctx, history, toolDefinitions)

        // 2. 无 ToolCalls → LLM 完成推理
        if len(response.ToolCalls) == 0:
            emit(EventSummary, response.Content)
            emit(EventComplete)
            appendAssistantMessage(response)
            return

        // 3. 执行每个 ToolCall
        appendAssistantMessage(response)
        for each tc in response.ToolCalls:

            // 安全检查
            // 安全检查
            level, reason = safety.Check(tc.Name, tc.Args)
            if level == SafetyDenied:
                emit(EventError, "denied: " + reason)
                appendToolResult(tc.ID, "DENIED: " + reason)
                continue

            emit(EventToolCall, tc.Name, tc.Args, toKubectlString(tc))

            result, logs, err = tools.ExecuteTool(ctx, tc.Name, tc.Args)
            if err:
                emit(EventError, err.Error())
                appendToolResult(tc.ID, "ERROR: " + err.Error())
                continue

            // 日志行写入 Log Viewer
            if len(logs) > 0:
                emit(EventLogsReady, logs)

            // 按 send_logs 配置决定发给 LLM 的内容
            llmResult = result
            if not sendLogs and len(logs) > 0:
                llmResult = fmt.Sprintf("Success: %d log lines returned", len(logs))

            emit(EventToolResult, tc.Name, truncate(llmResult))
            appendToolResult(tc.ID, llmResult)

        // 循环继续: LLM 看到 tool results 后决定下一步

    // 超过最大迭代
    emit(EventError, "Max iterations reached")
    emit(EventComplete)
```

### 4.2 Label Selector 智能发现 (agent/tools.go)

当用户说 "order-service" 而非给出精确 label:

```
1. FindDeploymentByName(ns, "order-service")
   ├── 找到 → 提取 Deployment.Selector (e.g., {"app": "order-service"})
   │          用 selector 查日志: StreamMultiPodLogs(ns, selector, ...)
   │
   └── 未找到 → SearchResources(ns, "order-service")  [fuzzy search]
               ├── 有候选 → 返回候选列表让 LLM 在下次迭代中让用户选择
               └── 无候选 → 返回 "No matching resources found" 让 LLM 修正
```

### 4.3 日志流管道 (k8s → agent → ui)

```
[K8s API Server]
       │
       │ io.ReadCloser per container
       ▼
[k8s/logs.go: StreamMultiPodLogs]
  ● 每个容器独立 goroutine
  ● bufio.Scanner → buffered channel (cap=50)
  ● Non-blocking send: select { case ch<-line: default: DROP + warn }
  ● ctx.Done() → close reader, exit goroutine
       │
       │ chan LogLine (merged from all containers)
       ▼
[agent/tools.go: ExecuteTool]
  ● 收集 []LogLine (bounded by LogLinesLimit)
  ● 序列化为 string 给 LLM (>2000 字符截断)
  ● return (resultString, logLines, nil)
       │
       │ AgentEvent{Type: EventLogsReady, LogLines: [...]}
       ▼
[ui/app.go: Update(AgentEventMsg)]
  ● → logviewer.Update(LogsLoadedMsg{...})
  ● → chat.Update(AgentEventMsg)  // 显示 tool 调用
       │
       ▼
[ui/logviewer.go]
  ● logparse.Parser.Parse() → LogEntry
  ● → RingBuffer.Append() (满时 shift oldest)
  ● → 重建 viewport 内容 (severity 着色)
       │
       ▼
[ui/ringbuffer.go]
  ● mutex.Lock → append → if len > cap: shift → mutex.Unlock
  ● Slice() clone-on-read 给 viewport 渲染
```

**手动浏览同管道**: 按 `l` 键 → `NavigateToLogsMsg` → `k8s.Logs().StreamLogs(ctx, req, ch)` → 同一 Ring Buffer + Viewport

### 4.4 TUI 键盘路由

**信号处理:** 只监听 SIGTERM (不监听 SIGINT)。Bubble Tea 原生处理 Ctrl+C：当 agent 运行中时取消 agent 操作而非退出程序；无 agent 运行时退出 TUI。SIGTERM 通过 `context.WithCancel` 传播到 `App.Run()`，调用 `p.Quit()` 实现优雅退出。

```
Update(msg tea.Msg):
    switch msg.(type):

    case tea.WindowSizeMsg:
        // 1. 始终处理: 重算布局, 传播给所有子面板
        // 注意: 传给子面板的尺寸是 inner (减去 border 的 2)，不是 CalculateLayout 的原始值
        // 这确保 Update 中的 scroll 计算和 View 中的 renderPanel 使用一致的 height

    case tea.KeyMsg:
        // 2. Global 快捷键 (无视 focus)
        switch key:
            "tab"       → cycleFocus(+1)
            "shift+tab" → cycleFocus(-1)
            "f1/f2/f3"  → 切换 LayoutMode
            ":"         → focus = PanelChat (vim-style)
            "?"         → 切换 help overlay
            "q"         → if focus != PanelChat: tea.Quit
            "ctrl+c"    → cancel agent 操作

        // 3. 委托给 focused panel
        switch focus:
            PanelResource:  resource.Update(msg)
            PanelLogViewer: logviewer.Update(msg)
            PanelChat:      chat.Update(msg)

    case AgentEventMsg:
        // 4. 按类型路由 (不按 focus)
        chat.Update(msg)  // 始终给 chat 显示
        if msg.Type == EventLogsReady:
            logviewer.Update(LogsLoadedMsg{...})  // 日志给 logviewer

    case NavigateToLogsMsg:
        // 5. 开始日志流, 切换 focus 到 logviewer
        startLogStream(msg)
        focus = PanelLogViewer
```

### 4.5 Setup Wizard 状态机 (已实现)

```
stepSelectContext
  → 列出 detected.Contexts, j/k 选择, Enter 确认
  → 无 context 时 selectedContext() 返回 ""

stepSelectProvider
  → 4 选项: OpenAI / Anthropic / Gemini / Custom
  → Custom → 额外进入 stepEnterBaseURL

stepEnterAPIKey
  → bubbles.TextInput (EchoMode=Password)
  → 注意: 不检查环境变量，即使 env var 已设置也会要求输入
  → config.go 的 applyEnvOverrides() 在 Load() 时覆盖

stepEnterBaseURL (仅 Custom provider)
  → 自定义 OpenAI 兼容端点 URL

stepSelectModel
  → 显示 provider 默认 model，允许覆盖
  → OpenAI: gpt-4o, Anthropic: claude-sonnet-4-20250514, Gemini: gemini-2.5-flash

stepPrivacyDisclosure
  → 显示隐私声明: 日志数据将发送给 LLM Provider
  → y 确认 / n 取消退出

stepComplete
  → 显示配置摘要 (context, provider, model)
  → Press Enter to start...
```

Wizard 作为独立 `tea.NewProgram(wizardModel)` 运行，完成后返回 `Config` 给 `main.go`。
buildConfig() 从 applyDefaults() 获取 Agent/UI/LLM 的默认值，所有 default.yaml 字段均有覆盖。

### 4.6 Message Flow Diagram (完整 tea.Msg 生命周期)

三条异步管道同时工作时，所有 `tea.Msg` 的产生源、路由路径、消费者如下：

#### 4.6.1 Msg 类型总表

| Msg Type | Source | 产生方式 | 消费者 | Routing Rule |
|----------|--------|---------|--------|-------------|
| `tea.KeyMsg` | Terminal | Bubble Tea 内建 | AppModel → 按 focus 委托 | Global keys 优先, 余下给 focused panel |
| `tea.WindowSizeMsg` | Terminal | Bubble Tea 内建 | AppModel → 全部子面板 | 始终广播给所有面板 |
| `tea.MouseMsg` | Terminal | Bubble Tea 内建 | — | 已移除 `tea.WithMouseAllMotion()`，不再接收鼠标事件（改善 CJK IME 兼容性） |
| `AgentEventMsg` | Agent goroutine | `p.Send()` | AppModel → chat + logviewer | **按类型路由，不按 focus** |
| `LogLineMsg` | K8s log goroutine | `p.Send()` | AppModel → logviewer | 手动浏览场景 |
| `NavigateToLogsMsg` | ResourceModel | `tea.Cmd` return | AppModel | 启动日志流 + 切换 focus |
| `NavigateToResourceMsg` | AppModel | 由 AgentEventMsg(EventToolCall: navigate_resource_browser) 拆分 | ResourceModel | Agent 专用导航工具 → 左侧面板同步到 AI 确定的最具体层级，支持 Name 光标定位 |
| `LogsLoadedMsg` | AppModel 内部 | 由 AgentEventMsg 拆分 | LogViewerModel | 日志行写入 Ring Buffer |
| `ClusterConnectedMsg` | App 启动 | `tea.Cmd` return | ResourceModel | 填充初始 namespace 列表 |
| `InformerUpdateMsg` | Informer callback | `p.Send()` | ResourceModel | 资源列表增量刷新 |
| `ErrorMsg` | 各模块 | `tea.Cmd` return | StatusBarModel + ChatModel | 错误展示 |
| `spinner.TickMsg` | `bubbles/spinner` | `tea.Tick` 调度 | AppModel → ChatModel | 仅当 `isRunning == true` 时路由到 chat，驱动 Thinking 动画帧更新 |
| `agentStartedMsg` | `executeAgent()` | `tea.Cmd` return | AppModel → ChatModel | 设置 `isRunning = true` + 存储 `cancelFunc` |

#### 4.6.2 三条异步管道流转图

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        Bubble Tea Event Loop                            │
│                     (单线程, 串行处理 Msg)                               │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                    AppModel.Update(msg)                         │    │
│  │                                                                 │    │
│  │  KeyMsg ───→ Global? ──Yes──→ Handle (Tab/F1/:/q/?)            │    │
│  │              │                                                  │    │
│  │              No                                                 │    │
│  │              ↓                                                  │    │
│  │         Focused panel.Update(key)                               │    │
│  │              │                                                  │    │
│  │  AgentEventMsg ──→ chat.Update(msg)     ← 始终               │    │
│  │                ├─→ logviewer.Update()    ← if EventLogsReady   │    │
│  │                └─→ statusbar.Update()   ← if EventError       │    │
│  │                                                                 │    │
│  │  LogLineMsg ────→ logviewer.Update()    ← 手动浏览日志流       │    │
│  │                                                                 │    │
│  │  InformerUpdateMsg → resource.Update()  ← 资源列表刷新         │    │
│  │                                                                 │    │
│  │  WindowSizeMsg ──→ ALL panels.Update()  ← 始终广播             │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                            ▲ ▲ ▲                                        │
│                            │ │ │  p.Send(msg)                           │
└────────────────────────────┼─┼─┼────────────────────────────────────────┘
                             │ │ │
          ┌──────────────────┘ │ └──────────────────┐
          │                    │                     │
   ┌──────┴──────┐     ┌──────┴──────┐      ┌──────┴──────┐
   │  Pipeline 1  │     │  Pipeline 2  │      │  Pipeline 3  │
   │  Agent 流式  │     │  日志流      │      │  Informer    │
   │              │     │              │      │              │
   │ tea.Cmd {    │     │ tea.Cmd {    │      │ EventHandler │
   │   go func { │     │   go func { │      │ { p.Send(    │
   │     agent.   │     │     k8s.    │      │   Informer   │
   │     Execute  │     │     Stream  │      │   UpdateMsg) │
   │     (ctx,    │     │     Logs    │      │ }            │
   │      query,  │     │     (ctx,   │      │              │
   │      ch)     │     │      req,   │      │  Triggered   │
   │   }          │     │      ch)    │      │  by Watch    │
   │   go func { │     │   }          │      │  stream from │
   │     for ev   │     │   go func { │      │  API Server  │
   │       :=range│     │     for line│      │              │
   │       ch {   │     │       :=range│     └──────────────┘
   │       p.Send │     │       ch {   │
   │       (Agent │     │       p.Send │
   │       Event  │     │       (Log   │
   │       Msg)   │     │       Line   │
   │     }        │     │       Msg)   │
   │   }          │     │     }        │
   │ }            │     │   }          │
   │              │     │ }            │
   │ Trigger:     │     │ Trigger:     │
   │ User Enter   │     │ Press 'l' on │
   │ in Chat      │     │ Pod / AI     │
   │              │     │ EventLogs    │
   │              │     │ Ready        │
   └──────────────┘     └──────────────┘
```

#### 4.6.3 关键并发约束

| 约束 | 实现方式 | 违反后果 |
|------|---------|---------|
| Model 只在 Update 中修改 | 所有外部事件通过 `p.Send()` 进入 Update | race condition, 数据损坏 |
| 每条管道独立取消 | 各管道持有独立 `context.WithCancel` | Ctrl+C 只取消 agent, 不影响日志流 |
| `p.Send()` 不阻塞 | Bubble Tea 内部 channel 有 buffer | 高频日志流不会卡住 agent 管道 |
| Agent 管道和日志管道可共存 | Agent EventLogsReady 向 logviewer 推日志, 同时手动日志流也在推 | 需要在 logviewer 中合并处理, 不能互相覆盖 |
| Informer 管道低频 | 默认 2 秒刷新, 首次 300ms | 不会冲击 Update 循环 |

#### 4.6.4 p.Send() 获取方式

Bubble Tea 不直接暴露 `p.Send()` 给 Model。标准模式：

```go
// 方式 1: 通过 tea.Cmd 闭包捕获 program 引用
func (m AppModel) Init() tea.Cmd {
    return func() tea.Msg {
        // 在 Init 返回的 Cmd 中, 可以启动后台工作
        // 但无法直接调用 p.Send()
        return initDoneMsg{}
    }
}

// 方式 2 (推荐): 使用 tea.Program 引用 + goroutine
// 在 main.go 中:
p := tea.NewProgram(model)
model.SetProgram(p)  // 注入引用
go p.Run()

// 在需要异步推送的地方:
func startAgentStream(p *tea.Program, agent Agent, query string) {
    ch := make(chan AgentEvent, 16)
    go func() {
        defer close(ch)
        agent.Execute(ctx, query, ch)
    }()
    go func() {
        for ev := range ch {
            p.Send(AgentEventMsg(ev))
        }
    }()
}
```

> **注意**: 方式 2 中 `p.Send()` 是 thread-safe 的，可以从任意 goroutine 调用。

---

## 5. Extensibility Design

### 5.1 LLM Provider 扩展 (Phase 2: Ollama)

```
新增步骤:
1. 创建 internal/llm/ollama.go
2. 实现 Provider 接口 (Chat, ChatStream, ValidateConnection)
3. 在 registry.go 中注册: Register("ollama", NewOllamaProvider)
4. 在 config.go 中 LLMConfig.Provider 枚举增加 "ollama"

影响范围: 仅 llm 包内部，agent/ui/config 无需修改
```

### 5.2 K8s Resource 扩展 (Phase 2: StatefulSet/DaemonSet)

```
新增步骤:
1. 在 k8s/types.go 增加 StatefulSet, DaemonSet 结构体
2. 在 ResourceLister 接口增加 ListStatefulSets(), ListDaemonSets()
3. 在 k8s/resources.go 实现新方法 (从 Informer cache 读取)
4. 在 agent/tools.go 增加对应 tool (kubectl_get_statefulsets 等)
5. 在 ui/resource.go 增加资源类型展示

接口扩展是向后兼容的 (增加方法不影响已有调用者)
```

### 5.3 Agent Tool 扩展 (Phase 3: Write Operations)

```
新增步骤:
1. 在 agent/tools.go 增加新 tool 定义 (kubectl_scale, kubectl_restart, etc.)
2. 在 agent/safety.go 增加 "Dangerous" 和 "Critical" 安全级别
3. 在 ui/chat.go 增加三次确认流程 UI
4. 在 k8s/ 增加对应的 write API 封装

Safety 分级设计已在 Phase 1 预留:
- Safe (Phase 1): 直接执行
- Dangerous (Phase 3): 三次确认
- Critical (Phase 3): 三次确认 + 输入资源名
```

### 5.4 TUI Panel 扩展 (Phase 2: Pod 详情面板)

```
新增步骤:
1. 创建 ui/poddetail.go, 实现 tea.Model 接口
2. 在 ui/app.go 增加 PanelPodDetail 枚举
3. 在 focus cycling 逻辑中增加新面板
4. 在 layout.go 增加新 layout 计算

面板系统基于 Bubble Tea 的组合模式，新面板是独立的 tea.Model
```

### 5.5 Plugin 系统 (Phase 4)

```
Phase 4 可选方向:
- 采用 hashicorp/go-plugin 实现跨进程插件 (gRPC)
- 或采用 Lua/Starlark 嵌入式脚本引擎
- Phase 1-3 的 Registry + Interface 设计为 Phase 4 plugin 奠定基础
```

---

## 6. Coding Standards & Development Harness

### 6.1 Go Project Layout

遵循 [golang-standards/project-layout](https://github.com/golang-standards/project-layout):

| 目录 | 用途 | 规则 |
|------|------|------|
| `cmd/korthex/` | CLI 入口 | main.go 只做依赖注入，不含业务逻辑 |
| `internal/` | 私有包 | 不允许被外部项目 import |
| `pkg/` | 公共包 | 仅 `logparse`，可被外部复用 |
| `configs/` | 配置模板 | 默认 config 文件 |
| `.github/` | CI/CD | GitHub Actions workflows |

### 6.2 Code Style

| 规范 | 工具/方式 | 说明 |
|------|----------|------|
| **Formatting** | `gofmt` / `goimports` | 自动格式化，import 分组 |
| **Linting** | `golangci-lint` | 见 6.3 配置 |
| **Naming** | Go conventions | exported = PascalCase, unexported = camelCase |
| **Error Handling** | `fmt.Errorf("...: %w", err)` | 始终 wrap error with context |
| **Testing** | Table-driven + testify | 见 6.4 |
| **Comments** | exported symbols only | 仅 exported 类型/函数需要 godoc 注释 |
| **Dependencies** | go mod tidy | 每次新增依赖后运行 |

### 6.3 golangci-lint Configuration

```yaml
# .golangci.yml
version: "2"
linters:
  enable:
    - goimports        # import 排序
    - govet            # Go vet 检查
    - errcheck         # 未检查的 error
    - staticcheck      # 静态分析
    - ineffassign      # 无效赋值
    - gosec            # 安全问题
    - unconvert        # 不必要的类型转换
    - misspell         # 拼写检查
    - bodyclose        # HTTP response body 未关闭 (LLM SDK 大量 HTTP)
    - noctx            # HTTP 请求缺少 context (K8s + LLM 双端)
    - exhaustive       # switch 未覆盖所有枚举值 (AgentEventType, Severity, SafetyLevel 等)

linters-settings:
  govet:
    enable:
      - shadow         # 变量 shadowing 检查

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
        - gosec
```

### 6.4 Testing Strategy

| 层级 | 策略 | 工具 |
|------|------|------|
| **Unit Tests** | Table-driven, 每个 module 独立测试 | `testing` + `testify/assert` |
| **Mock** | 接口 mock (不 mock 具体实现) | `testify/mock` 或手写 mock |
| **K8s Tests** | Fake clientset | `client-go/kubernetes/fake` |
| **LLM Tests** | Mock HTTP server (录制回放) | `net/http/httptest` |
| **Integration** | 端到端 (real cluster + real LLM) | `minikube` / `kind` |
| **TUI Tests** | Bubble Tea teatest (headless testing) | `charmbracelet/x/exp/teatest` |

```go
// 示例: Table-driven test
func TestDetectSeverity(t *testing.T) {
    tests := []struct {
        name string
        line string
        want Severity
    }{
        {"error keyword", "2024-01-01 ERROR something failed", SeverityError},
        {"warn keyword", "[WARN] connection slow", SeverityWarn},
        {"info keyword", "INFO: server started", SeverityInfo},
        {"no keyword", "just a plain line", SeverityUnknown},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := DetectSeverity(tt.line)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

### 6.5 Makefile

```makefile
.PHONY: build test lint fmt run clean

BINARY := korthex
BUILD_DIR := bin

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/korthex

test:
	go test -v -race -coverprofile=coverage.out ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .
	goimports -w .

run: build
	./$(BUILD_DIR)/$(BINARY)

clean:
	rm -rf $(BUILD_DIR) coverage.out
```

### 6.6 CI/CD

**GitHub Actions - CI (on every push/PR):**
1. `golangci-lint run`
2. `go test -race ./...`
3. `go build ./cmd/korthex`

**GitHub Actions - Release (on tag push v*):**
1. GoReleaser builds multi-platform binaries (macOS amd64/arm64, Linux amd64/arm64)
2. 自动创建 GitHub Release

### 6.7 Interface-First Development Rule

```
开发新 module 的标准流程:

1. 定义接口 (provider.go / client.go 中的 interface)
2. 编写 mock (基于接口)
3. 编写依赖模块的 tests (使用 mock)
4. 实现接口 (具体实现)
5. 运行 tests 验证

这确保了:
- 接口稳定后可并行开发
- 依赖模块不需要等待实现完成
- 测试覆盖率从第一天就有保障
```

### 6.8 Logging Strategy (TUI 程序调试)

TUI 程序的 stdout/stderr 被 Bubble Tea 占用（渲染 terminal UI），无法使用 `fmt.Println` 或 `log.Println` 调试。必须使用文件日志。

**方案: `log/slog` + 文件输出**

```go
// internal/app/app.go 中初始化
func setupLogging() (*os.File, error) {
    logDir := filepath.Join(os.TempDir(), "korthex")
    os.MkdirAll(logDir, 0755)
    f, err := os.OpenFile(
        filepath.Join(logDir, "korthex.log"),
        os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644,
    )
    if err != nil {
        return nil, err
    }
    handler := slog.NewTextHandler(f, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    })
    slog.SetDefault(slog.New(handler))
    return f, nil
}
```

**使用规范:**

| 场景 | 用法 | Level |
|------|------|-------|
| K8s 连接 / Informer 启停 | `slog.Info("informer started", "namespace", ns)` | Info |
| LLM API 调用 (不含敏感数据) | `slog.Debug("llm.Chat", "provider", name, "tools", len(tools))` | Debug |
| Tool 执行结果 | `slog.Debug("tool executed", "name", name, "lines", count)` | Debug |
| 错误 | `slog.Error("log stream failed", "err", err, "pod", pod)` | Error |
| **禁止** | 不要 log API Key、日志原文、用户输入 | - |

**调试方式:**
```bash
# 开发时在另一个终端 tail 日志
tail -f /tmp/korthex/korthex.log

# 或设置环境变量控制 level
KORTHEX_LOG_LEVEL=debug ./bin/korthex
```

**Production 模式:** 默认 Level=Warn，只有 `KORTHEX_LOG_LEVEL=debug` 时才开启 Debug/Info。

**client-go klog 重定向:**

client-go 使用 klog/v2 写日志，默认输出到 stderr，会污染 TUI 渲染。在 `App.New()` 中初始化 klog flags：
```go
fs := flag.NewFlagSet("klog", flag.ContinueOnError)
klog.InitFlags(fs)
_ = fs.Set("logtostderr", "false")
_ = fs.Set("stderrthreshold", "FATAL")
klog.SetOutput(logFile)  // 重定向到同一日志文件
```
Shutdown 时先 `klog.Flush()` 再关闭日志文件。

### 6.9 Go Version Policy

**Minimum version: Go 1.24** (锁定在 `go.mod`)

| 决策 | 原因 |
|------|------|
| 不选 1.22 | 1.23+ 的 `range over func` 在遍历 Informer cache 和 channel drain 时更清晰 |
| 选 1.24 | bubbletea v1.3+ 和 genai v1.54+ 要求 Go 1.24；client-go v0.30 兼容 1.24 |
| CI 与 go.mod 一致 | `.github/workflows/ci.yml` 和 `go.mod` 都锁 1.24 |

---

## 7. Development Order & Parallelism

### 7.1 Module Dependency Graph (DAG)

```
          config ←─────────── (Layer 0, no deps)
             │
        ┌────┴────┐
        ▼         ▼
       k8s       llm          (Layer 1, depend on config)
        │         │
        └────┬────┘
             ▼
           agent               (Layer 2, depend on k8s + llm)
             │
             ▼
            ui                 (Layer 3, depend on agent + k8s)
             │
             ▼
            app                (Layer 4, wires all)

   logparse (Layer 0, independent of everything)
```

### 7.2 Solo Developer: 8 Sprints

| Sprint | Module | Duration | Entry Criteria | Deliverable |
|--------|--------|----------|----------------|-------------|
| **S0** | Scaffolding | 1 day | None | go.mod, dirs, Makefile, CI, .golangci.yml |
| **S1** | logparse + config | 3-4 days | S0 | `go test ./pkg/logparse/... ./internal/config/...` 通过 |
| **S2** | k8s | 5-7 days | S1 (config) | CLI harness: 连接集群, 列出 ns, 流式 pod 日志到 stdout |
| **S3** | llm | 4-5 days | S1 (config) | CLI harness: 发送 tool-use prompt, 解析 tool call response |
| **S4** | agent | 5-7 days | S2 + S3 | CLI harness: 自然语言查询 → agentic loop → 打印结果 |
| **S5** | ui (shell) | 5-7 days | S2 (k8s) | TUI 启动, 显示 ns, 导航到 pods. 无日志/chat |
| **S6** | ui (logviewer) | 4-5 days | S5 + S2 | 选中 pod → 按 l → 彩色日志流 |
| **S7** | ui (chat) + app | 5-7 days | S4 + S6 | 完整 MVP: NL 查询 → 日志展示 + AI 摘要 |
| **S8** | wizard + polish | 3-4 days | S7 | Setup Wizard, help, 错误处理, 边界场景 |

**Total: ~35-46 days (7-9 weeks)**

> S2 和 S3 可交换顺序（它们互不依赖），但 S4 必须等两者完成。

### 7.3 Two-Three Developers

```
Week 1-2:
  Dev A: S0 + config + k8s          (基础设施 + K8s 能力)
  Dev B: logparse + llm             (解析 + LLM 能力)
  >> Day 1: 全员冻结接口定义 <<

Week 3:
  Dev A: k8s 集成测试, hardening
  Dev B: agent (使用 mock k8s + real llm)
  Dev C: ui/styles + ui/layout + ui/ringbuffer + ui/messages (纯 UI 基础设施)

Week 4-5:
  Dev A: ui/resource + ui/logviewer
  Dev B: agent 集成测试, ui/chat
  Dev C: wizard, help, statusbar

Week 6:
  All: 集成, 端到端测试, polish, README

Total: 5-6 weeks (2 devs), 4-5 weeks (3 devs)
```

### 7.4 Four+ Developers (Maximum Parallelism)

```
Week 1 (Day 1: 接口冻结):
  Dev A: config + config_test
  Dev B: logparse + tests
  Dev C: k8s/types + k8s/client (scaffolding)
  Dev D: llm/provider + llm/registry + llm/errors (接口 + 类型)

Week 1-2:
  Dev A: k8s/resources + k8s/logs
  Dev B: llm/openai + llm/anthropic + llm/gemini
  Dev C: agent/safety + agent/tools + agent/history (仅依赖接口)
  Dev D: ui/styles + ui/layout + ui/ringbuffer + ui/messages (无依赖)

Week 2-3:
  Dev A: k8s/events + k8s/describe + 集成测试
  Dev B: llm/prompt + adapter tests
  Dev C: agent/agent (agentic loop)
  Dev D: ui/app + ui/resource

Week 3-4:
  Dev A+B: agent 集成测试 (real providers + cluster)
  Dev C: ui/logviewer + ui/chat
  Dev D: config/wizard + cmd/korthex/main

Week 4-5:
  All: 集成, E2E 测试, polish

Total: 4-5 weeks
```

### 7.5 Critical Path

```
接口冻结 (Day 1)
    │
    ├── config ──→ k8s ──┐
    │                     ├──→ agent ──→ ui/chat ──→ 集成
    ├── config ──→ llm ──┘            ↗
    │                        ui/shell ─
    └── logparse ────────────────────────────────→ 集成
```

**瓶颈**: `agent` 模块，因为它依赖 k8s 和 llm 两者。缓解: 用 mock 先行开发 agent，待 k8s/llm 完成后切换真实实现。

---

## 8. Risk Assessment & Mitigations

### 8.1 LLM Tool-Call 跨 Provider 一致性 (HIGH)

**风险**: 三个 provider 的 tool-calling 行为差异大。Anthropic 返回 `tool_use` block，OpenAI 返回 `function` call，Gemini 返回 `FunctionCall`。边界情况：LLM 返回畸形参数、调用不存在的 tool、无限循环调用。

**缓解**:
- 每个 adapter 的 test suite 使用录制的 HTTP response (golden files)
- 防御性解析：验证 tool name 存在、参数可解析、默认空 map
- maxIterations 防止无限循环
- 未知 tool → 返回 "Unknown tool: X. Available: ..." 让 LLM 自纠

### 8.2 高频日志冲击 TUI (HIGH)

**风险**: 高吞吐服务每秒产生数千行日志。无 backpressure 则 TUI 冻结、OOM、终端无响应。

**缓解**:
- Non-blocking channel send (溢出时 drop + 计数)
- Ring buffer 硬上限 (默认 10000 行)
- Batched flush: 每 50ms 或累积 100 行才重绘
- 状态栏显示 drop 计数: "12 lines dropped (buffer full)"
- 超过 10000 行/分钟时提示缩小范围

### 8.3 Bubble Tea 长时操作并发 (MEDIUM-HIGH)

**风险**: Bubble Tea 的 Elm 架构要求所有状态变更走 Update。Agent loop 和日志流是长时操作，不当并发导致 race condition。

**缓解**:
- 所有长时操作在 `tea.Cmd` 内运行
- 流式操作使用 `p.Send()` 模式: goroutine 读 channel → 逐条 p.Send(msg)
- 永不在 goroutine 内直接修改 Model 字段
- 在 AppModel.Init() 中存储 `*tea.Program` 引用供 streaming goroutine 使用

### 8.4 client-go 依赖膨胀 (MEDIUM)

**风险**: client-go 依赖树巨大，与 LLM SDK 间可能有版本冲突。

**缓解**:
- Pin client-go 到特定 minor version (如 v0.30.x 对应 K8s 1.30)
- 只 import `k8s.io/client-go`, `k8s.io/api`, `k8s.io/apimachinery`（不引 `k8s.io/kubernetes`）
- CI 中 `go build ./...` 尽早发现冲突

### 8.5 System Prompt 跨 Provider 调优 (MEDIUM)

**风险**: 同一 prompt 在 GPT-4o 表现好但在 Claude/Gemini 上差。

**缓解**:
- `agent/prompt.go` 支持 provider-aware 片段 (检查 `provider.ProviderName()`)
- 预设 20 条测试 query 的 benchmark harness
- Anthropic 适合 XML 结构化 prompt, Gemini 需要更显式的 JSON Schema 引用

### 8.6 大集群 Informer 内存 (MEDIUM)

**风险**: 200+ namespace 的集群，全量 informer 消耗大量内存和 API Server watch 连接。

**缓解**:
- LRU 驱逐: 仅保留最近访问的 10 个 namespace 的 informer
- 切换 namespace 后 30 秒 grace period 再停止旧 informer
- >200 ns 集群禁用 "all namespaces" 模式
- 实现在 `k8s/informer.go`
- **详细设计 (状态机 + 伪代码)**: 见 [`internal/k8s/CLAUDE.md`](./internal/k8s/CLAUDE.md) "Informer LRU + Grace Period Design" 章节

### 8.7 Setup Wizard 兼容性 (LOW-MEDIUM)

**风险**: Wizard 需在各种终端环境下工作（无鼠标、小尺寸、无色彩）。

**缓解**:
- 仅使用基础 Bubble Tea 组件 (textinput, list)
- 最小终端 80x24 测试
- 无色彩时 fallback 到 bold/underline
- 可通过预创建 config.yaml + 环境变量完全跳过 Wizard

---

## 9. Phase 2 Architecture Deltas

> Phase 2 完整规格见 [Phase 2 PRD](./docs/superpowers/specs/2026-04-24-phase2-prd-design.md)。本节仅记录对 Phase 1 架构的增量变更。

### 9.1 New Modules

**pkg/redact (Layer 0)** — 正则脱敏引擎。日志发送给 LLM 前自动遮盖敏感信息 (JWT, API Key, Email, IPv4, 信用卡号, Bearer Token)。Engine 构造后不可变，并发安全。

```go
type Engine struct { /* unexported */ }
func NewEngine(cfg RedactionConfig) *Engine
func (e *Engine) Redact(input []byte) (output []byte, stats RedactStats)
```

> 详细规格: [pkg/redact/CLAUDE.md](./pkg/redact/CLAUDE.md) | PRD §5.1

**internal/history (Layer 1)** — 对话历史持久化。使用 modernc.org/sqlite (纯 Go, 无 CGO) 存储会话和消息，FTS5 全文搜索。

```go
type Store interface {
    CreateSession(ctx context.Context, cluster string) (sessionID string, err error)
    EndSession(ctx context.Context, sessionID string, summary string) error
    AddMessage(ctx context.Context, sessionID string, msg Message) error
    Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)
    CleanExpired(ctx context.Context, retentionDays int) error
}
```

> 详细规格: [internal/history/CLAUDE.md](./internal/history/CLAUDE.md) | PRD §3.2

### 9.2 Updated Layer Diagram

```
Layer 4 (Integration):    cmd/korthex + internal/app     ← imports everything
Layer 3 (Presentation):   internal/ui                     ← imports agent, k8s, config, logparse, history
Layer 2 (Intelligence):   internal/agent                  ← imports k8s, llm, logparse, config, history, redact
Layer 1 (Core):           internal/k8s, internal/llm, internal/history  ← imports config only
Layer 0 (Foundation):     internal/config, pkg/logparse, pkg/redact     ← imports nothing internal
```

### 9.3 New Interfaces Summary

**k8s.ResourceAccessor** — Resource Registry 模式（借鉴 k9s DAO Registry），UI/Agent 通过 `AccessorFor()` 查询资源，新增类型无需改动调用方：

```go
type ResourceAccessor interface {
    List(namespace string, filter string) ([]ResourceItem, error)
    GetLabelSelector(namespace, name string) (string, error)
}
func AccessorFor(resourceType string) (ResourceAccessor, bool)
```

> PRD §4.1

### 9.4 New Agent Tools Summary

| Tool | Function | Parameters | Domain |
|------|----------|-----------|--------|
| `kubectl_get_statefulsets` | 列出 StatefulSet | namespace, filter (可选) | Enhanced Browser |
| `kubectl_get_daemonsets` | 列出 DaemonSet | namespace, filter (可选) | Enhanced Browser |
| `kubectl_get_jobs` | 列出 Job | namespace, filter (可选), activeOnly (可选) | Enhanced Browser |
| `kubectl_get_cronjobs` | 列出 CronJob | namespace, filter (可选) | Enhanced Browser |
| `severity_stats` | 统计 severity 分布 | sinceMinutes (可选, int) | Deep Analysis |
| `compare_logs` | 对比两个时间段日志 | namespace, selector, since1, since2, duration | Deep Analysis |
| `get_pod_metrics` | 获取 Pod CPU/Memory | namespace, podName | Deep Analysis |
| `trace_logs` | 跨 namespace trace ID 搜索 | traceId, namespaces (可选), timeRange (可选) | Deep Analysis |
| `bookmark_log_lines` | AI 标记重要日志行 | lineIndices (int 数组) | Data Safety |

> PRD Appendix A

### 9.5 New Dependencies

| Dependency | License | Purpose | Pure Go |
|-----------|---------|---------|---------|
| modernc.org/sqlite | BSD-3-Clause | 对话历史 SQLite 持久化 | Yes (no CGO) |
| charmbracelet/glamour | MIT | AI Chat Markdown 终端渲染 | Yes |

两者均与 Apache-2.0 兼容。需更新 `NOTICE` 文件。

### 9.6 Configuration Schema Additions

三个新配置段，均有合理默认值。Phase 1 config.yaml 升级 Phase 2 无需修改：

```yaml
privacy:
  redaction:
    enabled: true              # 脱敏全局开关
    rules: []                  # 自定义规则 [{name, pattern, replacement}]
    disable_builtin: []        # 禁用的内置规则

history:
  enabled: true                # 对话历史持久化
  retention_days: 30           # 保留天数
  db_path: "~/.korthex/history.db"

analysis:
  trace_id_patterns: []        # 自定义 trace ID 正则
```

> 完整配置见 [configs/default.yaml](./configs/default.yaml) | PRD §9

### 9.7 Updated Module Import Rules

| Module | Phase 1 imports | Phase 2 new imports |
|--------|----------------|---------------------|
| **agent** | config, k8s, llm, logparse | + history, redact |
| **ui** | agent, k8s, config, logparse | + history |
| **app** | all | + history, redact |
| **history** (new) | config | — |
| **redact** (new) | (none) | — |

---

## Appendix: Architecture Decision Records

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| ADR-1 | 模块间通信 | Go interfaces | 可 mock, 并行开发, Phase 2+ 可替换实现 |
| ADR-2 | K8s 类型暴露 | 内部结构体，不暴露 client-go 类型 | 隔离 client-go API 变更 |
| ADR-3 | LLM 抽象 | 统一 Message/ToolCall + per-provider adapter | 三个 SDK API 不兼容，必须统一层 |
| ADR-4 | Agent 事件传递 | channel of AgentEvent + tea.Program.Send() | 符合 Bubble Tea Elm 架构 |
| ADR-5 | 日志 buffer | Mutex-based Ring Buffer | 比 lock-free 简单，单写多读场景足够 |
| ADR-6 | Config 优先级 | 环境变量 > config.yaml > defaults | 标准 twelve-factor, 匹配 SDK 行为 |
| ADR-7 | Wizard 实现 | 独立 Bubble Tea program | 清晰的生命周期: 完成后再启动主 TUI |
| ADR-8 | Agent 错误处理 | 错误作为 tool result 发给 LLM | LLM 可自纠，符合 agentic 模式 |
| ADR-9 | 项目 License | Apache 2.0 | K8s 生态标准, 企业友好, 兼容所有依赖 |
| ADR-10 | API Key 存储 | 环境变量优先, config.yaml fallback | 安全 + 易用平衡 |
| ADR-11 | Ollama 支持 | 延后到 Phase 3/4 | Phase 2 聚焦云端 LLM 分析深度 |
| ADR-12 | 日志可视化 | 延后到 Phase 3 | 用户决策不纳入 Phase 2 |
| ADR-13 | 对话持久化引擎 | modernc.org/sqlite | 纯 Go, 无 CGO, 保持单二进制零依赖 |
| ADR-14 | 脱敏边界 | 仅 LLM 发送前 | Log Viewer 展示原文, 安全边界清晰 |
| ADR-15 | Markdown 渲染库 | charmbracelet/glamour | Bubble Tea 同生态, 风格统一 |
| ADR-16 | 跨 Service 关联 | AI 智能识别 trace ID | 非 sidecar, 非 manual annotation |
| ADR-17 | 书签持久化 | 不持久化 | 与 Ring Buffer 生命周期一致, 简化实现 |
| ADR-18 | Pod Metrics 降级 | 优雅降级 | 无 Metrics Server 时显示 `-`, 不阻塞面板 |
