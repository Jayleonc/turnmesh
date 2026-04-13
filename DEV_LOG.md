# DEV_LOG

**最后同步**: 2026-04-14
**状态**: Public facade、one-shot API 与 ai-customer 接入已落地，可直接按应用接入视角继续推进

## 0. 2026-04-14 快照

这次同步后，需要先记住三个事实：

- `turnmesh` 的对外主入口已经是根包 [turnmesh.go](/Users/jayleonc/Developer/ts/turnmesh/turnmesh.go:1)
- 多轮带工具场景走 `turnmesh.New(...).RunTurn(...)`
- 单次无工具场景走 `turnmesh.RunOneShot(...)`

另外：

- `openai-chatcompat` 已经可用，专门面向 OpenAI-compatible `/chat/completions`
- `ai-customer` 的主问答链路已经接到 `RunTurn(...)`
- `ai-customer` 的 query rewrite 也已经接到 `RunOneShot(...)`
- `cmd/engine` 仍保留，但它现在是 bootstrap/test harness，不是外部业务仓库的推荐接入点

## 1. 当前结论

这个工作区已经不是“空架子”，而是一版可运行的 Go kernel baseline。

已经打通的主线：

- 核心协议：`internal/core`
- 执行层：`internal/executor`
- 反馈层：`internal/feedback`
- 编排层：`internal/orchestrator`
- 记忆存储：`internal/memory`
- Turn 级 Memory Runtime：`internal/memory/runtime.go`
- MCP 最小 runtime：`internal/mcp`
- OpenAI adapter：`internal/model/openai`
- Anthropic adapter：`internal/model/anthropic`
- OpenAI-compatible chat adapter：`internal/model/openaichat`
- Provider registry：`internal/model/registry.go`
- Streaming Tool Batch Runtime：`internal/executor/batch_runtime.go`
- Agent Runtime：`internal/agent/runtime_impl.go`
- Runtime assembly：`cmd/engine/bootstrap.go`

当前全量状态：

- `go test ./...` 通过
- `go build ./...` 通过

当前对外接入事实：

- 根包 facade 已提供 `Message`、`Tool`、`TurnRequest`、`TurnResult`
- 根包 facade 已提供 `OneShotRequest`、`OneShotResult`
- 外部仓库已经不需要 import `internal/*`

## 2. 关键事实

### 2.1 核心边界

- 核心包不依赖任何厂商 SDK
- `OpenAI` 和 `Anthropic` 都只是 `internal/model/*` 下的 adapter
- `orchestrator` 只依赖抽象 `model.Session` 和 `executor.Dispatcher`
- `MCP` 现在通过 adapter + executor 进入统一工具面，不拥有主编排权

### 2.2 编排层事实

文件：

- [engine.go](/Users/jayleonc/Developer/ts/turnmesh/internal/orchestrator/engine.go:1)
- [engine_test.go](/Users/jayleonc/Developer/ts/turnmesh/internal/orchestrator/engine_test.go:1)

当前行为：

- 外层 `Engine.StreamTurn()` 负责 turn 闭环
- 支持 `Preparer` / `Finalizer`，因此 turn 前注入与 turn 后回收已经有正式边界
- 会过滤 provider 自己发出的重复 `started`
- 支持多步循环：
  - 模型输出消息
  - 模型发出 `tool_call`
  - 通过 `executor.Dispatcher` 或 `executor.BatchRuntime` 执行工具
  - 生成 `tool_result`
  - 将 assistant/tool continuation message 回灌到下一次 `Session.StreamTurn`
- 有 pass limit，避免无限循环

### 2.3 Provider 事实

统一装配：

- [registry.go](/Users/jayleonc/Developer/ts/turnmesh/internal/model/registry.go:1)
- [bootstrap.go](/Users/jayleonc/Developer/ts/turnmesh/cmd/engine/bootstrap.go:1)
- [bootstrap_test.go](/Users/jayleonc/Developer/ts/turnmesh/cmd/engine/bootstrap_test.go:1)

行为：

- 已有 `Provider Registry`，支持注册、查找、列举和按名称创建 session
- `cmd/engine` 不再手工拼装 `orchestrator.Config`
- runtime assembly 会显式注册默认 provider 与默认工具表，并可选择接入 MCP server

OpenAI：

- [provider.go](/Users/jayleonc/Developer/ts/turnmesh/internal/model/openai/provider.go:1)
- [session.go](/Users/jayleonc/Developer/ts/turnmesh/internal/model/openai/session.go:1)
- [session_test.go](/Users/jayleonc/Developer/ts/turnmesh/internal/model/openai/session_test.go:1)

