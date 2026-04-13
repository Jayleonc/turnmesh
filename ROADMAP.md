# Turnmesh Roadmap

**日期**: 2026-04-14
**状态**: Active v0.3

## 0. 当前同步状态

截至 2026-04-14，`turnmesh` 已经不是“go-core-rewrite 草案”，而是一版可嵌入的 runtime baseline。

已完成：

- 根包公开 facade：`turnmesh.go`
- `RunTurn(...)` / `StreamTurn(...)`
- `RunOneShot(...)`
- `openai-chatcompat` provider
- `go.mod`、`cmd/engine/main.go` 和 `internal/*` 基础包结构
- 核心协议：`internal/core`
- 执行层最小闭环：`internal/executor`
- 反馈层最小闭环：`internal/feedback`
- 编排层 turn loop：`internal/orchestrator`
- 两个模型 adapter：`internal/model/openai`、`internal/model/anthropic`
- 第三个模型 adapter：`internal/model/openaichat`
- 最小 MCP runtime：`internal/mcp`
- 最小记忆存储：`internal/memory`
- `ai-customer` 接入文档与实际接入落地
- 全量验证：`go test ./...`、`go build ./...` 已通过

当前还没做完：

- 更正式的 semver 发布流程与 examples
- OpenAI / Anthropic 的流式 SSE 支持
- 将 MCP 正式桥接到 `executor` 的 tool surface
- compact / memory policy 的真实策略实现
- subagent runtime
- TS bridge / CLI 渐进接管

当前对外推荐接法已经明确：

- 多轮带工具：根包 `turnmesh.New(...).RunTurn(...)`
- 单次不带工具：根包 `turnmesh.RunOneShot(...)`

`cmd/engine` 保留为 bootstrap / smoke harness，不是外部业务仓库的推荐接入面。

## 1. 目标

我们要在 `ts/` 下启动一个新的 Go 重构工作区，用 Go 重做 Claude Code 类系统中的核心内核，而不是直接复刻整个现有 TypeScript CLI。

第一阶段聚焦四层：

- 记忆层
- 编排层
- 执行层
- 反馈层

第二阶段扩展：

- MCP
- Agent / Subagent
- TS Bridge / TUI 对接

## 2. 北极星

最终目标不是“把 Anthropic 的 SDK 或现有 TS 文件翻译成 Go”，而是构建一个 **模型无关、运行时可扩展、可接入多个模型提供商** 的 Go agent kernel。

这个 kernel 需要满足：

- Anthropic 只是一个 adapter，不是核心协议
- OpenAI、Gemini、OpenRouter、本地模型都可以通过 adapter 接入
- 工具执行、权限、事件、记忆、MCP、subagent 不依赖某一个模型厂商
- TS 现有 CLI 可以在早期继续作为前端/胶水层存在

## 3. 非目标

当前阶段不追求：

- 100% 复刻现有 Ink/React 终端 UI
- 100% 对齐所有 slash commands
- 还原上游缺失的内部 feature-gated 模块
- 直接照搬 Anthropic SDK 类型、命名和耦合方式

## 4. 参考源码范围

以下 TypeScript 模块是本项目的主要参考来源：

### 核心循环与状态

- `claude-code-source-code/src/QueryEngine.ts`
- `claude-code-source-code/src/query.ts`
- `claude-code-source-code/src/Tool.ts`
- `claude-code-source-code/src/services/tools/toolOrchestration.ts`
- `claude-code-source-code/src/services/tools/StreamingToolExecutor.ts`

### 记忆层

- `claude-code-source-code/src/memdir/memdir.ts`
- `claude-code-source-code/src/memdir/findRelevantMemories.ts`
- `claude-code-source-code/src/services/SessionMemory/sessionMemory.ts`
- `claude-code-source-code/src/services/extractMemories/extractMemories.ts`
- `claude-code-source-code/src/services/compact/compact.ts`
- `claude-code-source-code/src/services/compact/sessionMemoryCompact.ts`

### 执行层

- `claude-code-source-code/src/utils/Shell.ts`
- `claude-code-source-code/src/tools/BashTool/BashTool.tsx`
- `claude-code-source-code/src/tools/BashTool/bashSecurity.ts`
- `claude-code-source-code/src/tools/BashTool/bashPermissions.ts`

