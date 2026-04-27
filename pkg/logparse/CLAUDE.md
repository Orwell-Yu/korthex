# pkg/logparse — CLAUDE.md

> Log parsing utilities: timestamp, severity, JSON detection. Stateless, concurrent-safe, zero dependencies.

## Parent Reference

- **When confused about architecture:** Read [`../../CLAUDE.md`](../../CLAUDE.md) (root project rules)
- **When confused about interface design:** Read [`../../SPEC.md`](../../SPEC.md) Section 3.2 (LogParse interfaces)
- **When confused about log display format:** Read [`../../PRD.md`](../../PRD.md) Section 5.2 (Log Viewer 功能列表和展示格式)

## Module Rules

| # | Rule | Reason |
|---|------|--------|
| 1 | **Layer 0: 禁止 import 任何 internal/ 或 pkg/ 包** | 纯 stdlib 实现，零依赖 |
| 2 | **Parser 必须无状态** — 每次 Parse() 调用独立，不持有任何 state | 确保并发安全，多 goroutine 可共享同一 Parser 实例 |
| 3 | **永远不修改 Raw 字段** — LogEntry.Raw 必须保持原始日志行不变 | 日志导出、搜索、显示都依赖原始内容 |
| 4 | **Severity 检测正则必须编译后缓存** — 使用 `sync.Once` 或 package-level `var` | Parse 在热路径上，每行日志都会调用 |
| 5 | **这是 `pkg/` 不是 `internal/`** — 公共 API 稳定性要求高，接口变更需要向后兼容 | 外部消费者可能依赖这个包 |

## Interfaces (defined in this module)

```go
type Parser interface {
    Parse(podName, container, rawLine string) LogEntry
}
```

> Full type definitions → [`README.md`](./README.md) | SPEC Section 3.2 → [`../../SPEC.md`](../../SPEC.md)

## Files

| File | Do | Don't |
|------|-----|-------|
| `parser.go` | LogEntry struct, Parser 实现, Size() | 不要在这里做 IO 操作 |
| `severity.go` | 编译正则表, FATAL > ERROR > WARN > INFO > DEBUG | 不要硬编码字符串比较, 用正则 |
| `json.go` | `json.Valid()` 检测, `json.Indent()` 格式化 | 不要 unmarshal 到 struct, 只做格式化 |
| `timestamp.go` | 多格式尝试解析, 返回 zero time 表示失败 | 不要 panic, 不要 log.Fatal |

## Who Depends on Me

- `internal/agent` → 在 tool 执行后解析日志行
- `internal/ui` → LogViewerModel 使用 LogEntry 渲染带颜色的日志

## Supported Timestamp Formats

按尝试顺序:
1. RFC 3339 / ISO 8601: `2024-01-01T14:23:05.123456789Z`
2. RFC 3339 with offset: `2024-01-01T14:23:05+08:00`
3. Java style: `2024-01-01 14:23:05.123`
4. Go default: `2024/01/01 14:23:05`
5. Kubelet --timestamps: `2024-01-01T14:23:05.123456789Z <rest of line>`

## Testing Checklist

- [ ] 每种 severity 关键词的检测 (ERROR, WARN, INFO, DEBUG, FATAL, PANIC, CRITICAL)
- [ ] 每种 timestamp 格式的解析
- [ ] JSON 日志检测和格式化
- [ ] 非 JSON/无时间戳/无 severity 的 plain text
- [ ] 超长行 (>10KB) 的 Size() 计算
- [ ] Benchmark: DetectSeverity 和 ParseTimestamp
