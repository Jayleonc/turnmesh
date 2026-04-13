# AGENTS.md

本文件约束 `turnmesh/` 目录下的后续协作、设计和实现。

## 0. 进入工作区后先读什么

如果你是新会话、新窗口或新 agent，开始任何分析和编码前，按这个顺序读：

1. `./CONSTITUTION.md`
2. `./DEV_LOG.md`
3. `./ROADMAP.md`
4. `./docs/kernel-bootstrap/spec.md`
5. 你将要修改的包及其测试

最低要求：

- 没读完 `CONSTITUTION.md` 和 `DEV_LOG.md`，不要直接开始改代码
- 如果你要改 `internal/orchestrator`、`internal/model`、`internal/mcp`、`internal/memory`，先读对应测试
- 如果你的改动会改变事实状态，结束前同步 `DEV_LOG.md`

## 0.1 这些文件分别负责什么

- `CONSTITUTION.md`：仓库级最高稳定原则，回答“什么绝对不能做”
- `AGENTS.md`：操作手册，回答“进入仓库后先读什么、怎么工作、按什么顺序推进”
- `DEV_LOG.md`：当前事实快照，回答“现在已经做到哪里了、下一步接什么”
- `ROADMAP.md`：阶段路线，回答“中期演进顺序是什么”
- `docs/kernel-bootstrap/spec.md`：约束与验收，回答“哪些条件必须满足”

## 0.2 本文件与 CONSTITUTION 的关系

- `CONSTITUTION.md` 高于本文件
- 如果 `AGENTS.md` 与 `CONSTITUTION.md` 冲突，以 `CONSTITUTION.md` 为准
- 如果用户在当前会话里给出更高优先级指令，以会话指令为准

## 1. 这个工作区是做什么的

这个工作区用于构建一个新的 Go agent kernel。

目标不是复刻 Anthropic CLI 的全部外观，而是重建它的核心能力：

- 记忆层
- 编排层
- 执行层
- 反馈层
- 后续的 MCP
- 后续的 Agent / Subagent

## 2. 核心原则

### 2.1 不照搬 Anthropic SDK

这是硬约束。

不能把 Anthropic SDK 的请求/响应类型直接作为核心域模型。
不能让核心包 import Anthropic SDK。
不能把 Anthropic 特有语义泄漏到公共接口。

允许做法：

- 在 `internal/model/anthropic` 中编写 adapter
- 将 Anthropic 的 tool use / stream event / thinking event 映射到我们自己的核心协议

### 2.2 核心必须支持多模型

这是硬目标。

系统设计必须允许后续接入：

- Anthropic
- OpenAI
- Gemini
- OpenRouter
- 本地模型

判断标准：

- 新增第二家模型时，不需要重写 memory/orchestrator/executor/feedback

### 2.3 先内核，后前端

先做 Go kernel，再考虑 TUI 或 TS 集成。

不允许：

- 先围绕 UI 设计核心状态
- 先写一堆展示逻辑再补状态机

### 2.4 事件流优先

系统必须围绕统一事件流设计。

至少包括：

- 输入事件
- 模型输出事件
- 工具调用事件
- 工具结果事件
- 审批事件
- 记忆事件
- 背景任务事件
- agent 事件

### 2.5 明确边界，禁止跨层偷写

约束如下：

- `memory` 不直接调用 UI
- `executor` 不直接操作 session 展示
- `feedback` 不承载业务状态机
- `mcp` 不拥有主编排权
- `agent` 不复制一套新的 orchestrator

## 3. 实施顺序

顺序是强约束：

1. 定义核心协议
2. 落地执行层
3. 落地编排层
4. 落地记忆层
5. 落地反馈层
6. 接入模型 adapter
7. 接入 MCP
8. 接入 Agent / Subagent
9. 对接 TS Bridge / CLI

如果顺序要调整，必须先说明原因。

## 4. 我们要参考什么

以下文件是主要参考对象，不是照抄对象。

### 核心循环

- `../claude-code-source-code/src/QueryEngine.ts`
- `../claude-code-source-code/src/query.ts`
- `../claude-code-source-code/src/Tool.ts`
- `../claude-code-source-code/src/services/tools/toolOrchestration.ts`
- `../claude-code-source-code/src/services/tools/StreamingToolExecutor.ts`

### 记忆

- `../claude-code-source-code/src/memdir/memdir.ts`
- `../claude-code-source-code/src/memdir/findRelevantMemories.ts`
- `../claude-code-source-code/src/services/SessionMemory/sessionMemory.ts`
- `../claude-code-source-code/src/services/extractMemories/extractMemories.ts`
- `../claude-code-source-code/src/services/compact/compact.ts`

### 执行

