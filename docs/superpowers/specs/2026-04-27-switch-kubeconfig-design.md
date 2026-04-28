# /switch-kubeconfig — 运行时切换 Kubeconfig 设计文档

> **Date**: 2026-04-27
> **Status**: Draft
> **Scope**: 新增运行时 kubeconfig 文件 + context 切换功能

## 1. 背景与动机

Korthex 当前只在启动时通过 Setup Wizard 或 config.yaml 确定 kubeconfig 路径和 context。用户需要重启应用才能切换集群，这在多集群运维场景下体验很差。

**目标**：支持在 TUI 运行中热切换 kubeconfig 文件和 context，无需重启。

## 2. 需求规格

### 2.1 功能需求

| ID | 需求 | 优先级 |
|----|------|--------|
| F1 | 支持切换到不同的 kubeconfig 文件路径 | P0 |
| F2 | 支持切换同一 kubeconfig 文件内的不同 context | P0 |
| F3 | 通过 Chat 斜杠命令 `/kubeconfig` 触发 | P0 |
| F4 | 通过全局快捷键 `Ctrl+K` 触发 | P0 |
| F5 | 切换后重置资源浏览器和日志，保留 Chat 历史 | P0 |
| F6 | 切换成功后持久化到 config.yaml | P0 |
| F7 | 切换失败时保持旧连接不受影响 | P0 |
| F8 | Overlay 中支持模糊搜索过滤 | P1 |
| F9 | Agent 工具 `switch_kubeconfig` — AI 可主动切换集群 | P0 |

### 2.2 非功能需求

- 切换操作应在 3 秒内完成（正常网络条件下）
- 不影响现有的 AI Agent 执行流程（切换时如果 Agent 正在运行，先取消再切换）
- 遵守 Phase 1-2 只读策略，切换操作本身不会修改集群状态

## 3. 方案设计

### 3.1 总体方案：UI Overlay + K8s 热重连

用户触发切换 → KubeSwitch Overlay 弹出 → 两步选择（kubeconfig 文件 → context） → K8s 客户端热重连 → UI 全状态重置。

### 3.2 架构影响

```
Layer 0 (Foundation): config 新增 DiscoverKubeconfigs() / ParseContexts()
Layer 1 (Core):       k8s 新增 Reconnect() / ContextInfo()
Layer 2 (Intelligence):  agent 新增 switch_kubeconfig 工具
Layer 3 (Presentation): ui 新增 KubeSwitchModel Overlay
Layer 4 (Integration): app 协调切换流程、状态重置
```

不涉及的模块：`pkg/logparse`、`pkg/redact`、`internal/llm`、`internal/history`（history 无代码改动，切换不分割 session——Chat 消息保留，跨集群上下文对后续分析有参考价值）。

### 3.3 需求变更说明

- **Chat 历史保留**：切换后不清空 Chat 消息，用户可以参考之前的分析结果。只重置资源浏览器和日志。
- **Agent 工具**：新增 `switch_kubeconfig` 工具，AI 可以在用户说"切换到 staging 集群"时主动调用。工具返回后触发与手动切换相同的 Overlay + 重连流程。

## 4. 详细设计

### 4.1 Kubeconfig 发现与解析（config 模块）

#### 4.1.1 `DiscoverKubeconfigs() []KubeconfigEntry`

扫描逻辑（按优先级排序）：
1. `$KUBECONFIG` 环境变量中的所有路径（`filepath.SplitList` 冒号分隔）
2. `~/.kube/` 目录下匹配 `config*` 的文件（排除 `.lock` 文件、目录、非普通文件）
3. 当前 korthex config.yaml 中已配置的路径（如果不在上述范围内）
4. 去重（按绝对路径），按路径字母排序

返回类型：

```go
type KubeconfigEntry struct {
    Path       string // 绝对路径
    ContextCount int  // 该文件中的 context 数量
}
```

#### 4.1.2 `ParseContexts(kubeconfigPath string) ([]ContextEntry, error)`

解析指定 kubeconfig 文件的 contexts 列表。

```go
type ContextEntry struct {
    Name    string // context 名称
    Cluster string // 关联的 cluster 名称
    User    string // 关联的 user 名称
    Current bool   // 是否为该文件的 current-context
}
```

使用 `clientcmd.LoadFromFile()` 解析，不创建 REST client，纯文件解析操作。