### MCP

- `claude-code-source-code/src/services/mcp/client.ts`
- `claude-code-source-code/src/services/mcp/types.ts`
- `claude-code-source-code/src/services/mcp/config.ts`

### Agent / Subagent

- `claude-code-source-code/src/tools/AgentTool/AgentTool.tsx`
- `claude-code-source-code/src/tools/AgentTool/runAgent.ts`
- `claude-code-source-code/src/tools/AgentTool/forkSubagent.ts`
- `claude-code-source-code/src/tasks/LocalAgentTask/LocalAgentTask.tsx`
- `claude-code-source-code/src/tasks/RemoteAgentTask/RemoteAgentTask.tsx`
- `claude-code-source-code/src/utils/swarm/**`

### 预研参考

- `claude-code-source-code/docs/go-port-feasibility/research.md`

## 5. 架构原则

### 5.1 模型无关优先

核心层只定义自己的协议，不直接暴露任何 Anthropic SDK 类型。

建议的核心抽象：

- `ModelProvider`
- `ModelSession`
- `TurnRequest`
- `TurnEvent`
- `ToolCall`
- `ToolResult`
- `ApprovalRequest`
- `MemoryRecord`
- `AgentTask`

### 5.2 事件优先

整个 kernel 以事件流驱动，而不是 UI 驱动。

核心事件至少包括：

- 用户输入
- 模型增量输出
- tool_use 请求
- tool_result 回填
- 权限审批
- 记忆写入
- compact / summarize
- task start / task end
- agent spawn / agent join / agent fail

### 5.3 显式状态机

编排层必须是显式状态机，不允许把关键状态散落在 UI 或工具实现里。

至少需要有：

- Turn lifecycle
- Tool loop lifecycle
- Approval lifecycle
- Background task lifecycle
- Agent lifecycle

### 5.4 适配器隔离

厂商特有逻辑必须收敛在 adapter 层。

例如：

- Anthropic thinking / tool_use 细节
- OpenAI response/tool schema 细节
- Gemini function calling 细节

这些都不能进入 `memory/orchestrator/executor/feedback` 的核心包。

## 6. 建议目录

初始建议目录如下，后续逐步落地：

```text
turnmesh/
├── AGENTS.md
├── ROADMAP.md
├── cmd/
│   └── engine/
├── internal/
│   ├── core/
│   ├── model/
│   ├── memory/
│   ├── orchestrator/
│   ├── executor/
│   ├── feedback/
│   ├── mcp/
│   ├── agent/
│   └── bridge/
└── pkg/
```

说明：

- `internal/core` 放核心协议和事件类型
- `internal/model` 放模型 provider 抽象和各家 adapter
- `internal/memory` 放工作记忆、持久记忆、compact、retrieval
- `internal/orchestrator` 放 turn loop 和状态机
- `internal/executor` 放 tool runtime、shell、sandbox、timeouts
- `internal/feedback` 放日志、摘要、审计、UI 事件
- `internal/mcp` 放 MCP client/runtime
- `internal/agent` 放 subagent/task runtime
- `internal/bridge` 放 TS bridge / RPC / socket / stdio 协议

## 7. 分阶段路线

### Phase 0: 定义核心契约

目标：

- 固定模型无关的核心协议
- 固定事件总线和状态机边界
- 固定 adapter 边界

产物：

- 核心类型草案
- 状态机草案
- 错误模型草案

通过条件：

- 核心包不 import 任何厂商 SDK
- 核心协议可以表达至少两家模型的 tool calling 场景

### Phase 1: 执行层落地

目标：

- 先把 Shell / File / Search 这种最基础的工具运行时做出来

产物：

- command runner
- stdout/stderr streaming
- timeout / cancel / cwd / env / sandbox 抽象
- tool result 归一化模型

参考：

- `src/utils/Shell.ts`
- `src/tools/BashTool/BashTool.tsx`

通过条件：

- tool runtime 不依赖任何特定模型
- 可以被 orchestrator 直接复用

### Phase 2: 编排层落地

目标：

- 跑通单轮 agent loop
- 支持 `tool_use -> execute -> tool_result -> continue`

产物：

- turn state machine
- tool orchestration engine
- retry / approval / interrupt 基础框架

参考：