- `../claude-code-source-code/src/utils/Shell.ts`
- `../claude-code-source-code/src/tools/BashTool/BashTool.tsx`
- `../claude-code-source-code/src/tools/BashTool/bashSecurity.ts`
- `../claude-code-source-code/src/tools/BashTool/bashPermissions.ts`

### MCP

- `../claude-code-source-code/src/services/mcp/client.ts`
- `../claude-code-source-code/src/services/mcp/types.ts`
- `../claude-code-source-code/src/services/mcp/config.ts`

### Agent / Subagent

- `../claude-code-source-code/src/tools/AgentTool/AgentTool.tsx`
- `../claude-code-source-code/src/tools/AgentTool/runAgent.ts`
- `../claude-code-source-code/src/tools/AgentTool/forkSubagent.ts`
- `../claude-code-source-code/src/tasks/LocalAgentTask/LocalAgentTask.tsx`
- `../claude-code-source-code/src/tasks/RemoteAgentTask/RemoteAgentTask.tsx`
- `../claude-code-source-code/src/utils/swarm/**`

### 预研结论

- `../claude-code-source-code/docs/go-port-feasibility/research.md`
- `./ROADMAP.md`

## 5. 参考时要看什么

参考源码时，不是看“它怎么写 TypeScript 语法”，而是看：

- 它有哪些状态机
- 它把哪些问题收敛成了统一协议
- 它如何处理 tool loop
- 它如何处理中断、重试、压缩、记忆
- 它如何处理 task / subagent 生命周期
- 它如何把 MCP 接到工具系统里

## 6. 明确禁止事项

以下做法禁止：

- 把 Anthropic SDK 类型放进 `internal/core`
- 在核心包中出现厂商特有字段命名
- 为 Anthropic 单独设计一套主流程
- 在 `agent` 包里复制一份新的执行层
- 在 `mcp` 包里直接做主编排决策
- 把 UI 层状态当作内核状态来源

## 7. 建议的最小公共协议

后续写 Go 代码时，优先先把这些概念固定：

- `Provider`
- `Session`
- `TurnInput`
- `TurnEvent`
- `ToolSpec`
- `ToolInvocation`
- `ToolResult`
- `ApprovalDecision`
- `MemoryEntry`
- `TaskState`
- `AgentDefinition`

只要这些协议固定，后面：

- 模型 adapter
- MCP
- subagent
- TS bridge

都能围绕同一套内核扩展。

## 8. MCP 和 Subagent 的策略

### 8.1 MCP

MCP 在本项目中应被视为：

- 一个外部 capability runtime
- 一个可注册的 tool/resource provider

而不是：

- 一个新的主控内核

MCP 必须等核心 orchestrator 稳定后再接入。

### 8.2 Subagent

Subagent 不是“再起一个特殊系统”，而是：

- 在同一 kernel 上运行的另一类 task
- 具有自己的上下文、预算、审批、记忆作用域

Subagent 必须复用：

- 同一个 executor
- 同一个 orchestrator
- 同一套事件模型
- 同一套 cancellation / join / background 语义

## 9. 后续写代码时的默认要求

- 优先写接口和域模型，再写 adapter
- 优先写状态机，再写展示逻辑
- 优先做 mockable 的单元测试，再做集成测试
- 每增加一个厂商特有能力，都要先判断是否应进入 adapter
- 每增加一个新事件，都要判断是否应该进入统一事件模型
- 只要修改了系统事实、下一步建议或当前边界，就同步 `DEV_LOG.md`
- 只要修改了仓库级硬原则，就同步 `CONSTITUTION.md`

## 9.1 新窗口启动检查清单

开始编码前，至少完成以下动作：

1. 读完 `CONSTITUTION.md`
2. 读完 `DEV_LOG.md`
3. 确认当前默认下一步是否仍然成立
4. 确认你要修改的包是否已有测试
5. 明确这次改动是否会影响：
   `internal/core`
   `internal/orchestrator`
   `internal/executor`
   `internal/memory`
   `internal/mcp`
   `internal/model/*`

如果会影响以上核心边界，先说明影响面，再开始改动。

## 10. 当前默认下一步

后续默认按以下顺序推进：

1. 先做 `provider registry` 和统一装配入口
2. 把 `internal/mcp` 通过 `executor` 暴露成统一工具表
3. 补 `memory policy / compact policy`
4. 起 `agent/subagent runtime`
5. 再做 `TS bridge / CLI`

当前事实：

- `OpenAI` 和 `Anthropic` adapter 已落地
- `orchestrator` 已支持 `tool_call -> execute -> tool_result -> continue`
- `memory` 已有 `InMemoryStore` 和 `FileStore`
- `MCP stdio runtime` 已有最小可用实现
- 当前基线要求始终保持 `go test ./...` 和 `go build ./...` 为绿色
