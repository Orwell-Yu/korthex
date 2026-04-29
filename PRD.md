# Korthex - Product Requirements Document

> **Korthex** = K8s + Cortex — The Intelligent Brain of Your Kubernetes Cluster
>
> *"Speak to your cluster, see everything."*

**Version:** v0.1 (Phase 1 - Log Intelligence)
**Date:** 2026-04-22
**Author:** Mio

---

## 1. Product Vision

Korthex 是一个 **AI 原生的 Kubernetes 终端工具**，融合了三种能力：

1. **自然语言交互** — 像和 Claude Code 对话一样，用自然语言管理 K8s 集群
2. **K9s 级 TUI 界面** — 可视化浏览 Namespace / Deployment / Pod，实时查看日志
3. **智能日志检索** — LLM 理解你的意图，自动构建 kubectl 命令捞取、分析日志

**一句话定位：** 把 "Claude Code for Kubernetes" 和 "k9s" 合二为一，第一期聚焦日志查询和捞取。

---

## 2. Competitive Landscape (竞品分析)

### 2.1 LLM-Powered K8s Tools

| Tool | Stars | Language | LLM Support | Core Feature | Limitation |
|------|-------|----------|-------------|--------------|------------|
| **kubectl-ai** (Google) | ~7.2k | Go | Gemini/OpenAI/Ollama | NL → kubectl 命令，交互式 shell | **无 TUI**，纯命令行；不专注日志分析 |
| **k8sgpt** (CNCF) | ~7.5k | Go | OpenAI/Bedrock/Gemini | 集群问题诊断，分析器模式 | **无 TUI**，无对话模式；诊断导向，非日志导向 |
| **kube-copilot** (feiskyer) | ~157 | Go/Python | OpenAI/Claude/Gemini | 诊断/审计/执行/生成 | **无 TUI**，星数低，社区小 |
| **kopilot** (knight42) | - | Go | ChatGPT only | 工作负载诊断 | 仅支持 OpenAI，功能单一 |
| **KoPylot** | - | Python | Multi | 聊天界面 + 监控 | 不够成熟，无可滚动的大屏日志 |
| **HolmesGPT** (CNCF) | ~1.8k | Python | OpenAI/Claude/Gemini | 事件调查，关联 Prometheus/Loki/Tempo，有 k9s 插件 | 非 TUI，CLI agent 模式；调查导向非管理导向 |
| **Botkube** | ~2.3k | Go | Multi | Slack/Teams 集成，NL 集群控制 | 非终端工具，依赖外部平台 |

### 2.2 K8s TUI Tools

| Tool | Stars | Language | TUI Framework | Core Feature | Limitation |
|------|-------|----------|---------------|--------------|------------|
| **k9s** | ~33.4k | Go | 自研 tcell fork | 完整 K8s 资源浏览/管理，日志查看 | **无 AI 能力**，无自然语言交互 |
| **KDash** | ~2.5k | Rust | tui-rs | 只读 K8s 面板，轻量快速 | 只读，无管理能力，无 AI |
| **kubetui** | ~257 | Rust | ratatui | 实时监控，日志过滤(正则) | 无 AI，社区小 |

### 2.3 AI Log Analysis Tools

| Tool | Stars | Language | AI Support | Core Feature | Limitation |
|------|-------|----------|------------|--------------|------------|
| **Gonzo** | ~2.5k | Go | Anthropic/OpenAI/Ollama | TUI 日志分析，热力图，AI 洞察 | **仅日志查看器**，不是 K8s 管理工具；AI 仅能分析选中的单条日志 |

### 2.4 Market Gap (市场空白)

```
                        Has TUI
                          │
              k9s --------┤---------- Korthex ★
              KDash       │           (our target)
              kubetui     │
                          │
    No AI ────────────────┼──────────────── Has AI
                          │
              kubectl     │           kubectl-ai
              stern       │           k8sgpt
              kubetail    │           kube-copilot
                          │
                      No TUI
```

**核心发现：目前没有任何一个工具同时具备 "AI 自然语言交互 + 完整 TUI 界面 + 聚焦日志分析" 三个能力。**

可借鉴的设计：
- **kubectl-ai** 的交互式 shell 模式和多 LLM provider 支持
- **k9s** 的资源层级浏览 (Namespace → Deployment → Pod → Container → Logs)
- **Gonzo** 的日志可视化 (热力图、severity 分布)，以及作为 k9s plugin 的集成方式
- **kubetui** 的正则日志查询跨多 Pod 能力

---

## 3. Target Users (目标用户)

### Primary
- **SRE / DevOps 工程师** — 日常需要频繁查日志、排查问题
- **后端开发者** — 需要查看自己服务的日志但不精通 kubectl 复杂命令

### Secondary
- **平台工程师** — 构建内部工具链，需要可扩展的 K8s 交互工具
- **新手运维** — 不熟悉 kubectl 语法，需要 AI 辅助

### User Persona (用户画像)

> **小明**，后端开发 3 年经验。线上有个 bug，PM 说 "帮我看下 production 环境 order-service 最近 1 小时有没有报 NullPointerException"。
>
> 以前：`kubectl get pods -n production | grep order` → 找到 pod 名 → `kubectl logs <pod-name> -n production --since=1h | grep NullPointerException` → 发现有 3 个 replica，要查 3 次 → 复制出来发给 PM
>
> 现在：打开 Korthex，输入 "帮我查 production 的 order-service 最近一小时的 NullPointerException 日志"，LLM 自动查询所有 replica，结果在大屏展示，一键复制。

---

## 4. Product Architecture (产品架构)

```
┌─────────────────────────────────────────────────────────┐
│                    Korthex TUI                          │
│  ┌───────────────────────────────────────────────────┐  │
│  │              Resource Browser Panel               │  │
│  │  [Namespace] → [Deployment] → [Pod] → [Container]│  │
│  ├───────────────────────────────────────────────────┤  │
│  │              Log Viewer Panel (scrollable)        │  │
│  │  - Manual browse logs                             │  │
│  │  - AI-fetched logs display here too               │  │
│  │  - Syntax highlighting, severity coloring         │  │
│  ├───────────────────────────────────────────────────┤  │
│  │              AI Chat Panel                        │  │
│  │  > User: 查一下 prod 的 order-service 报错日志    │  │
│  │  < AI: 正在查询 3 个 Pod 的日志...                │  │
│  │  < AI: 找到 12 条 ERROR 日志，已在上方展示        │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
         │                │                │
         ▼                ▼                ▼
   ┌──────────┐    ┌──────────┐    ┌──────────┐
   │ K8s API  │    │ LLM API  │    │  Config  │
   │(client-  │    │(OpenAI/  │    │  Store   │
   │ go)      │    │ Claude/  │    │          │
   │          │    │ Gemini)  │    │          │
   └──────────┘    └──────────┘    └──────────┘
```

---

## 5. Feature Specification (功能规格)

### 5.0a Startup Logo (启动画面) — Phase 1

