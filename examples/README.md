# Turnmesh Examples

这个目录用于放本地学习和装配样例。它不是稳定 public API 文档；如果示例 import 了 `internal/*`，说明它只适合在本仓库内学习 runtime 结构。

## 学习顺序

1. `subagent`
   先学习 agent/subagent task runtime：如何定义 agent、启动 task、消费事件、读取最终快照。
2. `subagent-kernel`
   后续补一个复用 provider、tool runtime、memory、orchestrator 的完整 kernel subagent 装配示例。
3. `memory`
   后续补 memory snapshot、writeback、compact policy 的最小示例。
4. `mcp-tool`
   后续补 MCP capability provider 映射到统一 tool surface 的示例。
5. `app-facade`
   后续补只依赖根包 `turnmesh` 的业务应用接入示例。

## 当前边界

- 根包 facade 目前公开的是 `New`、`RunTurn`、`StreamTurn`、`RunOneShot`。
- subagent runtime 当前在 `internal/agent` 和 `cmd/engine` 装配层中可用。
- 因为 Go 不能 import `cmd/engine` 这个 `main` package，当前第一个 subagent 示例先聚焦 `internal/agent` 生命周期。
- 等根包补出稳定 agent API 后，示例应迁移到只 import `github.com/Jayleonc/turnmesh`。

