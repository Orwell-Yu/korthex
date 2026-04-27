# Korthex Parallel Development — Git Worktree Workflow

> **Repo:** `https://github.com/Orwell-Yu/korthex.git`

## Overview

使用 git worktree 实现多 Claude Code 实例并行开发。每个 worktree 是独立的工作目录 + 独立的分支，互不干扰。

```
/Users/mio/
├── korthex/                  ← main 分支 (主仓库)
├── korthex-logparse/         ← feat/logparse 分支 (worktree)
├── korthex-config/           ← feat/config 分支 (worktree)
├── korthex-k8s/              ← feat/k8s 分支 (worktree)
├── korthex-llm/              ← feat/llm 分支 (worktree)
├── korthex-agent/            ← feat/agent 分支 (worktree)
├── korthex-ui-base/          ← feat/ui-base 分支 (worktree)
└── korthex-ui-panels/        ← feat/ui-panels 分支 (worktree)
```

## Wave Execution Order

```
Wave 0 (1 instance)      scaffold          ← 必须最先完成
    │
Wave 1 (2 parallel)      logparse + config ← 基础层，互不依赖
    │
Wave 2 (2 parallel)      k8s + llm         ← 核心层，互不依赖
    │
Wave 3a (2 parallel)     agent + ui-base   ← agent 与 ui-base 可并行（共享 skeleton 接口）
    │                         │
Wave 3b (1 instance)      ui-panels ───────┘ ← 依赖 ui-base 完成，可与 agent 并行
    │
Wave 4 (1 instance)      integration       ← 必须最后
```

> **Wave 3 注意:** `ui-panels` 依赖 `ui-base` 的基础设施代码（styles, layout, ringbuffer, messages, app.go shell），
> 必须在 `ui-base` 合并后才能启动。但它可以与 `agent` 并行。

---

## Step 0: 初始化仓库

```bash
# 克隆仓库（如果还没有 clone）
cd /Users/mio
git clone https://github.com/Orwell-Yu/korthex.git korthex
cd korthex

# 如果已有目录但没有 git，初始化并关联远程
cd /Users/mio/korthex
git init
git remote add origin https://github.com/Orwell-Yu/korthex.git

# 把当前文件加入 main 分支
git add -A
git commit -m "docs: project documentation and architecture"
git push -u origin main
```

---

## Step 1: Wave 0 — Scaffold (主仓库，单实例)

```bash
cd /Users/mio/korthex          # 在 main 分支上工作

# 打开 Claude Code，执行 wave0_scaffold.md
# scaffold 完成后:
git add -A
git commit -m "feat: scaffold — interface skeletons, go.mod, all dependencies"
git push origin main
```

---

## Step 2: Wave 1 — logparse + config (2 个并行 worktree)

### 创建 worktree

```bash
cd /Users/mio/korthex

# 创建两个 worktree，各自在独立分支上
git worktree add ../korthex-logparse -b feat/logparse
git worktree add ../korthex-config   -b feat/config
```

### 开发

打开两个终端，分别启动 Claude Code：

```bash
# Terminal A
cd /Users/mio/korthex-logparse
# 启动 Claude Code，执行 wave1_logparse.md

# Terminal B
cd /Users/mio/korthex-config
# 启动 Claude Code，执行 wave1_config.md
```

### 合并 + 清理

两个实例都完成后：

```bash
cd /Users/mio/korthex   # 回到主仓库 (main)

# 合并 logparse
git merge feat/logparse --no-ff -m "feat: implement pkg/logparse — severity, timestamp, JSON parser"

# 合并 config
git merge feat/config --no-ff -m "feat: implement internal/config — Viper config + Setup Wizard"

# 如果有冲突（概率极低，因为文件不重叠）：
# git mergetool  或手动解决后 git merge --continue

# 推送合并结果
git push origin main

# 清理 worktree
git worktree remove ../korthex-logparse
git worktree remove ../korthex-config

# 清理远程分支（可选）
git branch -d feat/logparse feat/config
```

---

## Step 3: Wave 2 — k8s + llm (2 个并行 worktree)

### 创建 worktree