启动时在终端显示 ASCII Art Logo，类似 k9s 的启动体验。用户在初始化期间（K8s 连接、LLM 验证）看到品牌标识和版本信息，而非空白等待。

```
 ██╗  ██╗ ██████╗ ██████╗ ████████╗██╗  ██╗███████╗██╗  ██╗
 ██║ ██╔╝██╔═══██╗██╔══██╗╚══██╔══╝██║  ██║██╔════╝╚██╗██╔╝
 █████╔╝ ██║   ██║██████╔╝   ██║   ███████║█████╗   ╚███╔╝
 ██╔═██╗ ██║   ██║██╔══██╗   ██║   ██╔══██║██╔══╝   ██╔██╗
 ██║  ██╗╚██████╔╝██║  ██║   ██║   ██║  ██║███████╗██╔╝ ██╗
 ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝

  Korthex v0.1.0
  Speak to your cluster, see everything.

  Initializing...
```

Logo 在普通终端（非 alt-screen）中显示，`app.New()` 初始化完成后切换到 Bubble Tea 的 alt-screen TUI。

### 5.0b Setup Wizard (首次引导)

安装后首次运行，引导用户完成配置：

```
$ korthex

  🔧 Welcome to Korthex! Let's get you set up.

  Step 1/2: Kubernetes Configuration
  ─────────────────────────────────
  Detected kubeconfig at: ~/.kube/config
  Available contexts:
    1. production-cluster
    2. staging-cluster
    3. dev-cluster
  Select default context [1]: _

  Step 2/2: LLM Configuration
  ────────────────────────────
  Select LLM provider:
    1. OpenAI (API Key)
    2. Anthropic Claude (API Key)
    3. Google Gemini (API Key)
    4. Custom OpenAI-compatible endpoint (API Key + Base URL)
  Select [1]: _

  API Key: sk-****
  Model [gpt-4o]: _

  ✓ Configuration saved to ~/.config/korthex/config.yaml
  ✓ Connected to production-cluster (48 namespaces, 312 pods)
  ✓ LLM ready (OpenAI gpt-4o)

  Press Enter to start Korthex...
```

配置文件 `~/.config/korthex/config.yaml`:
```yaml
kubernetes:
  kubeconfig: ~/.kube/config
  default_context: production-cluster

llm:
  provider: openai           # openai | anthropic | gemini | custom
  api_key: sk-***            # encrypted storage
  model: gpt-4o
  base_url: ""               # for custom endpoint
  temperature: 0.1           # low temperature for precise kubectl generation
  max_tokens: 4096
  send_logs: true            # false = AI 仅生成命令，不接收日志内容分析

agent:
  max_iterations: -1         # Agentic Loop 最大迭代次数 (-1 = 无限)
  max_history_turns: 20      # AI 对话上下文保留轮数

ui:
  theme: dark                # dark | light | dracula | nord
  log_lines_limit: 10000    # Ring Buffer 最大日志行数
  log_page_size: 1000       # 日志分页每页行数
  default_log_since: 1h     # 默认日志时间范围
```

### 5.1 TUI Resource Browser (资源浏览器)

类似 k9s 的资源浏览面板：

**层级导航 (Phase 1)：**
```
Cluster
  └── Namespaces (列表, 可搜索)
       └── Deployments (列表, 显示 replica 状态)
            └── Pods (列表, 显示 STATUS/RESTARTS/AGE)
                 └── Containers
                      └── Logs
```

> **Phase 2 扩展：** StatefulSets、DaemonSets、Jobs/CronJobs 在 Phase 2 加入 Resource Browser。

**交互方式：**
- `↑↓` 或 `j/k` 上下选择
- `Enter` 进入下一层
- `Esc` / `Backspace` 返回上一层
- `/` 搜索过滤
- `l` 直接查看选中 Pod 的日志
- `d` 查看 describe 信息
- `y` 复制 YAML
- `Tab` 切换面板焦点 (Resource Browser / Log Viewer / AI Chat)

### 5.2 Log Viewer (日志查看器) — Phase 1 Core

可滚动的大屏日志展示面板：

**功能列表：**

| Feature | Description | Priority | Phase |
|---------|-------------|----------|-------|
| 实时日志流 | 类似 `kubectl logs -f`，实时 tail 日志 | P0 | **Phase 1** |
| 多 Pod 聚合 | 同一 Deployment 的所有 Pod 日志聚合展示，带 Pod 名前缀 | P0 | **Phase 1** |
| Severity 着色 | ERROR=红, WARN=黄, INFO=蓝, DEBUG=灰 | P0 | **Phase 1** |
| 关键词搜索 | 在日志中搜索高亮 (类似 less 的 `/pattern`) | P0 | **Phase 1** |
| 时间范围过滤 | `--since=1h` / `--since-time=2024-01-01T00:00:00Z` | P0 | **Phase 1** |
| 日志分页 | 日志超过 1000 行时分页展示，`Ctrl+D`/`Ctrl+U`/`PgDn`/`PgUp` 翻半页 | P0 | **Phase 1** |
| 日志导出 | 将当前查看的日志导出到文件 | P0 | **Phase 1** |
| 正则过滤 | 用正则表达式过滤日志行 | P1 | **Phase 1** |
| JSON 日志格式化 | 自动检测 JSON 日志并格式化展示 | P1 | **Phase 1** |
| Previous 日志 | 查看已重启容器的上一次日志 (`--previous`) | P1 | **Phase 1** |
| 日志书签 | 标记重要日志行，方便快速跳转 | P2 | Phase 2 |
| 日志热力图 | Severity 分布的时间线热力图可视化 | P2 | Phase 2 |

**日志展示格式：**
```
┌─ Logs: order-service (production) ─ 3 pods ─ since 1h ──────┐
│                                                               │
│ [14:23:01] pod/order-svc-7d4f8-abc12  INFO  Order #1234 ...  │
│ [14:23:02] pod/order-svc-7d4f8-def34  WARN  Slow query ...   │
│ [14:23:05] pod/order-svc-7d4f8-abc12  ERROR NullPointer...   │
│ [14:23:05] pod/order-svc-7d4f8-ghi56  ERROR NullPointer...   │
│ [14:23:08] pod/order-svc-7d4f8-def34  INFO  Retry success    │
│ ...                                                           │
│                                                               │
│ ──────── [/NullPointer] 2 matches ── [Ctrl+N] next ──────── │
└───────────────────────────────────────────────────────────────┘
```

### 5.3 AI Chat Interface (AI 对话界面) — Phase 1 Core

类似 Claude Code 的对话式交互，集成在 TUI 底部：

**核心工作流：**

```
User Input (自然语言)
      │
      ▼
LLM 理解意图
      │
      ├── 构建 kubectl 命令
      │       │
      │       ▼
      │   执行 kubectl (显示正在执行的命令)
      │       │
      │       ▼
      │   结果返回给 LLM
      │       │
      │       ▼
      │   LLM 总结/分析结果
      │       │
      │       ▼
      │   在 Log Viewer 展示原始日志
      │   在 AI Chat 展示分析摘要
      │   在 Resource Browser 自动导航到对应资源层级
      │
      └── 直接回答 K8s 知识问题
```