### 4.2 K8s 热重连（k8s 模块）

#### 4.2.1 Client 接口扩展

```go
type Client interface {
    // ... 现有方法 ...

    // Reconnect 热切换到新的 kubeconfig + context。
    // 先验证新连接可用，再断开旧连接。失败时旧连接保持不变。
    Reconnect(kubeconfig, context string) error

    // ContextInfo 返回当前连接的 kubeconfig 路径和 context 名称。
    ContextInfo() (kubeconfig string, context string)
}
```

#### 4.2.2 Reconnect 实现流程

```
1. 用新参数创建 clientcmd config + rest.Config
2. 创建新 kubernetes.Clientset
3. 验证连接：clientset.Discovery().ServerVersion()
4. 验证通过 →
   a. 调用现有 Disconnect() 清理旧资源（停止所有 informers）
   b. 赋值新 clientset、restConfig、context
   c. 重新初始化 InformerManager、ResourceLister、LogStreamer、EventLister、ResourceDescriber
   d. 存储新 kubeconfig 路径
5. 验证失败 → 返回 error，旧连接完全不受影响
```

#### 4.2.3 k8sClient 结构体变更

新增字段：`kubeconfigPath string`，在 `Connect()` 和 `Reconnect()` 中赋值。

### 4.3 KubeSwitch Overlay（ui 模块）

#### 4.3.1 组件：`KubeSwitchModel`

新文件 `internal/ui/kubeswitch.go`。

**状态机**：

```
stepSelectKubeconfig → stepSelectContext → stepConnecting → (关闭 / stepError)
                ↑              |                                    |
                └──────────────┘ (Esc/Backspace 返回)               |
                ↑                                                   |
                └───────────────────────────────────────────────────┘ (重试)
```

**Step 1 - 选择 Kubeconfig 文件**：
- 列表显示 `DiscoverKubeconfigs()` 结果
- 当前活跃文件标记 `● (current)`
- 每项显示路径 + context 数量，如 `~/.kube/config (3 contexts)`
- `j/k` / `↑/↓` 导航，`/` 进入模糊搜索，`Enter` 选定，`Esc` 关闭
- 如果只有 1 个 kubeconfig 文件，自动跳过进入 Step 2

**Step 2 - 选择 Context**：
- 列表显示 `ParseContexts()` 结果
- 当前活跃 context 标记 `● (current)`
- 每项显示 context 名 + cluster 名，如 `prod-us-east (cluster: eks-prod)`
- `j/k` / `↑/↓` 导航，`/` 模糊搜索，`Enter` 确认切换，`Esc`/`Backspace` 返回 Step 1

**Step 3 - 连接中**：
- 显示 spinner + "Connecting to {context}..."
- 不可交互

**错误状态**：
- 显示错误信息 + 提示 "Press Enter to retry, Esc to cancel"

#### 4.3.2 视觉样式

- 居中 Overlay 窗口，宽度 60%，高度 60%（与 Help Overlay 风格统一）
- 标题栏显示当前步骤：`Switch Kubeconfig [1/2]` / `Select Context [2/2]`
- 使用现有 `styles.go` 中的 theme border 和颜色
- 搜索栏在底部（`/` 触发时显示）

#### 4.3.3 触发方式

**斜杠命令**：`/kubeconfig`（chat.go `slashCommands` 列表新增一项）
- 执行时发送 `showKubeSwitchMsg{}`

**全局快捷键**：`Ctrl+K`（app.go 全局路由新增）
- 在 Agent 运行时不响应（避免意外切换中断分析）
- 在任何面板焦点状态下均可触发

### 4.4 消息类型与状态重置（ui/app 模块）

#### 4.4.1 新增 Message 类型

```go
// showKubeSwitchMsg 触发 KubeSwitch Overlay 显示
type showKubeSwitchMsg struct{}

// kubeSwitchExecuteMsg 用户确认选择后发起切换
type kubeSwitchExecuteMsg struct {
    Kubeconfig string
    Context    string
}

// kubeSwitchCompleteMsg 切换结果
type kubeSwitchCompleteMsg struct {
    ContextName string // 成功时的新 context 名
    Err         error  // 失败时的错误
}
```

#### 4.4.2 App 层协调流程

`AppModel.Update` 处理 `kubeSwitchExecuteMsg` 时：