行为：

- 用标准库 HTTP 调 `Responses API`
- 读 `OPENAI_API_KEY`
- 支持 `previous_response_id`
- 支持把 `core.MessageRoleTool` 映射成 `function_call_output`
- 当前是非流式实现，只解析 `message` 和 `function_call`

Anthropic：

- [provider.go](/Users/jayleonc/Developer/ts/turnmesh/internal/model/anthropic/provider.go:1)
- [session.go](/Users/jayleonc/Developer/ts/turnmesh/internal/model/anthropic/session.go:1)
- [session_test.go](/Users/jayleonc/Developer/ts/turnmesh/internal/model/anthropic/session_test.go:1)

行为：

- 用标准库 HTTP 调 `Messages API`
- 读 `ANTHROPIC_API_KEY`
- 支持 `tool_use -> tool_result -> 再请求`
- 当前是最小闭环，不做 SSE 流式和复杂重试

### 2.4 Memory 事实

文件：

- [store.go](/Users/jayleonc/Developer/ts/turnmesh/internal/memory/store.go:1)
- [inmemory_store.go](/Users/jayleonc/Developer/ts/turnmesh/internal/memory/inmemory_store.go:1)
- [file_store.go](/Users/jayleonc/Developer/ts/turnmesh/internal/memory/file_store.go:1)
- [runtime.go](/Users/jayleonc/Developer/ts/turnmesh/internal/memory/runtime.go:1)
- [store_test.go](/Users/jayleonc/Developer/ts/turnmesh/internal/memory/store_test.go:1)
- [runtime_test.go](/Users/jayleonc/Developer/ts/turnmesh/internal/memory/runtime_test.go:1)

行为：

- 已有 `InMemoryStore` 和 `FileStore`
- `Put` 自动补 `ID`、`CreatedAt`、`UpdatedAt`
- `List` 支持基础过滤
- `FileStore` 是单文件 JSON 快照，单进程内互斥安全
- 已有 turn 级 `Runtime`
- `Runtime.Snapshot()` 可在 turn 前按 session/scope 生成 memory 视图
- `Runtime.Writeback()` / `CommitWrites()` 可在 turn 后生成并持久化记忆写回
- `Runtime.PlanCompact()` / `ApplyCompact()` 已建立“先有 plan，再执行”的 compact 边界
- `cmd/engine` 已把 memory runtime 接到 orchestrator 的 `Preparer` / `Finalizer`

限制：

- 默认 policy 仍为空，当前 compact 主要是 runtime 边界落地，不是最终策略
- 没有 retrieval ranking、向量检索
- 没有跨进程文件锁

### 2.5 MCP 事实

文件：

- [client.go](/Users/jayleonc/Developer/ts/turnmesh/internal/mcp/client.go:1)
- [adapter.go](/Users/jayleonc/Developer/ts/turnmesh/internal/mcp/adapter.go:1)
- [stdio_transport.go](/Users/jayleonc/Developer/ts/turnmesh/internal/mcp/stdio_transport.go:1)
- [types.go](/Users/jayleonc/Developer/ts/turnmesh/internal/mcp/types.go:1)
- [client_test.go](/Users/jayleonc/Developer/ts/turnmesh/internal/mcp/client_test.go:1)

行为：

- 有 stdio 子进程 transport
- 有 JSON-RPC 风格 request/response 关联
- 已有 `BuildToolName` / `ParseToolName`，采用 `mcp__<server>__<tool>` 命名空间
- 已有 `ToolAdapter`，可把 `tools/list` / `tools/call` 映射为 `core.ToolSpec` / `core.ToolResult`
- 已支持：
  - `initialize`
  - `tools/list`
  - `tools/call`
  - `resources/list`
  - `prompts/list`
- 已可通过 runtime assembly 注册到 `executor` 的统一工具表

限制：

- 不是完整 MCP 协议
- 还没接 `resources/read`、`prompts/get`
- 还没把 `resources` / `prompts` 暴露成统一工具面

### 2.6 Executor 事实

文件：

- [types.go](/Users/jayleonc/Developer/ts/turnmesh/internal/executor/types.go:1)
- [handler_tool.go](/Users/jayleonc/Developer/ts/turnmesh/internal/executor/handler_tool.go:1)
- [command_tool.go](/Users/jayleonc/Developer/ts/turnmesh/internal/executor/command_tool.go:1)
- [batch_runtime.go](/Users/jayleonc/Developer/ts/turnmesh/internal/executor/batch_runtime.go:1)
- [tool_dispatcher.go](/Users/jayleonc/Developer/ts/turnmesh/internal/executor/tool_dispatcher.go:1)
- [tool_runtime_test.go](/Users/jayleonc/Developer/ts/turnmesh/internal/executor/tool_runtime_test.go:1)
- [batch_runtime_test.go](/Users/jayleonc/Developer/ts/turnmesh/internal/executor/batch_runtime_test.go:1)