**Phase 1 支持的对话场景 (日志相关)：**

| 场景 | 用户输入示例 | AI 行为 |
|------|-------------|---------|
| 查特定服务日志 | "查一下 production 的 order-service 日志" | `kubectl logs -l app=order-service -n production --all-containers` |
| 按时间范围查 | "最近 30 分钟 payment-service 的日志" | `kubectl logs ... --since=30m` |
| 按关键词过滤 | "order-service 有没有报 timeout 错误" | `kubectl logs ... \| grep -i timeout` |
| 多 Pod 日志 | "把 order-service 所有副本的 ERROR 日志都捞出来" | 遍历所有 Pod 执行 `kubectl logs`，过滤 ERROR |
| 日志分析 | "帮我分析一下这些错误日志是什么原因" | 将日志内容发给 LLM 分析 |
| 日志统计 | "统计一下过去 1 小时各 service 的 ERROR 数量" | 执行多次 kubectl logs，统计汇总 |
| 日志导出 | "把这些日志保存到文件" | 将当前 Log Viewer 内容导出 |
| 上下文追踪 | "帮我找一下这个 request-id 相关的所有日志" | 跨 Pod/Service grep request-id |

**Phase 1 额外支持的场景 (已确认)：**

| 场景 | 用户输入示例 | AI 行为 |
|------|-------------|---------|
| K8s 知识问答 | "CrashLoopBackOff 是什么意思" | LLM 直接回答，**不执行任何命令** |
| 概念解释 | "kubectl logs 的 --since 和 --since-time 有什么区别" | LLM 直接回答 |
| 排查建议 | "OOMKilled 一般怎么排查" | LLM 直接回答，可建议用户执行的查询 |
| 超范围请求 | "帮我 scale 这个 deployment 到 5 个副本" | 回复：「集群管理功能将在 Phase 3 支持，当前仅支持只读查询。」 |

**AI 安全机制：**
- Phase 1 仅允许只读命令，**日志查询类命令直接执行，无需确认**
- 命令执行时在 Chat 面板展示正在执行的 kubectl 命令（透明可见）
- 禁止执行 `kubectl delete`, `kubectl apply`, `kubectl edit` 等写操作
- Phase 3 的集群管理命令需经过 **三次确认** 才能执行
- 可配置的命令白名单

**错误处理与边界场景：**

| 场景 | 用户感知 | 处理方式 |
|------|---------|---------|
| K8s 集群连接失败 | TUI 启动时显示连接错误，提供重试选项 | 显示具体错误信息（证书过期/网络不通/context 不存在），引导用户检查 kubeconfig |
| LLM API 超时/限流 | AI Chat 显示「AI 服务暂时不可用，您可以手动浏览日志」 | 超时阈值 30 秒，自动重试 1 次；限流时显示等待时间。TUI 手动浏览功能不受影响 |
| RBAC 权限不足 | AI Chat 显示「无权限访问 namespace X 的日志」 | 展示具体缺失的权限（如 `pods/log` verb `get`），建议用户联系管理员 |
| kubectl logs 返回空结果 | AI 回复「未找到匹配的日志，可能原因：...」 | Agentic Loop 尝试修正（检查 namespace/label 是否正确），给出缩小排查范围的建议 |
| Pod 处于 CrashLoopBackOff | 自动尝试 `--previous` 拉取上一次运行的日志 | AI 检测到 Pod 非 Running 状态时，自动附加 `--previous` 参数 |
| 日志无时间戳 | Log Viewer 按接收顺序展示，不做时间排序 | 在 Pod 名前缀后标注「(无时间戳)」，多 Pod 聚合时按 Pod 分组而非按时间混排 |
| 单行日志超长 (>10KB) | 自动截断显示，末尾标注 `[truncated]` | 完整内容可通过导出功能获取 |

**AI Chat 面板 UI：**

用户按 Enter 提交查询后，Chat 面板使用 `bubbles/spinner` MiniDot (⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏) 动画显示 Thinking 状态，
防止用户以为程序卡住。动画在首个 LLM 流式 delta 到达后消失，结果开始渐进渲染。