```bash
cd /Users/mio/korthex

git worktree add ../korthex-k8s -b feat/k8s
git worktree add ../korthex-llm -b feat/llm
```

### 开发

```bash
# Terminal C
cd /Users/mio/korthex-k8s
# 启动 Claude Code，执行 wave2_k8s.md

# Terminal D
cd /Users/mio/korthex-llm
# 启动 Claude Code，执行 wave2_llm.md
```

### 合并 + 清理

```bash
cd /Users/mio/korthex

git merge feat/k8s --no-ff -m "feat: implement internal/k8s — client-go, Informer LRU, log streaming"
git merge feat/llm --no-ff -m "feat: implement internal/llm — OpenAI, Anthropic, Gemini adapters"
git push origin main

git worktree remove ../korthex-k8s
git worktree remove ../korthex-llm
git branch -d feat/k8s feat/llm
```

---

## Step 4: Wave 3 — agent + ui-base + ui-panels (2~3 个并行 worktree)

### 创建 worktree

```bash
cd /Users/mio/korthex

# agent 和 ui-base 可以完全并行
git worktree add ../korthex-agent   -b feat/agent
git worktree add ../korthex-ui-base -b feat/ui-base

# ui-panels 需要等 ui-base 完成后再创建（依赖 ui-base 的基础设施）
# 或者也可以同时创建，在 prompt 里让 Claude Code 自己处理占位代码
```

### 开发

```bash
# Terminal E
cd /Users/mio/korthex-agent
# 启动 Claude Code，执行 wave3_agent.md

# Terminal F
cd /Users/mio/korthex-ui-base
# 启动 Claude Code，执行 wave3_ui_base.md

# -- 等 ui-base 完成后 --

# 先合并 ui-base 到 main
cd /Users/mio/korthex
git merge feat/ui-base --no-ff -m "feat: implement internal/ui — TUI infrastructure + Resource Browser"
git push origin main
git worktree remove ../korthex-ui-base
git branch -d feat/ui-base

# 再创建 ui-panels worktree（基于最新 main，包含 ui-base 代码）
git worktree add ../korthex-ui-panels -b feat/ui-panels

# Terminal G
cd /Users/mio/korthex-ui-panels
# 启动 Claude Code，执行 wave3_ui_panels.md
```

### 合并 + 清理

```bash
cd /Users/mio/korthex

# 合并 agent（可能在 ui-panels 之前或之后完成，都可以）
git merge feat/agent --no-ff -m "feat: implement internal/agent — agentic loop, safety checker, 7 tools"

# 合并 ui-panels
git merge feat/ui-panels --no-ff -m "feat: implement internal/ui — Log Viewer + AI Chat panels"

git push origin main

git worktree remove ../korthex-agent
git worktree remove ../korthex-ui-panels 2>/dev/null  # 如果还在
git branch -d feat/agent feat/ui-panels
```

---

## Step 5: Wave 4 — Integration (主仓库，单实例)

```bash
cd /Users/mio/korthex   # 直接在 main 上

# 启动 Claude Code，执行 wave4_integration.md
# 完成后:
git add -A
git commit -m "feat: integration — app lifecycle, main.go, E2E verification"
git push origin main

# 打 tag
git tag -a v0.1.0 -m "Phase 1: AI-native Kubernetes TUI"
git push origin v0.1.0
```

---

## Quick Reference: 常用命令

```bash
# 查看所有 worktree
git worktree list

# 强制删除有未提交内容的 worktree
git worktree remove ../korthex-xxx --force

# 在 worktree 里提交（就像正常 git 操作）
cd /Users/mio/korthex-logparse
git add -A
git commit -m "feat: implement logparse module"

# 从 worktree 里推送到远程（便于备份 / MR）
cd /Users/mio/korthex-logparse
git push -u origin feat/logparse

# 用 GitLab MR 代替本地 merge（更正式）
# 在 GitLab 上 feat/logparse → main 创建 Merge Request
```

---

## File Ownership Matrix (零冲突保证)

每个 worktree 只修改自己的文件，绝不触碰其他模块：