行为：

- 公共执行入口已从 command-centric 调整为 generic tool surface
- `ToolRuntime.Execute()` / `Runtime.Execute()` 现在消费 `ToolRequest`
- `CommandRequest` / `CommandResult` 只保留给本地命令工具兼容路径
- `HandlerTool` 可将任意 generic handler 注册为统一工具
- `ToolDispatcher` 会保留工具语义失败产生的 `core.Error`
- 已有 `BatchRuntime`
- 能基于 `ToolSpec.ConcurrencySafe` 做并发批/串行批分组
- 已有 failure cascade、discard 结果和稳定结果顺序
- `cmd/engine` 已把 batch runtime 接入 orchestrator 主 loop

### 2.7 Agent Runtime 事实

文件：

- [types.go](/Users/jayleonc/Developer/ts/turnmesh/internal/agent/types.go:1)
- [runtime_impl.go](/Users/jayleonc/Developer/ts/turnmesh/internal/agent/runtime_impl.go:1)
- [task.go](/Users/jayleonc/Developer/ts/turnmesh/internal/agent/task.go:1)
- [task_registry.go](/Users/jayleonc/Developer/ts/turnmesh/internal/agent/task_registry.go:1)
- [state_machine.go](/Users/jayleonc/Developer/ts/turnmesh/internal/agent/state_machine.go:1)
- [runtime_impl_test.go](/Users/jayleonc/Developer/ts/turnmesh/internal/agent/runtime_impl_test.go:1)

行为：

- 已有可用的 `agent.Runtime`
- 支持 `Start` / `Stop` / `GetTask` / `ListTasks`
- 任务状态机覆盖 `pending -> running -> completed/failed/stopped`
- `cmd/engine` 已注入 `kernelAgentRunner`
- agent task 会复用现有 provider registry、tool runtime、memory coordinator 和 orchestrator，而不是复制第二套 query engine

## 3. 官方语义参考

OpenAI：

- `Responses API` conversation state：
  - https://developers.openai.com/api/docs/guides/conversation-state
- `Function calling`：
  - https://developers.openai.com/api/docs/guides/function-calling

Anthropic：

- `Messages / tool use`：
  - https://platform.claude.com/docs/en/agents-and-tools/tool-use/define-tools

说明：

- 这次没有引入任何第三方 SDK
- 两家 adapter 都是标准库 HTTP 手写请求

## 4. 新窗口建议入口

新开窗口后，先读这四个文件：

1. [CONSTITUTION.md](/Users/jayleonc/Developer/ts/turnmesh/CONSTITUTION.md:1)
2. [DEV_LOG.md](/Users/jayleonc/Developer/ts/turnmesh/DEV_LOG.md:1)
3. [ROADMAP.md](/Users/jayleonc/Developer/ts/turnmesh/ROADMAP.md:1)
4. [kernel-bootstrap/spec.md](/Users/jayleonc/Developer/ts/turnmesh/docs/kernel-bootstrap/spec.md:1)
5. [runtime-unification/spec.md](/Users/jayleonc/Developer/ts/turnmesh/docs/runtime-unification/spec.md:1)

然后直接从下面顺序继续：

1. 做真正的 `memory policy / compact policy` 策略层，而不是继续补 runtime 边界
2. 把 `resources` / `prompts` 继续桥接进统一工具面
3. 做更完整的 streaming tool event surface / fallback 语义
4. 做 agent-specific MCP / workspace isolation / background orchestration
5. 最后做 `TS bridge`

## 5. 下一步最值得做的具体任务

如果只做一件事，优先做这个：

- 补真正的 `memory policy / compact policy`

原因：

- 统一装配入口、provider registry、MCP tool surface、batch runtime、agent runtime 都已经落地
- 当前最缺的是“策略”，不是“再造 runtime 骨架”
- 这会决定后续 subagent、bridge 和长会话是否需要返工

## 6. 恢复工作时不要忘的约束

- 不要把厂商字段泄漏进 `internal/core`
- 不要让 `MCP` 拥有主编排权
- 不要为 `subagent` 再复制一套 orchestrator
- 改动后保持：
  - `go test ./...`
  - `go build ./...`