```
┌─ AI Assistant ⠹ running ──────────────────────────────────┐
│                                                             │
│  You: 帮我查一下 prod 的 order-service 最近 1 小时的报错    │
│                                                             │
│  ⠹ Thinking...                                              │
│                                                             │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

查询完成后：
```
┌─ AI Assistant ─────────────────────────────────────────────┐
│                                                             │
│  You: 帮我查一下 prod 的 order-service 最近 1 小时的报错    │
│                                                             │
│  AI: 正在查询... 执行命令:                                   │
│  $ kubectl logs -l app=order-service -n production          │
│    --since=1h --all-containers | grep -iE "error|exception" │
│                                                             │
│  找到 3 个 Pod, 共 47 条错误日志:                            │
│  - NullPointerException: 23 条 (集中在 14:20-14:30)         │
│  - ConnectionTimeout: 18 条 (数据库连接超时)                 │
│  - OutOfMemoryError: 6 条                                   │
│                                                             │
│  完整日志已在上方 Log Viewer 中展示。                         │
│  主要问题: 14:20 左右数据库连接池耗尽，导致大量请求超时。      │
│                                                             │
│  > _                                                        │
└─────────────────────────────────────────────────────────────┘
```

**AI Chat 对话管理 (已实现)：**
- **FIFO 轮次淘汰**: 当对话轮数超过 `max_history_turns`（默认 20 轮）时，最旧的完整轮次（user → assistant 全流程）被移除
- **折叠指示器**: 被淘汰的轮次在 Chat 面板顶部显示 `(N earlier turns collapsed)` 提示
- **上下箭头历史回溯**: 在输入框中按 `↑`/`↓` 可浏览并重新提交之前的查询（类似 shell 历史）
- **StatusBar 消息通道**: 日志导出等操作的结果通过 StatusBar 的 MSG 段落显示瞬态通知

**日志 Grep 过滤 (已实现)：**
- Agent 工具 `kubectl_logs` 和 `kubectl_logs_selector` 支持 `grepPattern` 参数
- AI 在用户提及关键词过滤时，自动在 tool call 中添加 `grepPattern`（Go 端正则匹配，非 server-side）
- Event 工具 `kubectl_get_events` 支持 `fieldSelector` 参数，按 name/kind/type/reason 过滤

**Describe 覆盖层 (已实现)：**
- 在 Resource Browser 中按 `d` 查看资源的 describe 输出
- 以覆盖层形式展示在 Resource 面板中，Esc 返回列表

### 5.4 Layout Modes (布局模式)

支持多种界面布局，用户可按需切换：

**Mode 1: Full TUI (默认)**
```
┌──────────────────┬─────────────────────────────────────┐
│ Resource Browser │          Log Viewer                  │
│                  │                                      │
│ Namespaces:      │  [14:23:01] INFO  Order created     │
│ > production     │  [14:23:02] WARN  Slow query        │
│   staging        │  [14:23:05] ERROR NullPointer...    │
│   dev            │                                      │
│                  │                                      │
├──────────────────┴─────────────────────────────────────┤
│ AI Chat                                                 │
│ > _                                                     │
└─────────────────────────────────────────────────────────┘
```

**Mode 2: Chat Focus (聊天主导)**
```
┌─────────────────────────────────────────────────────────┐
│ AI Chat (full width)                                     │
│                                                          │
│  You: 查一下 prod 的 order-service 报错日志               │
│  AI: [executing...] Found 47 errors in 3 pods            │
│  AI: 主要问题是数据库连接超时...                           │
│                                                          │
│  > _                                                     │
├─────────────────────────────────────────────────────────┤
│ Log Viewer (collapsed, expandable)                       │
│ ▶ 47 lines - click to expand                            │
└─────────────────────────────────────────────────────────┘
```

**Mode 3: Log Focus (日志主导)**
```
┌─────────────────────────────────────────────────────────┐
│ Log Viewer (full screen, scrollable)                     │
│                                                          │
│ [14:23:01] pod/order-svc-abc12  INFO   Order created    │
│ [14:23:02] pod/order-svc-def34  WARN   Slow query       │
│ [14:23:05] pod/order-svc-abc12  ERROR  NullPointer...   │
│ [14:23:05] pod/order-svc-ghi56  ERROR  NullPointer...   │
│ ...                                                      │
│                                                          │
│ [/ search] [f filter] [: AI command]                     │
└─────────────────────────────────────────────────────────┘
```

切换方式：`F1` / `F2` / `F3` 或 `Ctrl+1` / `Ctrl+2` / `Ctrl+3`

### 5.5 Keyboard Shortcuts (快捷键)

| Key | Action | Context |
|-----|--------|---------|
| `Tab` | 切换面板焦点 | Global |
| `F1`/`F2`/`F3` | 切换布局模式 | Global |
| `:` | 进入 AI Chat 输入 | Global (vim-style) |
| `/` | 搜索/过滤 | Resource Browser / Log Viewer |
| `Esc` | 退出当前模式/返回上级 | Global |
| `q` | 退出 Korthex | Global |
| `j/k` | 上下移动 | Resource Browser / Log Viewer |
| `h/l` | 左右水平滚动 | Log Viewer |
| `0` | 重置水平滚动到行首 | Log Viewer |
| `g/G` | 跳到顶部/底部 | Log Viewer |
| `Ctrl+D/Ctrl+U` / `PgDn/PgUp` | 向下/向上翻半页 | Log Viewer / AI Chat |
| `f` | 进入日志过滤模式 | Log Viewer |
| `F` | Follow 模式 (实时 tail) | Log Viewer |
| `s` | 保存日志到文件 (`~/.korthex/logs/`) | Log Viewer |
| `y` | 复制选中内容 | Log Viewer / Resource |
| `Ctrl+C` | 中断当前 AI 操作 | AI Chat |
| `?` | 显示帮助 | Global |

---

## 6. Technical Architecture (技术架构)

### 6.1 Tech Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| **Language** | **Go** | K8s 生态主力语言，client-go 原生支持，单二进制分发 |
| **TUI Framework** | **Bubble Tea** (charmbracelet/bubbletea) | 现代 Go TUI 框架，Elm 架构，组件化，社区活跃(25k+ stars) |
| **TUI Components** | **Lip Gloss** + **Bubbles** | 样式库 + 预制组件 (table, viewport, textinput) |
| **K8s Client** | **client-go** | 官方 K8s Go client |
| **LLM Integration** | 自研 adapter 层 | Phase 1: OpenAI / Anthropic / Gemini / Custom OpenAI-compatible；Phase 2: + Ollama |
| **Config** | **Viper** | Go 标准配置库 |
| **Log Parsing** | 自研 | JSON/plain text 日志解析，severity 检测 |
| **Retry** | **cenkalti/backoff/v4** | 指数退避重试（k9s 同款） |
| **Fuzzy Search** | **sahilm/fuzzy** | 资源列表模糊搜索（k9s 同款） |

### 6.2 Module Design

```
korthex/
├── cmd/                    # CLI entrypoint
│   └── korthex/
│       └── main.go
├── internal/
│   ├── app/                # Application orchestration
│   │   └── app.go
│   ├── config/             # Configuration management
│   │   ├── config.go
│   │   └── wizard.go       # Setup wizard
│   ├── k8s/                # Kubernetes client
│   │   ├── client.go       # K8s API wrapper
│   │   ├── resources.go    # Resource listing
│   │   └── logs.go         # Log streaming
│   ├── llm/                # LLM integration
│   │   ├── provider.go     # Provider interface
│   │   ├── openai.go       # OpenAI + Custom OpenAI-compatible
│   │   ├── anthropic.go
│   │   ├── gemini.go
│   │   └── prompt.go       # Prompt templates
│   ├── agent/              # AI agent logic
│   │   ├── agent.go        # Core agent loop
│   │   ├── tools.go        # Tool definitions (kubectl commands)
│   │   ├── safety.go       # Command whitelist & safety checks
│   │   └── parser.go       # Parse LLM output → actions
│   └── ui/                 # TUI components
│       ├── app.go          # Main TUI model
│       ├── resource.go     # Resource browser panel
│       ├── logviewer.go    # Log viewer panel
│       ├── chat.go         # AI chat panel
│       ├── statusbar.go    # Status bar
│       ├── styles.go       # Theme & styles
│       └── layout.go       # Layout manager
├── pkg/
│   └── logparse/           # Log parsing utilities
│       ├── parser.go
│       ├── json.go
│       └── severity.go
├── configs/
│   └── default.yaml        # Default config template
├── NOTICE                  # Third-party license attributions
├── LICENSE                 # Apache 2.0
├── go.mod
├── go.sum
└── README.md
```

### 6.3 Performance Architecture (借鉴 k9s)

大集群 + 海量日志下绝对不能卡。以下是从 k9s 源码提炼出的、必须在 Korthex 中采用的性能模式：

#### 6.3.1 Informer + Cache Lister (永远不直接轮询 API Server)

```
┌─────────┐   Watch Stream    ┌──────────────┐   List from cache   ┌─────┐
│ K8s API │ ───────────────→  │ Informer     │ ────────────────→   │ TUI │
│ Server  │                   │ (per-ns)     │                     │     │
│         │ ← Initial List    │ In-memory    │                     │     │
└─────────┘                   │ cache + index│                     └─────┘
                              └──────────────┘