| Worktree | Branch | 拥有的文件 | 绝不触碰 |
|----------|--------|-----------|----------|
| korthex-logparse | feat/logparse | `pkg/logparse/*.go` | 其他所有 |
| korthex-config | feat/config | `internal/config/*.go` | 其他所有 |
| korthex-k8s | feat/k8s | `internal/k8s/*.go` | llm, agent, ui |
| korthex-llm | feat/llm | `internal/llm/*.go` | k8s, agent, ui |
| korthex-agent | feat/agent | `internal/agent/*.go` | k8s, llm, ui |
| korthex-ui-base | feat/ui-base | `internal/ui/*.go` | agent, k8s, llm |
| korthex-ui-panels | feat/ui-panels | `internal/ui/logviewer.go`, `chat.go` | agent, k8s, llm |

> 因为 Layer 规则的存在，每个模块的文件集合天然不重叠，merge 时几乎不会有冲突。
> 唯一可能的"冲突"是 `go.sum`，这个用 `go mod tidy` 自动解决。

---

## Interface Change Protocol (接口变更协议)

如果某个 Wave 实例在开发中发现 Wave 0 定义的接口不满足需求：

1. **停下当前工作**，不要自行修改接口
2. **记录需求**：在 worktree 根目录创建 `INTERFACE_CHANGE.md`，写明：
   - 哪个接口需要改
   - 为什么需要改（具体场景）
   - 建议的新签名
3. **通知操作者**（你，回到主仓库执行变更）：
   ```bash
   # 在 main 分支上修改 skeleton 接口
   cd /Users/mio/korthex
   # 修改对应的 interface 文件
   # 更新所有相关的 mock
   git add -A && git commit -m "fix: update XxxInterface — add YyyMethod per agent requirement"
   git push origin main
   ```
4. **各 worktree 同步**：
   ```bash
   cd /Users/mio/korthex-agent  # 或其他 worktree
   git fetch origin main
   git rebase origin/main
   ```

> **原则：接口是合约，单方面变更会破坏其他并行实例。所有接口变更必须经过 main。**

---

## Troubleshooting

### Q: merge 时 go.sum 冲突了怎么办？

```bash
# 接受任意一方，然后重新生成
git checkout --theirs go.sum
go mod tidy
git add go.sum
git merge --continue
```

### Q: worktree 里 `go build ./...` 报错找不到其他模块的类型？

这说明 Wave 0 的 skeleton 不完整。回到 main 补充缺失的接口定义，然后在 worktree 里：

```bash
git fetch origin main
git rebase origin/main
```

### Q: 想用 GitLab MR 而不是本地 merge？

完全可以。每个 worktree 完成后 push 到远程，在 GitLab 上创建 MR：

```bash
cd /Users/mio/korthex-logparse
git push -u origin feat/logparse
# 然后在 GitLab 创建 feat/logparse → main 的 Merge Request
```

### Q: 某个 wave 开发到一半想暂停？

worktree 是持久的，直接退出 Claude Code 即可。下次继续：

```bash
cd /Users/mio/korthex-logparse
# 重新启动 Claude Code，它会从上次的代码状态继续
```

### Q: 想同时推送所有分支到远程备份？

```bash
cd /Users/mio/korthex
for wt in $(git worktree list --porcelain | grep 'worktree ' | awk '{print $2}'); do
  (cd "$wt" && branch=$(git branch --show-current) && git push -u origin "$branch" 2>/dev/null && echo "pushed $branch")
done
```

---

## Prompt Files

| File | Wave | Instance | Module | Est. Time |
|------|------|----------|--------|-----------|
| `wave0_scaffold.md` | 0 | - | All (skeleton) | 1-2h |
| `wave1_logparse.md` | 1 | A | pkg/logparse | 2-3h |
| `wave1_config.md` | 1 | B | internal/config | 3-4h |
| `wave2_k8s.md` | 2 | C | internal/k8s | 6-8h |
| `wave2_llm.md` | 2 | D | internal/llm | 4-5h |
| `wave3_agent.md` | 3 | E | internal/agent | 5-7h |
| `wave3_ui_base.md` | 3 | F | internal/ui (infra) | 6-8h |
| `wave3_ui_panels.md` | 3 | G | internal/ui (panels) | 5-7h |
| `wave4_integration.md` | 4 | - | app + cmd + E2E | 3-4h |