```
1. 如果 Agent 正在运行 → 先取消 Agent
2. 发起异步 tea.Cmd：
   a. k8sClient.Reconnect(kubeconfig, context)
   b. 成功：更新 config + 持久化 → 返回 kubeSwitchCompleteMsg{ContextName: ctx}
   c. 失败：返回 kubeSwitchCompleteMsg{Err: err}
```

`AppModel.Update` 处理 `kubeSwitchCompleteMsg` 时：

```
成功路径：
  1. kubeSwitch.Close() — 关闭 Overlay
  2. ResourceModel.Reset() — 回到 namespace 层级，触发 loadNamespaces()
  3. LogViewerModel.Reset() — 清空 RingBuffer，停止日志流
  4. ChatModel 保持不变 — 用户可参考之前的分析
  5. StatusBar.SetContext(newContext) — 更新显示
  6. 切换 Layout 到 LayoutFull
  7. agent.SetClusterContext(newContext) — 更新 Agent 集群上下文
  注：不分割 history session。跨集群的对话共享同一 session，
  因为 Chat 历史保留且跨集群上下文对后续分析有参考价值。

失败路径：
  1. kubeSwitch.ShowError(err) — Overlay 显示错误
  2. 其他状态不变
```

#### 4.4.3 面板 Reset 方法

各面板新增 `Reset()` 方法，用于切换后的状态清理：

- `ResourceModel.Reset()`：cursor=0, level=namespace, 清空选择状态, 触发 loadNamespaces()
- `LogViewerModel.Reset()`：清空 RingBuffer, 停止流, 清空搜索/过滤/书签
- `ChatModel`：不重置，保留消息和 query history（切换后 Chat 上下文仍有参考价值）

### 4.5 配置持久化

切换成功后更新 config.yaml：

```go
cfg.Kubernetes.Kubeconfig = newKubeconfigPath
cfg.Kubernetes.DefaultContext = newContext
configManager.Save(cfg)
```

需要在 `App` 中持有 `config.Manager` 引用（当前 App 只持有 `*config.Config`，需要调整为也持有 Manager）。

### 4.6 Agent 工具：list_kubeconfigs + switch_kubeconfig（agent 模块）

新增两个工具：

#### 4.6.1 list_kubeconfigs — 发现可用集群

```go
{
    Name:        "list_kubeconfigs",
    Description: "List all available kubeconfig files under ~/.kube/ and their contexts. Use BEFORE switch_kubeconfig to show the user what clusters are available.",
    Parameters:  []llm.ParameterDef{},
}
```

**工具执行流程**：
1. 调用 `config.DiscoverKubeconfigs(~/.kube/, $KUBECONFIG)` 发现所有 kubeconfig 文件
2. 对每个文件调用 `config.ParseContexts()` 列出 context
3. 标记当前活跃的 context（`*` 前缀）
4. 返回格式化的文件 + context 列表

**System prompt 指引**：
- 用户要求切换集群时，AI **必须先调 `list_kubeconfigs`** 发现可用选项
- 资源查找失败时，AI 应提问是否需要切换集群，并主动调用 `list_kubeconfigs`

#### 4.6.2 switch_kubeconfig — 执行切换

新增工具定义：

```go
{
    Name:        "switch_kubeconfig",
    Description: "Switch to a different Kubernetes cluster by changing kubeconfig file and/or context.",
    Parameters: []llm.ToolParameter{
        {Name: "kubeconfig", Type: "string", Description: "Path to kubeconfig file. Empty = keep current file.", Required: false},
        {Name: "context", Type: "string", Description: "Context name within the kubeconfig file. Required.", Required: true},
    },
}
```

**工具执行流程**：
1. 接收 kubeconfig（可选）和 context 参数
2. 如果 kubeconfig 为空，使用当前 kubeconfig 路径
3. 验证 context 在指定文件中存在（使用 `config.ParseContexts()`）
4. 返回结果字符串（不直接执行切换）
5. 同时通过 `AgentEvent` 发送 `EventKubeSwitch` 事件，携带 kubeconfig + context
6. UI 层接收到事件后触发与手动切换相同的重连流程

**新增 AgentEvent 类型**：
```go
EventKubeSwitch  AgentEventType = "kube_switch"
```

**安全性**：`list_kubeconfigs` 和 `switch_kubeconfig` 均加入 `safetyChecker` 白名单（SafetyAllowed），因为这是客户端配置变更，不修改集群状态。

## 5. 文件变更清单