```

- 使用 `DynamicSharedInformerFactory`，**按 namespace 隔离**
- 资源列表从 Informer 的 Lister（内存缓存）读取，**零 API 调用**
- Resync 间隔 10 分钟，不频繁重建缓存
- 切换 namespace 时，按需启停对应的 Informer Factory
- All-namespaces 模式使用单个全局 factory

#### 6.3.2 资源列表刷新 — Atomic Dedup + Delta Diff

```go
// k9s 模式：原子去重，防止并发刷新堆积
if !atomic.CompareAndSwapInt32(&inUpdate, 0, 1) {
    return // 上一次刷新还没结束，跳过本次
}
defer atomic.StoreInt32(&inUpdate, 0)
```

- 默认 2 秒刷新间隔，首次 300ms 快速渲染
- **Delta 追踪**：只对比变化的字段，跳过时间列，只重绘变化的 cell
- **Clone on Read**：UI 读取数据时拿到 Clone 副本，不锁主数据
- 刷新失败时 **指数退避重试**（cenkalti/backoff/v4）

#### 6.3.3 日志流 — 管道架构 (核心性能关键)

```
K8s API Stream
      │
      ▼  (per-container goroutine)
  readLogs()
      │
      ▼  (buffered channel, cap=50)
  LogChan ──→ overflow? → DROP + warn (非阻塞 select/default)
      │
      ▼
  updateLogs()
      │
      ▼  (batched flush, 不是每行都刷)
  Append to Ring Buffer (bounded capacity)
      │
      ▼  (flush timeout 或 buffer 溢出时才 Notify)
  UI Redraw
```

**关键实现细节：**

| 模式 | 说明 | 为什么重要 |
|------|------|-----------|
| **Buffered Channel + Non-blocking Send** | `select { case ch <- line: default: }` | 日志生产者永远不阻塞，UI 慢了就丢日志而非卡死 |
| **Per-container Goroutine** | 每个容器独立 goroutine tail | 一个慢容器不影响其他容器 |
| **Ring Buffer (有界)** | 超过容量时 `Shift()` 淘汰最老的行 | 内存永远可控，不会 OOM |
| **Batched Flush** | 积累到阈值或 flush timeout 才通知 UI | 避免每行日志都触发 UI 重绘 |
| **指数退避重试** | 初始 500ms，最大 15s，最多 20 次 | 网络抖动时不暴打 API Server |
| **Pod 状态感知** | Succeeded/Failed/Deleted 时停止重试 | 不在死 Pod 上浪费资源 |

#### 6.3.4 内存管理

- **Ring Buffer 容量上限** — 每个日志流最多保留 `log_lines_limit` 行（默认 10000）
- **Ring Buffer 与分页的关系** — Ring Buffer 是内存中的滑动窗口，保存最近 N 行。Log Viewer 的"分页"是 UI 层概念（每屏展示 `log_page_size` 行，可在 buffer 内自由滚动）。当 buffer 满时最老的行被淘汰，**不支持向上加载 buffer 之外的历史日志**（kubelet 可能已轮转）。如需保留历史日志，用户应在日志还在 buffer 内时按 `s` 导出。
- **LogItem.Size()** — 每条日志显式计算内存占用（100 + byte lengths），用于容量追踪
- **预分配 slice** — `make([]T, 0, expectedLen)` 避免频繁扩容
- **Worker Pool + Semaphore** — 并发操作受信号量限制，防止 goroutine 爆炸
- **Metrics 按需加载** — Pod metrics 仅在用户主动请求时才拉取

#### 6.3.5 大集群优化策略

| 集群规模 | 策略 |
|---------|------|
| < 50 namespaces | 正常模式，切换 ns 时启停 informer |
| 50-200 namespaces | 仅缓存最近访问的 10 个 ns 的 informer |
| > 200 namespaces | 强制要求用户选择 ns，禁用 all-namespaces 的资源列表 |
| 单 ns > 500 pods | 默认开启 label selector 过滤，提示用户按 deployment 筛选 |
| 日志 > 10000 行/分钟 | 自动降采样 + 提示用户缩小范围或添加过滤条件 |

### 6.4 AI Agent Design

Agent 采用 **Tool-Use 模式** (类似 Claude Code 的 Function Calling)：

**底层执行方式 (已确认)：全阶段使用 client-go API，不依赖 kubectl 二进制**

AI Agent 的所有 tools 底层均通过 **client-go API** 执行，不依赖 kubectl 二进制：

- **资源查询类** (`kubectl_get_pods`、`kubectl_get_deployments` 等) → 直接从 Informer Cache 读取，零 API 调用
- **日志查询类** (`kubectl_logs`、`kubectl_logs_selector`) → 调用 client-go 的 `CoreV1().Pods().GetLogs()` 流式读取
- **Describe 类** (`kubectl_describe`) → 调用 client-go 对应资源的 Get API + Event 列表聚合
- AI Chat 面板展示的 `$ kubectl logs ...` 命令是**给用户看的可读表示**，方便用户理解 AI 做了什么、以及在终端中复现

**Phase 3 集群管理操作同样使用 client-go（已确认）：**

`kubectl` 自身就是基于 client-go 构建的，所有 K8s 操作均有等价 API：

| Phase 3 操作 | client-go 等价调用 |
|---|---|
| `kubectl scale` | `AppsV1().Deployments().UpdateScale()` |
| `kubectl rollout restart` | `AppsV1().Deployments().Patch()` (添加 restart annotation) |
| `kubectl delete` | 对应资源的 `.Delete()` 方法 |
| `kubectl drain` | Cordon (Update Node) + Eviction API |
| `kubectl apply` | Server-Side Apply (`Patch` with `fieldManager`, K8s 1.22+) |

**设计原则：Korthex 全生命周期保持单二进制、零外部依赖。不在任何阶段引入 kubectl 二进制依赖。**

**AI 查询 → Log Viewer 数据流：**

```
用户输入 "查 prod 的 order-service 报错日志"
      │
      ▼
AI Agent 解析意图，调用 tool
      │
      ├─→ kubectl_get_deployments (从 Informer Cache 读)
      │         → 找到 order-service, 提取 label selector
      │
      ├─→ kubectl_logs_selector (调 client-go GetLogs API)
      │         → 返回 io.ReadCloser 日志流
      │
      ▼
日志流写入 Log Viewer 的 Ring Buffer
      │
      ├─→ Log Viewer 面板实时渲染日志（severity 着色、Pod 前缀）
      │
      ├─→ 日志文本同时发给 LLM（如 send_logs=true）
      │         → LLM 返回分析摘要
      │
      ▼
AI Chat 面板展示：执行的"命令" + 分析摘要
Log Viewer 面板展示：完整日志原文（可滚动、可搜索、可导出）
```

> 核心设计：AI 查询和手动浏览共享同一个 Log Viewer + Ring Buffer。AI 不是在 Chat 面板中"打印"日志，而是把日志灌入 Log Viewer 的 buffer，用户在 Log Viewer 中操作日志（搜索、过滤、导出）的体验完全一致。

```go
// Tool definitions for the AI agent
type Tool struct {
    Name        string
    Description string
    Parameters  []Parameter
    Execute     func(params map[string]string) (string, error)
}

