# internal/llm — CLAUDE.md

> Multi-provider LLM communication: unified interface + SDK adapters for OpenAI, Anthropic, Gemini.

## Parent Reference

- **When confused about architecture:** Read [`../../CLAUDE.md`](../../CLAUDE.md) (root project rules, especially Rule #4: LLM SDK 不泄漏)
- **When confused about interface design:** Read [`../../SPEC.md`](../../SPEC.md) Section 3.4 (LLM interfaces)
- **When confused about provider support:** Read [`../../PRD.md`](../../PRD.md) Section 6.1 (Tech Stack) 和 Section 5.0 (LLM Configuration)
- **When confused about tool definitions:** Read [`../../SPEC.md`](../../SPEC.md) Section 3.7 (Phase 1 Tool Definitions)

## Module Rules

| # | Rule | Reason |
|---|------|--------|
| 1 | **Layer 1: 只能 import `internal/config`** | 与 k8s 同层，禁止互相 import |
| 2 | **SDK 类型封装** — adapter 文件 (openai.go/anthropic.go/gemini.go) 内部使用 SDK 类型，但公共方法只返回 `provider.go` 中的统一类型 | Root CLAUDE.md Rule #4: agent/ui 永远不接触 SDK 类型 |
| 3 | **每个 adapter 独立** — openai.go, anthropic.go, gemini.go 之间禁止互相引用 | 避免 provider 间耦合 |
| 4 | **ToolDefinition → Native Schema 转换在 adapter 内** — 不要创建公共的 JSON Schema 工具函数 | 三个 provider 的 schema 格式不同，统一转换反而增加复杂度 |
| 5 | **Error 标准化** — 所有 SDK 错误必须映射为 `errors.go` 中的标准类型 | 调用方 (agent) 不需要知道底层是哪个 provider 报的错 |
| 6 | **自定义 endpoint 用原生 provider + BaseURL** — 用户选择 openai/anthropic + 填 BaseURL，不再有 "custom" provider。Registry 对旧配置 `provider: custom` 做向后兼容映射到 openai | 明确 API 协议，避免 OpenAI SDK 打到 Anthropic 代理 |
| 7 | **Adapter 不含重试逻辑** — adapter 只负责错误分类（`mapXxxError` → sentinel error），重试由 `internal/agent/` 决策 | 分层职责：adapter 标准化错误，agent 掌握上下文（迭代次数、用户体验）来决定重试策略。PRD §6.7 承诺"自动重试 1 次"由 agent 层兑现 |

## Interfaces (defined in this module)

```go
type Provider interface {
    Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error)
    ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, ch chan<- StreamDelta) error
    ModelName() string
    ProviderName() string
    ValidateConnection(ctx context.Context) error
}

type Registry interface {
    Create(cfg config.LLMConfig) (Provider, error)
    SupportedProviders() []string
}
```

> Full type definitions (Message, ToolCall, StreamDelta, ParameterDef) → [`README.md`](./README.md) | SPEC Section 3.4 → [`../../SPEC.md`](../../SPEC.md)

## Files

| File | Do | Don't |
|------|-----|-------|
| `provider.go` | 接口 + 统一类型定义 (Message, ToolCall, etc.) | 不要放实现代码 |
| `registry.go` | provider name → constructor 映射 | 不要硬编码, 用 map 注册 |
| `openai.go` | OpenAI SDK 调用 + custom BaseURL 支持 | 不要把 openai 特定类型暴露到公共接口 |
| `anthropic.go` | Anthropic SDK 调用, 注意 system prompt 是独立参数 | 不要假设 system 在 messages 里 |
| `gemini.go` | Gemini SDK 调用, FunctionDeclaration/FunctionCall 转换 | 不要用 legacy SDK (generative-ai-go) |
| `prompt.go` | System prompt 模板, cluster context 注入 | 可以有 provider-aware 片段 |
| `errors.go` | ErrRateLimit, ErrAuth, ErrTimeout, ErrModelUnavailable | 不要用字符串匹配, 用 errors.Is/As |

## Adapter Responsibilities (每个 adapter 必须做)

1. `[]ParameterDef` → provider native tool schema
2. `[]Message` → provider native message format (注意: Anthropic system prompt 分离)
3. Provider response → unified `Message` with `[]ToolCall`
4. Provider error → `ErrRateLimit` / `ErrAuth` / `ErrTimeout` / `ErrModelUnavailable`
5. Streaming: provider stream → `chan<- StreamDelta`

## Cross-Module Dependencies

| This module | → | Dependency | Via |
|-------------|---|-----------|-----|
| llm | imports | config | `config.LLMConfig` |
| agent | imports | llm | `llm.Provider` interface, `llm.Message`, `llm.ToolDefinition` |

## Provider-Specific Notes

| Provider | Gotcha |
|----------|--------|
| OpenAI | tool_choice 参数可控制是否强制 tool use. Response 中 tool_calls 是数组 |
| Anthropic | system prompt 是 `Messages.New()` 的独立参数, 不在 messages 数组里. Content block 类型需区分 text vs tool_use |
| Gemini | 使用 `google.golang.org/genai` (新 SDK)，不要用 `github.com/google/generative-ai-go` (legacy). **FunctionCall 参数是 `map[string]any` 不是 JSON string** — adapter 中需要: (1) 逐字段 `fmt.Sprintf("%v", v)` 转为 `map[string]string` 存入 `ToolCall.Arguments`; (2) 将原始 map JSON 序列化存入 `ToolCall.RawArgs` 保留完整类型信息 |

## ToolCall.Arguments 类型约束

> 详见 [`../../SPEC.md`](../../SPEC.md) Section 3.4 ToolCall 设计说明

- **Phase 1: `map[string]string` 足够** — 7 个 tool 的参数全是 string
- **`RawArgs` 是 escape hatch** — 保留 provider 原始 JSON, 供 Phase 3 扩展时使用
- **Gemini adapter 是唯一需要类型转换的** — OpenAI/Anthropic 返回 JSON string, 解析为 map 即可; Gemini 返回 `map[string]any`, 需要额外处理

## Testing Checklist

- [ ] 每个 adapter: Chat with tool definitions → 验证 tool call 解析
- [ ] 每个 adapter: Chat without tools → 验证纯文本 response
- [ ] 每个 adapter: ChatStream → 验证 StreamDelta 顺序和 Done 标志
- [ ] 每个 adapter: Error mapping (rate limit, auth, timeout)
- [ ] Registry: Create with valid/invalid provider name
- [ ] ValidateConnection: 验证 API 连通性
- [ ] Custom provider: 使用 httptest.NewServer 模拟 OpenAI-compatible endpoint

## Phase 2 Notes

Phase 2 新增 9 个 tool 定义通过 `agent/tools.go` 构建并传入 `Provider.Chat()`。LLM 模块无需接口变更。新 `ToolDefinition` 中的参数类型 (`integer` for `sinceMinutes`, `boolean` for `activeOnly`, `array` of `integer` for `lineIndices`) 通过现有 `ParameterDef.Type` 字段和 `RawArgs` fallback 机制处理。
