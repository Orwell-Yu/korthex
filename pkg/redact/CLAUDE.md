# pkg/redact — CLAUDE.md

> Regex-based log redaction engine. Stateless after construction, concurrent-safe, pure stdlib + regexp.

## Parent Reference

- **When confused about architecture:** Read [`../../CLAUDE.md`](../../CLAUDE.md) (root project rules)
- **When confused about interface design:** Read [`../../SPEC.md`](../../SPEC.md) Section 9.3 (Phase 2 interfaces)
- **When confused about redaction requirements:** Read [Phase 2 PRD](../../docs/superpowers/specs/2026-04-24-phase2-prd-design.md) Section 5.1 (正则脱敏规则)

## Module Rules

| # | Rule | Reason |
|---|------|--------|
| 1 | **Layer 0: 禁止 import 任何 internal/ 或 pkg/ 包** | 纯 stdlib + regexp 实现，零依赖 |
| 2 | **Engine 构造后不可变** — `NewEngine()` 编译所有正则并返回 `*Engine`，之后 `Redact()` 只读使用编译后的正则 | 确保并发安全，多 goroutine 可共享同一 Engine 实例 |
| 3 | **正则启动时一次性编译** — 禁止在 `Redact()` 中编译正则 | 1MB 日志脱敏 < 10ms 的性能目标 |
| 4 | **`[]byte` 入 `[]byte` 出** — `Redact()` 接受 `[]byte` 参数并返回 `[]byte`，热路径禁止 string 转换 | 避免内存分配，日志管道性能关键路径 |
| 5 | **这是 `pkg/` 不是 `internal/`** — 公共 API 稳定性要求高，接口变更需要向后兼容 | 外部消费者可能依赖这个包 |
| 6 | **内置规则在 `builtin.go` 中硬编码** — 用户自定义规则通过 config 在 `NewEngine()` 时注入，运行时不可修改 | 内置规则版本可控，自定义规则隔离于配置层 |

## Interfaces (defined in this module)

```go
type Engine struct { /* unexported fields */ }

func NewEngine(cfg RedactionConfig) *Engine
func (e *Engine) Redact(input []byte) (output []byte, stats RedactStats)
```

> Full type definitions → [`README.md`](./README.md) | SPEC Section 9.3 → [`../../SPEC.md`](../../SPEC.md)

## Files

| File | Do | Don't |
|------|-----|-------|
| `redact.go` | Engine struct, Rule type, RedactStats, NewEngine, Redact | 不要在这里做 IO 操作 |
| `builtin.go` | 6 条内置规则常量 (jwt, api_key, email, ipv4, credit_card, bearer_token) | 不要在运行时修改规则 |
| `redact_test.go` | Table-driven tests + benchmark | 不要依赖外部文件 |

## Who Depends on Me

- `internal/agent` → tool result 发送给 LLM 前通过 `Redact()` 脱敏，脱敏统计附加到消息末尾

## Built-in Rules

| Rule Name | Match Target | Replacement |
|-----------|-------------|-------------|
| `jwt` | JWT Token (`eyJ...`) | `[JWT_REDACTED]` |
| `api_key` | API Key / Token / Secret / Password | `$1=[REDACTED]` |
| `email` | Email address | `[EMAIL_REDACTED]` |
| `ipv4` | IPv4 address | `[IP_REDACTED]` |
| `credit_card` | Credit card number (16 digits) | `[CC_REDACTED]` |
| `bearer_token` | Bearer Token | `Bearer [TOKEN_REDACTED]` |

## Testing Checklist

- [ ] 每条内置规则匹配预期模式 (正向测试)
- [ ] 每条内置规则不误匹配相似但非目标的内容 (反向测试)
- [ ] 自定义规则从 config 加载并生效
- [ ] `disable_builtin` 排除指定内置规则
- [ ] 空输入和无匹配路径
- [ ] `RedactStats.ByRule` 计数准确
- [ ] Benchmark: 1MB 日志文本脱敏 < 10ms
- [ ] 多 goroutine 并发 `Redact()` 无 race condition