- `src/QueryEngine.ts`
- `src/query.ts`
- `src/services/tools/toolOrchestration.ts`

通过条件：

- 不依赖 TUI
- 可以用 mock model provider 测试

### Phase 3: 记忆层落地

目标：

- 把“会话日志 + 工作记忆 + 持久记忆 + 检索 + compact”做成独立系统

产物：

- session event log
- working memory store
- persistent memory store
- retrieval policy
- compact policy

参考：

- `src/memdir/**`
- `src/services/SessionMemory/**`
- `src/services/extractMemories/**`
- `src/services/compact/**`

通过条件：

- 记忆层不直接依赖具体 UI
- compact 可以作为 orchestrator 的策略模块接入

### Phase 4: 反馈层落地

目标：

- 将内部事件转为外部可消费反馈，而不是把展示逻辑塞进执行层

产物：

- progress event schema
- audit log schema
- summary generator interface
- approval / notification / telemetry hooks

参考：

- `src/services/tools/StreamingToolExecutor.ts`
- `src/services/toolUseSummary/toolUseSummaryGenerator.ts`

通过条件：

- 反馈层可以同时服务 TUI、日志、API 和远端桥接

### Phase 5: 模型 adapter 落地

目标：

- 先接 Anthropic adapter
- 再补至少一个第二模型 adapter 作为“去锁定验证”

建议：

- Anthropic adapter first
- OpenAI adapter skeleton second

通过条件：

- 核心层无需修改即可接第二家模型
- tool call、流式输出、结构化结果都能落入统一事件模型

### Phase 6: MCP 落地

目标：

- 将 MCP 作为独立 runtime 接入，而不是嵌入在某个模型 adapter 中

产物：

- MCP client manager
- transport abstraction
- tool/resource registry
- auth/session 生命周期

参考：

- `src/services/mcp/client.ts`

通过条件：

- MCP 工具对 orchestrator 看起来只是“另一类 tool provider”

### Phase 7: Agent / Subagent 落地

目标：

- 做本地 subagent runtime
- 再决定是否扩展 remote agent

产物：

- agent definition
- agent task runtime
- spawn/join/cancel/background protocol
- shared memory / scoped memory 策略

参考：

- `src/tools/AgentTool/**`
- `src/tasks/LocalAgentTask/**`
- `src/tasks/RemoteAgentTask/**`
- `src/utils/swarm/**`

通过条件：

- subagent 使用与主线程同一套 orchestrator + executor 内核
- agent 生命周期可观测、可取消、可恢复

### Phase 8: TS Bridge 与渐进切换

目标：

- 让现有 TS CLI 在一段时间内充当前端和兼容层

产物：

- stdio / socket bridge
- Go engine event stream
- TS adapter glue code

通过条件：

- UI 可以逐步迁移
- Go engine 可以脱离 TS 独立运行

## 8. 顺序约束

以下顺序是强约束，不建议跳过：

1. 先定义核心协议
2. 再做执行层
3. 再做编排层
4. 再做记忆层
5. 再做反馈层
6. 再接模型 adapter
7. 再做 MCP
8. 最后做 subagent

原因：

- 没有统一事件和任务模型，MCP/subagent 很容易做成特殊分支
- 没有稳定 executor，subagent 只会复制不稳定逻辑
- 没有模型无关协议，后续多模型支持会变成二次重写

## 9. 风险

- 风险 1：过早照搬 Anthropic SDK，导致核心协议厂商锁定
- 风险 2：过早做 UI，导致状态逻辑再次分散
- 风险 3：MCP 太早接入，导致 orchestrator 边界还没稳定就固化
- 风险 4：subagent 太早上，导致 task model 和 cancellation model 不可控

## 10. 当前结论

可以做，而且 bootstrap 基线已经完成。

当前结论仍然成立：正确方式不是“翻译 TS”，而是：

- 以 TS 源码为行为参考
- 以 Go 重新定义内核
- 以 adapter 方式兼容各家模型
- 以分阶段方式逐步接入 MCP 和 subagent

下一步默认动作：

- 补 `provider registry` 和装配入口
- 把 `MCP client` 接进 `executor`，让 provider 看到的是统一工具表
- 推进 `memory policy / compact policy`
- 起 `agent/subagent runtime`
- 最后再做 `TS bridge / CLI` 渐进接管