// Phase 1 available tools
var Phase1Tools = []Tool{
    {
        Name:        "kubectl_get_pods",
        Description: "List pods in a namespace with optional label selector",
        // kubectl get pods -n <ns> -l <selector>
    },
    {
        Name:        "kubectl_get_deployments",
        Description: "List deployments in a namespace",
    },
    {
        Name:        "kubectl_logs",
        Description: "Get logs from a pod/container with time range and grep",
        // kubectl logs <pod> -n <ns> --since=<duration> -c <container>
    },
    {
        Name:        "kubectl_logs_selector",
        Description: "Get logs from all pods matching a label selector",
        // kubectl logs -l <selector> -n <ns> --all-containers
    },
    {
        Name:        "kubectl_describe",
        Description: "Describe a Kubernetes resource",
    },
    {
        Name:        "kubectl_get_namespaces",
        Description: "List all namespaces",
    },
    {
        Name:        "kubectl_get_events",
        Description: "Get events in a namespace",
    },
}
```

**Prompt 设计核心思路：**
- System prompt 包含当前集群上下文 (context, namespace, 已知的 deployment 列表)
- 引导 LLM 优先使用 label selector 而非具体 pod name (因为 pod 名是动态的)
- 输出格式要求：先展示要执行的命令，等用户确认（或自动模式直接执行）
- 日志过多时，先统计再展示（避免 token 浪费）

**Agentic Loop 设计 (已确认)：**

AI Agent 采用迭代式执行，最多 **3 次迭代**自动修正错误：

```
User Input → LLM 生成 Action
                │
                ▼
        ┌─ Execute Action ─┐
        │                   │
        ▼                   ▼
    Success             Failure / Empty Result
        │                   │
        ▼                   ▼
  展示结果 + 摘要     迭代次数 < 3?
                        │         │
                       Yes        No
                        │         │
                        ▼         ▼
                  错误信息反馈    展示失败原因
                  给 LLM 修正    让用户调整输入
                        │
                        ▼
                  LLM 生成新 Action
                  (回到 Execute)
```

- 每次迭代在 AI Chat 面板展示中间过程（命令 + 错误 + 修正）
- 配置项 `agent.max_iterations: -1`（默认无限，用户可设置正整数限制）
- 空结果也视为需要修正的情况（如 namespace 名错误、label 不匹配）

**Label Selector 智能发现策略：**

用户通常用服务名（如 "order-service"）而非精确 label 来描述目标。AI Agent 需要智能匹配：

1. 先用服务名查找 Deployment（及 StatefulSet 等，AI 日志查询不受 Resource Browser UI 范围限制）：`kubectl get deploy -n <ns> | grep <name>`
2. 从匹配的 Deployment 中提取 `.spec.selector.matchLabels`，获得准确的 label selector
3. 用该 selector 查询日志：`kubectl logs -l <selector> -n <ns> --all-containers`
4. 如果 Step 1 无匹配，退化为模糊搜索所有资源名，并在 Chat 中列出候选让用户选择

**多 Pod 日志排序策略：**

- 优先尝试解析日志行首的时间戳（支持 ISO 8601、RFC 3339、常见 Java/Go 格式）
- 可解析时：按时间戳归并排序，每行前缀标注 Pod 来源
- 不可解析时：按 Pod 分组展示（而非混排），每个 Pod 的日志保持原始顺序
- 添加 `--timestamps` 参数让 kubelet 注入时间戳（作为兜底方案）

---

## 7. User Flows (用户流程)

### 7.1 首次使用

```
Install → Run korthex → Setup Wizard → Enter TUI
```

### 7.2 手动浏览日志

```
TUI → Select Namespace → Select Deployment → Select Pod → Press 'l' → Log Viewer
```

### 7.3 AI 查询日志

```
TUI → Press ':' → Type NL query → AI builds kubectl → Auto-execute (Safe 命令) → Logs appear in viewer + AI summary in chat
```

### 7.4 AI 持续对话

```
AI Chat → Ask follow-up → AI remembers context → Refine query → Updated results
```

---

## 8. Phase Roadmap (分期规划)

### Phase 1: Log Intelligence (本期) — MVP

**目标：** 一个能用自然语言查日志的 K8s TUI 工具

**P0 (Must Have)：**
- [ ] Setup Wizard (kubeconfig + LLM config + 隐私声明)
- [ ] TUI Resource Browser (Namespace → Deployment → Pod)
- [ ] Log Viewer — 实时流、多 Pod 聚合、severity 着色、关键词搜索、时间范围过滤、分页
- [ ] AI Chat — 自然语言 → client-go API 调用 → 日志写入 Log Viewer + AI 摘要
- [ ] AI Agentic Loop (最多 3 次自动修正)
- [ ] 命令安全白名单 (仅只读操作)
- [ ] Multi LLM Provider (OpenAI / Anthropic / Gemini / Custom OpenAI-compatible)
- [ ] 日志导出到文件
- [ ] 3 种布局模式 (Full TUI / Chat Focus / Log Focus)

**P1 (Should Have)：**
- [ ] 正则过滤 (日志行级正则匹配)
- [ ] JSON 日志格式化 (自动检测 + 展开)
- [ ] Previous 日志 (已重启容器的上一次日志)

### Phase 2: Deep Analysis & Enhanced Browser

**AI 分析增强：**
- [x] AI 日志分析 (异常检测、错误归因、模式识别)
- [x] 对话历史持久化 — 本地 SQLite 存储，支持 `/history` 搜索历史对话、按关键词/时间/namespace 过滤
- [x] Tool 调用结果的智能摘要压缩（上下文窗口优化）
- [x] Ollama 本地模型支持 — 完全离线使用，日志数据不出本机 (往后移，先不做)

**日志可视化：**
- [ ] 日志可视化 (severity 分布柱状图、时间线热力图)
- [ ] 跨 Service 日志关联 (trace ID / request ID)
- [ ] 日志 Bookmark & 标注

**Resource Browser 扩展：**

- [ ] 支持 StatefulSets、DaemonSets、Jobs/CronJobs
- [ ] Pod 详情面板 (资源用量、Events、Conditions)

**数据安全增强：**
- [ ] 正则脱敏规则 — 自动遮盖日志中的 API Key、JWT、邮箱、IP 地址等敏感信息后再发送给 LLM

### Phase 3: Cluster Management

- [ ] AI 辅助 K8s 资源管理 (scale, rollout, restart) — 三次确认机制
- [ ] 危险操作保护 (delete, drain) — 三次确认 + 输入资源名确认
- [ ] 集群健康检查 & 诊断
- [ ] Resource YAML 查看/编辑
- [ ] 事件 (Events) 浏览和分析

### Phase 4: Advanced

- [ ] 多集群支持
- [ ] Plugin 系统 (可扩展 tools)
- [ ] MCP Server 模式 (供其他 AI 工具调用)
- [ ] 团队协作 (共享查询、查询历史)
- [ ] 外部日志源集成 (Loki, ElasticSearch, CloudWatch)

---

## 9. Non-Functional Requirements (非功能需求)

| Requirement | Spec | 备注 |
|------------|------|------|
| **启动时间** | < 2 秒 | 含 kubeconfig 加载 + 首个 informer 初始化 |
| **Namespace 切换** | < 500ms | Informer factory 按需启动 |
| **日志首屏加载** | < 1 秒 (1000 行) | 分页加载，首屏 1000 行 |
| **日志流延迟** | < 200ms | 从 Pod 产生日志到 TUI 展示 |
| **TUI 滚动帧率** | 60fps，无感知卡顿 | Batched flush + delta diff |
| **内存占用** | < 100MB (空闲), < 300MB (10000 行日志 tail) | Ring buffer 有界 |
| **100+ ns / 1000+ pod 集群** | 资源列表加载 < 3 秒 | Informer cache + label selector |
| **高频日志 (>1000 行/秒)** | 不卡不 OOM，可丢行但要告知 | Non-blocking channel + drop policy |
| **安装方式** | 单二进制文件，支持 Homebrew / go install / 直接下载 | |
| **平台支持** | macOS, Linux (amd64, arm64) | |
| **离线模式** | Phase 2 支持 Ollama 本地 LLM 后可完全离线使用 | Phase 1 需要网络访问 LLM API |
| **安全性** | API Key 加密存储，仅只读 kubectl 操作 | |

### 9.1 数据安全与隐私

**Phase 1 策略：全量发送 + 免责声明**

| 数据类型 | 处理方式 |
|---------|---------|
| 用户自然语言输入 | 发送给 LLM Provider |
| kubectl 命令及执行结果 | 发送给 LLM Provider（用于分析和后续对话） |
| 日志原文内容 | 发送给 LLM Provider（用于 AI 分析时） |
| K8s 集群元数据 (namespace/deployment 名称) | 包含在 system prompt 中发送给 LLM Provider |
| LLM API Key | 本地加密存储，不传输 |
| kubeconfig | 仅本地使用，不传输 |

**安全措施：**
- 首次使用时弹出隐私声明，告知用户日志内容将发送给第三方 LLM 服务
- README 和 Setup Wizard 中明确标注：**"日志数据会发送至所选 LLM Provider，请勿在包含敏感数据的集群上使用云端 LLM"**
- Phase 1 所有 LLM Provider 均为云端 API，**不支持离线使用**。对安全敏感环境可使用 `llm.send_logs: false`（AI 仅生成查询，不接收日志原文），或等待 Phase 2 的 Ollama 本地模式
- 配置项 `llm.send_logs: true | false` — 设为 false 时 AI 仅生成 kubectl 命令，不接收日志内容做分析
  - **send_logs=false 下的 Agentic Loop 行为：** AI 仍能看到命令的 **执行状态**（成功/失败/返回行数），但看不到日志原文。足够判断是否需要重试（如命令报错、返回 0 行）。分析摘要功能不可用，Log Viewer 仍正常展示日志。
- 后续版本（Phase 2）增加正则脱敏规则（自动遮盖 API Key、JWT、邮箱等）

---

## 10. Decisions (已确认决策)

### 10.1 AI 命令安全分级

| 命令类型 | 安全级别 | 执行策略 | 适用阶段 |
|---------|---------|---------|---------|
| 日志查询类 (`kubectl logs`, `kubectl get`) | Safe | **直接执行**，无需确认 | Phase 1 |
| 集群信息类 (`kubectl describe`, `kubectl top`) | Safe | **直接执行**，无需确认 | Phase 1 |
| 集群管理类 (`kubectl scale`, `kubectl rollout`) | Dangerous | **三次确认才能执行** | Phase 3+ |
| 危险操作 (`kubectl delete`, `kubectl drain`) | Critical | **三次确认 + 输入资源名确认** | Phase 3+ |

三次确认流程 (Phase 3+):
```
AI: 即将执行: kubectl scale deployment/order-service -n production --replicas=5
    [1/3] 确认执行此命令？(y/n): y
    [2/3] 此操作将影响 production 环境，再次确认？(y/n): y
    [3/3] 最终确认 - 输入资源名 "order-service" 以执行: order-service
    ✓ 执行中...