### 5.1 代码文件

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `internal/config/config.go` | 修改 | 新增 `DiscoverKubeconfigs()`、`ParseContexts()` |
| `internal/config/config_test.go` | 修改 | 新增发现/解析的单元测试 |
| `internal/k8s/types.go` | 修改 | `Client` 接口新增 `Reconnect()`、`ContextInfo()` |
| `internal/k8s/client.go` | 修改 | 实现 `Reconnect()`、`ContextInfo()`，新增 `kubeconfigPath` 字段 |
| `internal/k8s/client_test.go` | 修改 | 新增 Reconnect 单元测试 |
| `internal/k8s/mock.go` | 修改 | Mock 实现新接口方法 |
| `internal/ui/kubeswitch.go` | **新建** | `KubeSwitchModel` Overlay 组件 |
| `internal/ui/kubeswitch_test.go` | **新建** | Overlay 单元测试 |
| `internal/ui/messages.go` | 修改 | 新增 3 个 message 类型 |
| `internal/ui/app.go` | 修改 | `Ctrl+K` 路由、切换消息处理、状态重置 |
| `internal/ui/chat.go` | 修改 | `/kubeconfig` 斜杠命令 |
| `internal/ui/help.go` | 修改 | 帮助文本新增 `Ctrl+K` |
| `internal/ui/resource.go` | 修改 | 新增 `Reset()` 方法 |
| `internal/ui/logviewer.go` | 修改 | 新增 `Reset()` 方法 |
| `internal/agent/tools.go` | 修改 | 新增 `list_kubeconfigs` + `switch_kubeconfig` 工具定义和执行 |
| `internal/agent/agent.go` | 修改 | 新增 `EventKubeSwitch` 事件类型 |
| `internal/agent/safety.go` | 修改 | 白名单新增 `list_kubeconfigs` + `switch_kubeconfig` |
| `internal/agent/prompt.go` | 修改 | System prompt 新增集群切换指引（先 list 再 switch，资源查找失败时建议切换） |
| `internal/app/app.go` | 修改 | 持有 `config.Manager`、处理切换协调 |

### 5.2 文档文件

| 文件 | 说明 |
|------|------|
| `internal/config/CLAUDE.md` | 新增 `DiscoverKubeconfigs` / `ParseContexts` 接口说明 |
| `internal/config/README.md` | 新增「Kubeconfig 发现」章节 |
| `internal/k8s/CLAUDE.md` | 新增 Reconnect 规则：先验新再断旧 |
| `internal/k8s/README.md` | 新增「热重连」章节 |
| `internal/ui/CLAUDE.md` | 新增 KubeSwitch Overlay 规则（步骤切换、Ctrl+K 路由） |
| `internal/ui/README.md` | 新增 KubeSwitch 章节、快捷键表更新 |
| `internal/app/CLAUDE.md` | 新增运行时重配置规则 |
| `internal/app/README.md` | 新增「运行时切换」章节 |
| 根 `CLAUDE.md` | Phase 1 决策表新增 kubeconfig 切换条目 |

## 6. 测试策略

| 层级 | 测试内容 | 方法 |
|------|---------|------|
| config | `DiscoverKubeconfigs` 扫描逻辑 | 临时目录 + 假 kubeconfig 文件 |
| config | `ParseContexts` 解析 | 预制 kubeconfig YAML fixtures |
| k8s | `Reconnect` 成功/失败路径 | Mock clientset + fake discovery |
| k8s | `ContextInfo` 返回值 | Connect 后验证 |
| ui | KubeSwitchModel 步骤流转 | tea.Msg 序列驱动 |
| ui | 搜索过滤 | 输入字符验证过滤结果 |
| ui | Esc/Backspace 导航 | 状态断言 |
| app | 端到端切换流程 | Mock k8s + 验证 Reset 调用 |

## 7. 风险与缓解

| 风险 | 缓解措施 |
|------|---------|
| 切换期间网络超时导致长时间无响应 | `Reconnect` 使用 5 秒超时的 context |
| 新集群 RBAC 权限不足导致部分功能不可用 | `Reconnect` 只验证 `ServerVersion()`，权限问题在后续操作中自然暴露并显示错误 |
| 切换时 Agent 正在执行 | App 层先取消 Agent 再执行切换 |
| kubeconfig 文件格式异常 | `ParseContexts` 返回错误，Overlay 显示提示 |
