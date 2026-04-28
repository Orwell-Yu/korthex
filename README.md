<div align="center">

```
 ██╗  ██╗ ██████╗ ██████╗ ████████╗██╗  ██╗███████╗██╗  ██╗
 ██║ ██╔╝██╔═══██╗██╔══██╗╚══██╔══╝██║  ██║██╔════╝╚██╗██╔╝
 █████╔╝ ██║   ██║██████╔╝   ██║   ███████║█████╗   ╚███╔╝
 ██╔═██╗ ██║   ██║██╔══██╗   ██║   ██╔══██║██╔══╝   ██╔██╗
 ██║  ██╗╚██████╔╝██║  ██║   ██║   ██║  ██║███████╗██╔╝ ██╗
 ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝
```

**K8s + Cortex — The Intelligent Brain of Your Kubernetes Cluster**

*Speak to your cluster, see everything.*

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![Bubble Tea](https://img.shields.io/badge/TUI-Bubble_Tea-FF75B7?style=flat-square)](https://github.com/charmbracelet/bubbletea)
[![client-go](https://img.shields.io/badge/K8s-client--go_v0.30-326CE5?style=flat-square&logo=kubernetes)](https://github.com/kubernetes/client-go)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue?style=flat-square)](LICENSE)
[![Phase](https://img.shields.io/badge/Phase-2_Deep_Analysis-8B5CF6?style=flat-square)]()

**[English](README.md)** | **[中文](README_CN.md)**

</div>

---

## What is Korthex?

Korthex fuses **Claude Code-style AI conversation** with a **k9s-grade terminal UI** into a single binary. Talk to your cluster in plain language, browse resources with vim-like navigation, and let AI fetch and analyze logs — all without leaving your terminal.

```
┌─ Resources ──────┬─ Log Viewer ─────────────────────────────────┐
│ ▸ production     │ 14:23:01 order-svc-7b ERROR  Connection      │
│   staging        │ 14:23:02 order-svc-7b WARN   Retry attempt 3 │
│   monitoring     │ 14:23:03 pay-svc-4a  INFO   Payment success  │
│                  │ 14:23:04 order-svc-7b ERROR  Timeout after 30│
│ Deployments:     ├─ AI Chat ────────────────────────────────────┤
│  order-service   │ You: show me error logs from order-service    │
│  payment-service │                                               │
│  user-service    │ AI: $ kubectl logs -l app=order-service -n    │
│                  │     production --all-containers | grep ERROR   │
│                  │                                               │
│                  │ Found 23 errors in the last hour. The main    │
│                  │ issue is connection timeouts to the payment    │
│                  │ gateway (18/23 errors). This correlates with  │
│                  │ the pay-svc deployment rolling out 12 min ago.│
├──────────────────┴──────────────────────────────────────────────┤
│ ctx: prod-cluster | ns: production | openai:gpt-4o | Full [F1] │
└─────────────────────────────────────────────────────────────────┘
```

## Features

**AI-Powered Log Intelligence**
- Natural language queries: *"Why is order-service throwing errors?"*
- Agentic loop: LLM autonomously discovers services, fetches logs, and summarizes findings
- Smart label selector discovery — mentions a service name, AI finds the right pods
- Large cluster support: `filter` parameter for searching across 100s of namespaces
- Resource Browser auto-sync: AI tool calls automatically navigate the left panel to matching resources
- Animated thinking indicator — braille dot spinner while AI processes your query
- Multi-provider: OpenAI, Anthropic, Gemini, or any OpenAI-compatible endpoint

**k9s-Grade Terminal UI**
- Hierarchical resource browser: Namespace > Deployment > Pod > Container
- Real-time log streaming with severity coloring and regex search
- Horizontal scrolling for long log lines (`h`/`l`), log export to file (`s`)
- Three layout modes: Full TUI (F1) / Chat Focus (F2) / Log Focus (F3)
- Vim-like navigation: `j`/`k`, `/` search, `g`/`G` jump, `:` command

**Production Ready**
- Informer/Cache architecture (zero redundant API calls, same pattern as k9s)
- 10,000-line ring buffer with follow mode, export, and clipboard support
- Graceful shutdown, context cancellation, configurable retry with exponential backoff
- Phase 1: strictly read-only — your cluster is safe

**Data Safety** (Phase 2)
- Automatic regex redaction of sensitive data (JWT, API keys, emails, IPs, credit cards) before LLM calls
- Customizable redaction rules with ability to disable specific built-in rules
- Log bookmarks: mark important lines with `m`, navigate with `n`/`N`, list with `'`

**Enhanced Browser** (Phase 2)
- StatefulSet, DaemonSet, Job, CronJob browsing via Resource Registry pattern
- Pod detail panel with conditions, events, container metrics (press `d` on a pod)
- Quick resource type switching with number keys `1`-`5`

**Deep Analysis** (Phase 2)
- AI-powered severity statistics, log period comparison, cross-service trace correlation
- Conversation history persistence with full-text search (SQLite-backed, `/history`)
- Rich Markdown rendering for AI responses (headings, code blocks, tables, lists)

## Prerequisites

- **kubectl configured** — a valid `~/.kube/config` (or `KUBECONFIG` env var) with cluster access
- **LLM API key** — one of: OpenAI, Anthropic, Gemini, or any OpenAI-compatible endpoint
- **Go 1.24+** — only needed if [building from source](#build-from-source) ([install](https://go.dev/dl/))

## Quick Start

### Install via Homebrew (macOS / Linux)

```bash
brew tap Orwell-Yu/tap
brew install korthex
```

### Build from Source

```bash
git clone https://github.com/Orwell-Yu/korthex.git
cd Korthex
make build      # → bin/korthex
```

### Run

```bash
# Set your LLM API key (pick one)
export OPENAI_API_KEY="sk-..."
# export ANTHROPIC_API_KEY="sk-ant-..."
# export GEMINI_API_KEY="..."

# Run — setup wizard launches on first run
korthex
```

### CLI Flags

```bash
./bin/korthex                    # start with default/auto-detected config
./bin/korthex --config path.yaml # use a custom config file
./bin/korthex --version          # print version and exit
```

First launch detects your kubeconfig and walks you through a multi-step setup wizard:

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
    Custom endpoint (OpenAI-compatible)
    Custom endpoint (Anthropic-compatible)

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

## Usage

After setup, Korthex launches a three-panel TUI:

```
┌─ Resources ──────┬─ Log Viewer ──────────────────────┐
│ Namespaces:      │ (logs appear here automatically   │
│ > production     │  when you or AI fetch them)       │
│   staging        ├─ AI Chat ─────────────────────────┤
│   monitoring     │ > ask anything about your cluster │
├──────────────────┴───────────────────────────────────┤
│ ctx: prod-cluster | ns: production | openai:gpt-4o   │
└──────────────────────────────────────────────────────┘
```

**Basic workflow:**
1. **Browse resources** — use `j`/`k` to navigate, `Enter` to drill down (Namespace > Deployment > Pod > Container), `Esc` to go back
2. **Talk to AI** — press `:` to focus the chat input, type a question like *"why is order-service throwing errors?"*, press `Enter`. The AI autonomously calls kubectl, fetches logs, and summarizes findings
3. **View logs** — press `l` on a pod to stream its logs, or let AI push logs automatically. Use `h`/`l` to scroll horizontally, `Ctrl+U`/`Ctrl+D` to scroll vertically, `/` to search, `F` to follow, `s` to export
4. **Switch layouts** — `F1` full 3-panel, `F2` chat focus, `F3` log focus
5. **Quit** — `q` (when not in chat input) or `Ctrl+C`

## Keyboard Shortcuts

| Key | Context | Action |
|-----|---------|--------|
| `j` / `k` | Global | Navigate up/down |
| `h` / `l` | Logs | Scroll left/right (horizontal) |
| `0` | Logs | Reset horizontal scroll to start |
| `Enter` | Resources | Drill into selected item |
| `Esc` | Resources | Go back one level |
| `l` | Resources | Stream logs for selected pod |
| `:` | Global | Focus AI chat input |
| `/` | Resources/Logs | Search / filter |
| `F1` `F2` `F3` | Global | Switch layout mode |
| `Tab` | Global | Cycle panel focus |
| `f` | Logs | Filter mode (regex) |
| `F` | Logs | Toggle follow mode |
| `s` | Logs | Save logs to `~/.korthex/logs/` |
| `Ctrl+C` | Chat | Cancel running AI query |
| `Ctrl+U` / `PgUp` | Chat/Logs | Scroll up half page |
| `Ctrl+D` / `PgDn` | Chat/Logs | Scroll down half page |
| `1`-`5` | Resources | Switch resource type (Deploy/SS/DS/Job/CJ) |
| `d` | Resources (Pod) | Open Pod detail panel |
| `m` | Logs | Toggle bookmark on current line |
| `'` | Logs | Open bookmark list |
| `n` / `N` | Logs | Jump to next/previous bookmark |
| `Up` / `Down` | Chat | Browse previous queries (shell-style history) |
| `?` | Global | Show help overlay |
| `q` | Global | Quit (not in chat input) |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    cmd/korthex (main.go)                     │
│                     internal/app                             │
├──────────────────────────┬──────────────────────────────────┤
│       internal/ui        │         Layer 3: Presentation    │
│   Bubble Tea TUI Panels  │                                  │
├──────────────────────────┼──────────────────────────────────┤
│     internal/agent       │         Layer 2: Intelligence    │
│   Agentic Loop + Safety  │                                  │
├─────────────┬────────────┼──────────────────────────────────┤
│ internal/k8s│internal/llm│         Layer 1: Core            │
│  client-go  │ Multi-LLM  │                                  │
│             │            │  internal/history (SQLite)        │
├─────────────┴────────────┼──────────────────────────────────┤
│  internal/config         │         Layer 0: Foundation      │
│  pkg/logparse            │                                  │
│  pkg/redact              │                                  │
└──────────────────────────┴──────────────────────────────────┘
```

Each layer only imports layers below it. No circular dependencies. Every module has its own interface, mock, and test suite — developed and tested in isolation.

## Configuration

Config file: `~/.config/korthex/config.yaml`

```yaml
kubernetes:
  kubeconfig: ~/.kube/config
  default_context: ""              # empty = current context

llm:
  provider: openai                 # openai | anthropic | gemini
  api_key: ""                      # prefer env var: KORTHEX_LLM_API_KEY
  model: gpt-4o
  base_url: ""                     # optional: for proxy/gateway endpoints
  temperature: 0.1
  max_tokens: 4096
  send_logs: true                  # false = AI generates commands only

agent:
  max_iterations: 20
  max_history_turns: 20

ui:
  theme: dark                      # dark | light | dracula | nord
  log_lines_limit: 10000           # ring buffer max log lines
  log_page_size: 1000              # log viewer lines per page
  default_log_since: 1h            # default time range for log queries

# Phase 2
privacy:
  redaction:
    enabled: true                  # redact sensitive data before LLM calls
    rules: []                      # custom rules: [{name, pattern, replacement}]
    disable_builtin: []            # builtin rule names to disable (e.g., ipv4)

history:
  enabled: true                    # conversation history persistence
  retention_days: 30               # auto-cleanup older sessions on startup
  db_path: "~/.korthex/history.db"

analysis:
  trace_id_patterns: []            # custom trace ID patterns: [{name, pattern}]
```

Environment variables take precedence over config file:
```bash
KORTHEX_LLM_API_KEY    # highest priority for API key
OPENAI_API_KEY         # also auto-detected
ANTHROPIC_API_KEY      # also auto-detected
GEMINI_API_KEY         # also auto-detected
```

## Building from Source

```bash
git clone https://github.com/Orwell-Yu/korthex.git
cd Korthex
```

### Make Targets

| Command | Description |
|---------|-------------|
| `make build` | Build binary to `bin/korthex` (embeds git version via `-ldflags`) |
| `make run` | Build and run in one step |
| `make test` | Run all tests with race detector and coverage (`go test -v -race -coverprofile`) |
| `make test-short` | Run tests without integration tests (`-short` flag) |
| `make lint` | Run `golangci-lint` |
| `make fmt` | Format code (`gofmt` + `goimports`) |
| `make vet` | Run `go vet` |
| `make ci` | Full CI pipeline: lint + test + build |
| `make clean` | Remove `bin/`, `coverage.out`, `dist/` |

### Cross-compilation

The binary is pure Go with no CGO dependencies, so cross-compilation works out of the box:

```bash
GOOS=linux GOARCH=amd64 make build    # Linux x86_64
GOOS=darwin GOARCH=arm64 make build   # macOS Apple Silicon
```

Requires Go 1.24+.

## Roadmap

| Phase | Focus | Status |
|-------|-------|--------|
| **Phase 1** | Log Intelligence — TUI + AI Chat + Log Viewer + Multi-LLM | **Done** |
| **Phase 2** | Deep Analysis — log analysis, conversation history, data redaction, enhanced browser, Markdown rendering | **In Progress** |
| Phase 3 | Cluster Management — AI-assisted write ops with triple confirmation | Planned |
| Phase 4 | Advanced — Multi-cluster, plugin system, MCP Server | Planned |

## License

[Apache 2.0](LICENSE) — consistent with the Kubernetes ecosystem.

---

<div align="center">

**Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [client-go](https://github.com/kubernetes/client-go) + AI**

*Stop copying pod names. Start talking to your cluster.*

</div>