```

V1 产品仅包含 Safe 级别命令，Dangerous/Critical 命令在 Phase 3 中实现。

### 10.2 开发语言 — Go + Bubble Tea (已确认)

| 维度 | Go + Bubble Tea | Rust + Ratatui |
|------|----------------|----------------|
| 开发速度 | **快** — 语法简单，编译快，client-go 直接用 | 慢 — 所有权/生命周期学习曲线，kube-rs 成熟度低 |
| K8s 生态 | **碾压级** — client-go 官方 client，kubectl/k9s 全是 Go | 可用但弱 — kube-rs 文档少、edge case 多 |
| TUI 框架 | **Bubble Tea** 25k stars, Elm 架构, charmbracelet 全家桶 | Ratatui 12k stars, immediate mode, 更底层 |
| LLM SDK | **官方 SDK 多** — OpenAI/Anthropic 都有高质量 Go SDK | Rust SDK 少，多数需自己封装 HTTP |
| 开源潜力 | **K8s 社区绝大多数是 Go 用户**，贡献门槛低 | Rust 有技术光环但受众窄 |

**结论：Go + Bubble Tea，开发速度快 2-3 倍，K8s 生态完美融合。**

### 10.3 日志缓存策略

- **实时查询为主** — 每次 AI 查询或手动浏览都实时从 K8s API 获取日志
- **内存缓存** — 当前 TUI 会话中已加载的日志保留在内存中，支持回看滚动
- **一键持久化** — 按 `s` 快捷键将当前 Log Viewer 中的日志保存到本地文件
- 默认保存路径：`~/.korthex/logs/{namespace}_{deployment}_{timestamp}.log`
- AI 查询的日志也可一键保存

### 10.4 开源策略 (已确认)

- **License:** Apache 2.0 (与 K8s 生态一致，企业友好)
- **目标：** Star 收割机级别的开源项目
- **Star 收割策略：**
  - 精心设计的 README（GIF demo 动图 + 对比表格 + 一键安装）
  - 发布到 Hacker News / Reddit r/kubernetes / CNCF Slack
  - 提交 awesome-kubernetes 列表
  - 录制 YouTube/Bilibili demo 视频
  - 在 kubectl-ai / k9s 的 GitHub Discussion 中介绍（作为互补工具）
  - 设计精美的 Logo 和品牌标识
  - 提供 Homebrew tap 一键安装

### 10.5 Remaining Open Questions

所有 Open Questions 均已关闭：

1. ~~**日志量限制**~~ ✅ 已决策 — 见 10.6 日志分页策略
2. ~~**多语言 AI 交互**~~ ✅ 已决策 — Phase 1 不做特殊处理。System prompt 用英文以保证 API 调用生成质量，允许用户用任何语言输入，AI 用用户的语言回复。观察实际表现后在 Phase 2 决定是否需要额外处理。

### 10.6 日志分页策略 (已确认)

- **分页展示** — Log Viewer 每页展示 1000 行日志，使用 `Ctrl+D` / `Ctrl+U` 或 `PgDn` / `PgUp` 在 Ring Buffer 内翻半页滚动
- **Ring Buffer 内滚动** — 用户可在 Ring Buffer 保留的日志范围内（最多 `log_lines_limit` 行）自由上下滚动。被淘汰出 buffer 的旧日志不可回看
- **AI Chat 滚动** — AI 回复超长时，Chat 面板支持 `Ctrl+U`/`PgUp` 上翻半页、`Ctrl+D`/`PgDn` 下翻半页。`autoScroll` 标志位自动管理：用户手动向上滚动时暂停自动滚动（header 显示 `SCROLLED` 指示器），滚回底部或 AI 完成回复时恢复
- **LLM 分析范围** — AI 仅分析当前页面的日志内容（约 1000 行），避免 token 爆炸
- **总量提示** — 查询结果超过 5000 行时，在 AI Chat 中提示用户缩小时间范围或增加过滤条件
- **配置项：** `ui.log_page_size: 1000`（可在 config.yaml 中调整）

### 10.7 AI Chat 上下文策略 (已确认)

- **保留最近 10 轮对话**（1 轮 = 1 次用户输入 + AI 完整回复，含 tool 调用及结果）
- **FIFO 淘汰** — 超过 10 轮时丢弃最早的对话轮次
- **始终保留** — System prompt（角色定义 + 安全规则）+ 集群元数据（当前 context / namespace / deployment 列表）不计入轮次上限
- **Tool 调用结果压缩** — 日志类 tool 返回结果超过 2000 字符时，只保留前 500 字符 + 统计摘要（行数、ERROR/WARN 数量），避免历史对话吃掉 context window
- **配置项：** `agent.max_history_turns: 20`
- **用户可感知** — 当旧对话被淘汰时，AI Chat 面板顶部显示「(较早的 N 轮对话已折叠)」

**Phase 1 局限性 & Phase 2 规划：**
- Phase 1 中被淘汰的对话**不可恢复**，关闭 Korthex 后所有对话历史丢失
- **Phase 2 规划：对话历史持久化 + 检索**
  - 所有对话（含 tool 调用结果摘要）自动持久化到本地 SQLite：`~/.korthex/history.db`
  - AI Chat 中输入 `/history` 或自然语言"之前那个 timeout 的问题"时，搜索历史对话并加载上下文
  - 支持按时间、关键词、namespace/deployment 过滤历史记录
  - 可配置自动过期清理（如保留最近 30 天）

---

## 11. Success Metrics (成功指标)

### Phase 1 MVP

| Metric | Target | 度量方式 |
|--------|--------|---------|
| 首次查询到日志的时间 | < 30 秒 (从输入查询到看到结果) | 端到端计时：用户按 Enter → Log Viewer 渲染第一行日志 |
| AI 查询准确率 | > 90% (AI 调用正确的 client-go API 且结果匹配用户意图) | 「正确调用」= API 无报错返回；「匹配意图」= 目标 namespace、Deployment、时间范围正确。以 20 条预设测试 query 评估 |
| 日志加载失败率 | < 5% | 失败 = client-go GetLogs 返回 error 或 30 秒超时。部分 Pod 失败但其他成功计为「部分成功」，不计入失败率 |
| 用户从安装到首次使用 | < 5 分钟 | 从 `brew install korthex` 到完成 Setup Wizard 并看到第一屏 namespace 列表 |

---

## Appendix A: Competitive Tool Details

### kubectl-ai (GoogleCloudPlatform)
- **URL:** github.com/GoogleCloudPlatform/kubectl-ai
- **Stars:** ~7.2k
- **Language:** Go
- **Key Insight:** 支持 MCP Server 模式，可被其他 AI 工具调用。采用 Agentic Loop 架构 (Perception → Reasoning → Planning → Action → Learning)，可配置最大迭代次数。这两个设计值得 Korthex 借鉴。
- **Limitation:** 纯 CLI，无 TUI。不专注日志场景。

### k8sgpt
- **URL:** github.com/k8sgpt-ai/k8sgpt
- **Stars:** ~7.5k
- **Language:** Go
- **Key Insight:** CNCF Sandbox 项目，使用 Analyzer 模式扫描集群问题。可借鉴其 analyzer 架构设计。
- **Limitation:** 分析导向，非交互式对话。无 TUI。

### k9s
- **URL:** github.com/derailed/k9s
- **Stars:** ~33.4k
- **Language:** Go (自研 tcell fork + tview)
- **Key Insight:** 最成功的 K8s TUI 工具。层级导航 + 快捷键 + 插件系统是标杆。
- **Limitation:** 无 AI 能力。日志查看功能基础。

### Gonzo
- **URL:** github.com/control-theory/gonzo
- **Stars:** ~2.5k
- **Language:** Go
- **Key Insight:** TUI + AI 日志分析的先行者。热力图和 severity 分布可视化做得好。可作为 k9s plugin。
- **Limitation:** 仅是日志查看器，不是 K8s 管理工具。AI 能力有限（只能分析单条日志）。

### HolmesGPT
- **URL:** github.com/robusta-dev/holmesgpt
- **Stars:** ~1.8k
- **Language:** Python
- **Key Insight:** CNCF Sandbox 项目。能关联 Prometheus/Loki/Tempo/ArgoCD/Helm 多数据源做根因分析。已有 k9s 插件集成（Shift-H/Shift-Q）。多数据源关联是 Phase 2+ 可借鉴的方向。
- **Limitation:** 非 TUI，是 CLI agent。Python 写的，不适合作为终端常驻工具。调查导向，非日常操作导向。

---

## Appendix B: Third-Party License Attribution

Korthex 使用 **Apache 2.0** 开源。以下第三方项目的代码或设计被参考/借鉴，需在 `NOTICE` 文件中标注：

| Project | License | Usage in Korthex |
|---------|---------|-----------------|
| **derailed/k9s** | Apache-2.0 | Informer/cache 架构、日志流管道模式、资源导航设计 |
| **derailed/tcell** | Apache-2.0 | k9s 的 TUI 底层库（参考设计，不直接使用） |
| **derailed/tview** | MIT | k9s 的 TUI 组件库（参考设计，不直接使用） |
| **charmbracelet/bubbletea** | MIT | TUI 框架（直接依赖） |
| **charmbracelet/lipgloss** | MIT | TUI 样式库（直接依赖） |
| **charmbracelet/bubbles** | MIT | TUI 预制组件（直接依赖） |
| **k8s.io/client-go** | Apache-2.0 | K8s 官方 Go client（直接依赖） |
| **cenkalti/backoff/v4** | MIT | 指数退避重试（直接依赖） |
| **sahilm/fuzzy** | MIT | 模糊搜索（直接依赖） |
| **spf13/viper** | MIT | 配置管理（直接依赖） |
| **control-theory/gonzo** | MIT | 日志可视化设计参考 |
| **GoogleCloudPlatform/kubectl-ai** | Apache-2.0 | Agentic loop 设计参考 |

**License 兼容性：** Apache-2.0 和 MIT 均为宽松许可证，完全互相兼容。MIT 代码纳入 Apache-2.0 项目时保留其 MIT 声明即可。

**NOTICE 文件维护规则：**
- 每次新增直接依赖时更新 NOTICE
- `go.sum` 中的间接依赖由 Go module 机制自动处理
- 参考设计（非直接代码引用）也列出以示尊重

