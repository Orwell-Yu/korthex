<div align="center">

```
 ██╗  ██╗ ██████╗ ██████╗ ████████╗██╗  ██╗███████╗██╗  ██╗
 ██║ ██╔╝██╔═══██╗██╔══██╗╚══██╔══╝██║  ██║██╔════╝╚██╗██╔╝
 █████╔╝ ██║   ██║██████╔╝   ██║   ███████║█████╗   ╚███╔╝
 ██╔═██╗ ██║   ██║██╔══██╗   ██║   ██╔══██║██╔══╝   ██╔██╗
 ██║  ██╗╚██████╔╝██║  ██║   ██║   ██║  ██║███████╗██╔╝ ██╗
 ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝
```

**K8s + Cortex — 你的 Kubernetes 集群的智能大脑**

*对集群说话，看见一切。*

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![Bubble Tea](https://img.shields.io/badge/TUI-Bubble_Tea-FF75B7?style=flat-square)](https://github.com/charmbracelet/bubbletea)
[![client-go](https://img.shields.io/badge/K8s-client--go_v0.30-326CE5?style=flat-square&logo=kubernetes)](https://github.com/kubernetes/client-go)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue?style=flat-square)](LICENSE)
[![Phase](https://img.shields.io/badge/Phase-1_日志智能-8B5CF6?style=flat-square)]()

**[English](README.md)** | **[中文](README_CN.md)**

</div>

---

## 为什么叫 Korthex？

> **Korthex = K8s + Cortex**

Cortex 是人类的**大脑皮层**（cerebral cortex）——负责思考、分析、决策的部分。

Korthex 的寓意是给你的 Kubernetes 集群装上一个「大脑皮层」：

- 不只是**看**数据 —— 而是能**理解**你说的话
- 不只是**展示**日志 —— 而是能**思考**该执行什么命令
- 不只是**列出**资源 —— 而是能**分析**日志里到底出了什么问题

你不再需要记住 `kubectl logs -l app=order-service -n production --all-containers --since=1h | grep ERROR` 这样的命令。

你只需要说：**「order-service 最近有什么报错？」**

---

## Korthex 是什么？

Korthex 把 **Claude Code 式的 AI 对话** 和 **k9s 级别的终端 UI** 融合进一个二进制文件。用自然语言和集群交流、用 vim 风格浏览资源、让 AI 自动捞取和分析日志——全程不离开终端。

```
┌─ 资源浏览器 ─────┬─ 日志查看器 ─────────────────────────────────┐
│ ▸ production     │ 14:23:01 order-svc-7b ERROR  Connection      │
│   staging        │ 14:23:02 order-svc-7b WARN   Retry attempt 3 │
│   monitoring     │ 14:23:03 pay-svc-4a  INFO   Payment success  │
│                  │ 14:23:04 order-svc-7b ERROR  Timeout after 30│
│ Deployments:     ├─ AI 对话 ────────────────────────────────────┤
│  order-service   │ 你: 帮我看看 order-service 的报错日志         │
│  payment-service │                                               │
│  user-service    │ AI: $ kubectl logs -l app=order-service -n    │
│                  │     production --all-containers | grep ERROR   │
│                  │                                               │
│                  │ 过去一小时发现 23 个错误。主要问题是连接       │
│                  │ payment gateway 超时（23 个错误中有 18 个）。  │
│                  │ 这和 12 分钟前 pay-svc 的滚动更新时间吻合。   │
├──────────────────┴──────────────────────────────────────────────┤
│ ctx: prod-cluster | ns: production | openai:gpt-4o | 全屏 [F1] │
└─────────────────────────────────────────────────────────────────┘
```

## 核心能力

### AI 驱动的日志智能

- **自然语言查询**：*「为什么 order-service 一直报错？」*
- **Agentic Loop**：LLM 自主发现服务、捞取日志、分析问题、给出总结
- **智能标签发现**：你说服务名，AI 自动通过 Deployment 找到 label selector，定位所有 Pod
- **大集群支持**：`filter` 参数支持在数百个命名空间中按名称搜索，不再被截断
- **资源面板联动**：AI 查询资源列表时，左侧 Resource Browser 自动同步导航到对应层级，方便交互选择
- **Thinking 动画**：AI 处理查询时显示 braille 点阵旋转动画，不再让用户以为卡住了
- **多 LLM 支持**：OpenAI / Anthropic / Gemini / 任何 OpenAI 兼容端点

### k9s 级终端界面

- **层级资源浏览**：Namespace > Deployment > Pod > Container，一路 Enter 下钻
- **实时日志流**：severity 着色、正则搜索、Follow 模式
- **水平滚动**：长日志行可用 `h`/`l` 左右滚动查看完整内容
- **日志导出**：`s` 键一键保存日志到 `~/.korthex/logs/`
- **三种布局**：全屏三面板 (F1) / 对话优先 (F2) / 日志全屏 (F3)
- **Vim 风格导航**：`j`/`k` 上下、`/` 搜索、`g`/`G` 跳转、`:` 进入对话

### 生产级可靠性

- **Informer/Cache 架构**：零冗余 API 调用，和 k9s 同样的模式
- **万行日志缓冲**：10,000 行 Ring Buffer，支持 Follow、导出、剪贴板复制
- **优雅退出**：SIGINT/SIGTERM 信号处理、context 级联取消
- **Phase 1 只读**：所有操作都是读取，你的集群绝对安全

## 环境要求

- **Go 1.24+** — [安装](https://go.dev/dl/)
- **kubectl 已配置** — 有效的 `~/.kube/config`（或 `KUBECONFIG` 环境变量）且能访问集群
- **LLM API Key** — OpenAI / Anthropic / Gemini / 或任何 OpenAI 兼容端点（任选其一）

## 快速开始

```bash
# 从源码构建
git clone https://github.com/Orwell-Yu/korthex.git
cd Korthex
make build      # → bin/korthex

# 设置 LLM API Key（任选一个）
export OPENAI_API_KEY="sk-..."
# export ANTHROPIC_API_KEY="sk-ant-..."
# export GEMINI_API_KEY="..."

# 启动 — 首次运行自动进入配置向导
./bin/korthex
```

### 命令行参数

```bash
./bin/korthex                    # 使用默认/自动检测的配置启动
./bin/korthex --config path.yaml # 指定配置文件路径
./bin/korthex --version          # 打印版本号并退出
```

首次启动时，Korthex 会自动检测你的 kubeconfig 并引导你完成多步配置：

```
  Welcome to Korthex! Let's get you set up.

  Step 1/5: Kubernetes Configuration
  ───────────────────────────────────
  Detected kubeconfig at: ~/.kube/config
  Available contexts:
  > prod-cluster
    staging-cluster
    minikube

  Use arrows to select, Enter to confirm

  ---

  Step 2/5: LLM Provider
  ────────────────────────
  > OpenAI (API Key)
    Anthropic Claude (API Key)
    Google Gemini (API Key)
    Custom OpenAI-compatible endpoint (API Key + Base URL)

  ---

  Step 3/5: API Key
  ───────────────────
  API Key: ****

  Step 4/5: Model Selection
  ──────────────────────────
  Default model: gpt-4o
  Override (leave empty for default):

  Step 5/5: Privacy Disclosure
  ─────────────────────────────
  Do you accept and wish to continue? (y/n)
```

## 使用方法

配置完成后，Korthex 启动三面板 TUI 界面：

```
┌─ 资源浏览器 ─────┬─ 日志查看器 ─────────────────────┐
│ 命名空间:        │ （你或 AI 获取日志时，            │
│ > production     │  日志会自动显示在这里）           │
│   staging        ├─ AI 对话 ─────────────────────────┤
│   monitoring     │ > 问任何关于集群的问题            │
├──────────────────┴───────────────────────────────────┤
│ ctx: prod-cluster | ns: production | openai:gpt-4o   │
└──────────────────────────────────────────────────────┘
```

**基本工作流：**
1. **浏览资源** — 用 `j`/`k` 上下导航，`Enter` 进入下一层（Namespace > Deployment > Pod > Container），`Esc` 返回上一层
2. **和 AI 对话** — 按 `:` 聚焦对话输入框，输入问题如 *「order-service 为什么报错？」*，按 `Enter`。AI 会自主调用 kubectl、拉取日志、分析总结
3. **查看日志** — 在 Pod 上按 `l` 流式查看日志，或让 AI 自动推送。`h`/`l` 左右滚动，`Ctrl+U`/`Ctrl+D` 上下翻页，`/` 搜索，`F` 实时跟踪，`s` 导出到文件
4. **切换布局** — `F1` 三面板全屏、`F2` 对话优先、`F3` 日志全屏
5. **退出** — `q`（对话输入时无效）或 `Ctrl+C`

## 键盘快捷键

| 按键 | 场景 | 功能 |
|------|------|------|
| `j` / `k` | 全局 | 上下导航 |
| `h` / `l` | 日志 | 左右水平滚动 |
| `0` | 日志 | 重置水平滚动到行首 |
| `Enter` | 资源浏览 | 进入下一层 |
| `Esc` | 资源浏览 | 返回上一层 |
| `l` | 资源浏览 | 查看选中 Pod 的日志 |
| `:` | 全局 | 聚焦 AI 对话输入 |
| `/` | 资源/日志 | 搜索 / 过滤 |
| `F1` `F2` `F3` | 全局 | 切换布局模式 |
| `Tab` | 全局 | 切换面板焦点 |
| `f` | 日志 | 正则过滤模式 |
| `F` | 日志 | 切换 Follow 模式 |
| `s` | 日志 | 保存日志到 `~/.korthex/logs/` |
| `Ctrl+C` | 对话 | 取消正在运行的 AI 查询 |
| `Ctrl+U` / `PgUp` | 对话/日志 | 向上滚动半页 |
| `Ctrl+D` / `PgDn` | 对话/日志 | 向下滚动半页 |
| `↑` / `↓` | 对话 | 浏览历史查询（类似 shell 历史） |
| `?` | 全局 | 显示帮助 |
| `q` | 全局 | 退出（对话输入时无效） |

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                    cmd/korthex (main.go)                     │
│                     internal/app                             │
├──────────────────────────┬──────────────────────────────────┤
│       internal/ui        │      第 3 层: 展示层             │
│   Bubble Tea TUI 面板    │                                  │
├──────────────────────────┼──────────────────────────────────┤
│     internal/agent       │      第 2 层: 智能层             │
│   Agentic Loop + 安全    │                                  │
├─────────────┬────────────┼──────────────────────────────────┤
│ internal/k8s│internal/llm│      第 1 层: 核心层             │
│  client-go  │  多 LLM    │                                  │
├─────────────┴────────────┼──────────────────────────────────┤
│  internal/config         │      第 0 层: 基础层             │
│  pkg/logparse            │                                  │
└──────────────────────────┴──────────────────────────────────┘
```


## 配置

配置文件位置：`~/.config/korthex/config.yaml`

```yaml
kubernetes:
  kubeconfig: ~/.kube/config
  default_context: ""              # 空 = 使用当前 context

llm:
  provider: openai                 # openai | anthropic | gemini | custom
  api_key: ""                      # 建议用环境变量: KORTHEX_LLM_API_KEY
  model: gpt-4o
  base_url: ""                     # 自定义 OpenAI 兼容端点
  temperature: 0.1
  max_tokens: 4096
  send_logs: true                  # false = AI 只生成命令，不分析日志内容

agent:
  max_iterations: 20               # Agentic Loop 最大迭代次数
  max_history_turns: 20            # 对话上下文保留轮数

ui:
  theme: dark                      # dark | light | dracula | nord
  log_lines_limit: 10000           # Ring Buffer 最大行数
  log_page_size: 1000              # 日志查看器每页行数
  default_log_since: 1h            # 默认日志查询时间范围
```

环境变量优先级高于配置文件：
```bash
KORTHEX_LLM_API_KEY    # 最高优先级
OPENAI_API_KEY         # 自动识别
ANTHROPIC_API_KEY      # 自动识别
GEMINI_API_KEY         # 自动识别
```

## 从源码构建

```bash
git clone https://github.com/Orwell-Yu/korthex.git
cd Korthex
```

### Make 命令

| 命令 | 说明 |
|------|------|
| `make build` | 编译二进制到 `bin/korthex`（通过 `-ldflags` 嵌入 git 版本号） |
| `make run` | 编译并运行 |
| `make test` | 运行所有测试，带 race 检测和覆盖率（`go test -v -race -coverprofile`） |
| `make test-short` | 跳过集成测试（`-short` 标志） |
| `make lint` | 运行 `golangci-lint` |
| `make fmt` | 格式化代码（`gofmt` + `goimports`） |
| `make vet` | 运行 `go vet` |
| `make ci` | 完整 CI 流程：lint + test + build |
| `make clean` | 清理 `bin/`、`coverage.out`、`dist/` |

### 交叉编译

纯 Go 无 CGO 依赖，交叉编译开箱即用：

```bash
GOOS=linux GOARCH=amd64 make build    # Linux x86_64
GOOS=darwin GOARCH=arm64 make build   # macOS Apple Silicon
```

需要 Go 1.24+。

## 路线图

| 阶段 | 方向 | 状态 |
|------|------|------|
| **Phase 1** | 日志智能 — TUI + AI 对话 + 日志查看 + 多 LLM 支持 | **开发中** |
| Phase 2 | 深度分析 — Ollama 本地模型、日志可视化、对话持久化、脱敏规则 | 规划中 |
| Phase 3 | 集群管理 — AI 辅助写操作（scale/restart/delete），三次确认机制 | 规划中 |
| Phase 4 | 高级特性 — 多集群、插件系统、MCP Server、外部日志源接入 | 规划中 |

## 许可证

[Apache 2.0](LICENSE) — 与 Kubernetes 生态一致，企业友好。

---

<div align="center">

**基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [client-go](https://github.com/kubernetes/client-go) + AI 构建**

*别再复制 Pod 名字了。直接和你的集群对话吧。*

</div>
