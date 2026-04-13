# Go Kernel Bootstrap 需求规格说明书

**版本**: 1.1
**日期**: 2026-04-13
**状态**: Implemented Baseline

---

## 文件边界

- **本文件（spec.md）**：定义必须满足的约束、行为与验收标准
- **plan.md**：目录结构、实现步骤
- **附录 A**：设计草案（非强制，供参考）

---

## 0. 代码事实（Code Facts）

> 以下信息来自代码库探查，作为本 Spec 的事实基础。

### 0.1 已检查的关键代码

| 文件 | 关键发现 |
|------|----------|
| `turnmesh/ROADMAP.md` | 当前工作区已有“模型无关、事件优先、先核心后 MCP/subagent”的目标约束，并已同步到一版可运行 bootstrap 基线。 |
| `turnmesh/AGENTS.md` | 当前工作区已把“不得照搬 Anthropic SDK”“MCP 和 subagent 复用同一 kernel”定义为硬约束。 |
| `claude-code-source-code/src/QueryEngine.ts` | 现有核心是长生命周期会话引擎，状态包括消息、usage、permission denial、read file cache，并通过 `submitMessage()` 驱动单轮提交与持续会话。 |
| `claude-code-source-code/src/query.ts` | 主代理循环是显式的跨迭代状态机，维护 `messages`、`toolUseContext`、compact/retry/budget 等状态，并在循环中执行 tool use 与恢复逻辑。 |
| `claude-code-source-code/src/Tool.ts` | `ToolUseContext` 同时承载工具池、MCP 客户端、agent 定义、权限、UI 回调、通知、compact 回调等，说明当前上下文边界偏宽。 |
| `claude-code-source-code/src/utils/Shell.ts` | shell 执行层负责 shell 发现、cwd 恢复、超时、sandbox、stdout 流式输出、取消和 provider 选择。 |
| `claude-code-source-code/src/tools/BashTool/BashTool.tsx` | Bash 工具并非简单命令执行器，还处理安全限制、只读判定、后台运行、输出归一化和 UI 反馈。 |
| `claude-code-source-code/src/memdir/memdir.ts` | 记忆目录是显式文件系统结构，系统会确保目录存在，并向模型注入明确的 typed memory 行为说明。 |
| `claude-code-source-code/src/services/SessionMemory/sessionMemory.ts` | Session memory 通过后台 forked agent 周期性提取，触发条件依赖 token 增长、tool call 数和 turn 边界。 |
| `claude-code-source-code/src/services/extractMemories/extractMemories.ts` | Durable memory 抽取在 query loop 完结后运行，并使用受限工具权限、forked agent 和 memory path 约束。 |
| `claude-code-source-code/src/services/compact/compact.ts` | compact 是一个独立能力，负责消息裁剪、附件剥离、boundary 注入和 post-compact message 重建。 |
| `claude-code-source-code/src/services/mcp/client.ts` | MCP 客户端不仅做 transport，还承载 auth、session expiry、resource/tool 桥接、输出裁剪和错误恢复。 |
| `claude-code-source-code/src/tools/AgentTool/runAgent.ts` | subagent 不是特殊模式，而是复用主循环、共享或派生上下文、带 agent-specific MCP 和清理逻辑的运行时。 |
| `claude-code-source-code/src/tasks/LocalAgentTask/LocalAgentTask.tsx` | agent 生命周期包含注册、后台化、进度更新、kill、通知和 transcript/output 生命周期。 |

### 0.2 现有实现模式

- **依赖注入**：现有 TS 核心以构造参数和上下文对象注入为主；`QueryEngineConfig` 和 `ToolUseContext` 汇聚大量运行时依赖，而不是通过 setter 注入。
- **错误处理**：现有 TS 核心大量使用显式 typed error、abort signal 和恢复分支；失败路径是主循环的一等公民。
- **事务管理**：当前任务范围内未观察到数据库事务模式；状态主要由内存状态机、文件系统持久化和会话 side effects 组成。

### 0.3 潜在冲突点

- 当前 `turnmesh/` 已具备 `go.mod`、`cmd/engine` 和 `internal/*` 核心包；剩余缺口已从“能否启动”转为“如何继续演进而不返工”。
- 现有 TS `ToolUseContext` 同时耦合 UI、MCP、agent、permissions 和 tool execution；Go bootstrap 若直接照搬会把边界污染复制过去。
- 现有 session memory 和 durable memory 都依赖 forked agent；Go bootstrap 初期若把这类后台 agent 作为前置条件，会抬高实现复杂度。
- 现有 MCP 与 agent/subagent 运行时非常重；当前 Go 侧只实现了最小 stdio/runtime 边界，仍不能把它误当成完整协议实现。
- 当前工作区要求模型无关；任何直接复用 Anthropic SDK 类型的实现都与既有约束冲突。

### 0.4 当前实现快照

- `internal/core`：核心域模型、状态枚举、错误模型
- `internal/model`：抽象 `Provider` / `Session`
- `internal/model/openai`：基于 OpenAI `Responses API` 的最小 adapter，支持 `previous_response_id` 和 `function_call_output`
- `internal/model/anthropic`：基于 Anthropic `Messages API` 的最小 adapter，支持 `tool_use -> tool_result -> continue`
- `internal/executor`：本地命令执行、tool registry、dispatcher
- `internal/orchestrator`：显式多步 turn loop，支持 `tool_call -> execute -> tool_result -> continue`
- `internal/memory`：`InMemoryStore`、`FileStore`
- `internal/mcp`：stdio transport、JSON-RPC 风格 client、`tools/list` / `tools/call`
- `internal/feedback`：结构化 event sink
- 验证结果：`go test ./...`、`go build ./...` 均已通过

---

## 1. 背景与问题

### 1.1 症状 A: 当前只有路线，没有可编译内核
**位置**：`turnmesh/ROADMAP.md`
**现象**：工作区只存在目标和约束，没有 Go 模块、核心协议和最小可运行骨架。
**危害**：后续实现会在没有统一域模型的情况下分散展开，导致 MCP、subagent 和多模型支持过早耦合。

### 1.2 症状 B: 现有 TS 主循环边界过宽
**位置**：`claude-code-source-code/src/Tool.ts`，`claude-code-source-code/src/QueryEngine.ts`
**现象**：工具上下文与查询引擎同时承载工具池、权限、UI、agent、MCP 和状态更新回调。
**危害**：如果 Go 版本复刻这种上下文结构，核心层将无法保持模型无关和前端无关。

### 1.3 症状 C: 执行层语义比表面复杂
**位置**：`claude-code-source-code/src/utils/Shell.ts`，`claude-code-source-code/src/tools/BashTool/BashTool.tsx`
**现象**：命令执行包含 shell 发现、取消、cwd 恢复、sandbox、后台任务、安全限制与输出归一化。
**危害**：如果 bootstrap 只实现“跑命令”，后续 orchestrator 和 subagent 无法建立稳定执行语义。

### 1.4 症状 D: 记忆与 compact 是内核策略，不只是附属功能
**位置**：`claude-code-source-code/src/services/SessionMemory/sessionMemory.ts`，`claude-code-source-code/src/services/extractMemories/extractMemories.ts`，`claude-code-source-code/src/services/compact/compact.ts`
**现象**：记忆抽取与 compact 直接影响 query loop 边界、工具触发和上下文维持。
**危害**：如果 bootstrap 完全忽略这些边界，后续将被迫在主循环外补丁式塞入记忆和 compact。

### 1.5 症状 E: MCP 与 subagent 需要统一 runtime，而非额外内核
**位置**：`claude-code-source-code/src/services/mcp/client.ts`，`claude-code-source-code/src/tools/AgentTool/runAgent.ts`
**现象**：MCP 和 subagent 都依附于主循环、工具系统、权限系统和任务生命周期。
**危害**：如果 bootstrap 为它们各自引入特例，Go kernel 会在早期形成多套编排路径。

---

## 2. 术语与规范用语

### 2.1 规范用语
| 关键词 | 语义 |
|--------|------|
| **MUST** | 必须满足，否则不算完成 |
| **MUST NOT** | 明确禁止 |
| **SHOULD** | 建议满足，可在 Plan 里说明例外 |

### 2.2 术语定义
| 术语 | 定义 |
|------|------|
| **Kernel** | Go 侧的核心运行时，负责模型编排、工具执行、事件流和记忆策略承载。 |
| **Provider** | 某一模型厂商或本地模型的适配器实现。 |
| **Turn** | 一次从用户输入开始，到模型输出稳定并完成必要 tool loop 的处理周期。 |
| **Tool Runtime** | 负责执行工具调用、取消、权限判定、输出归一化的执行子系统。 |
| **Memory Policy** | 决定何时读写工作记忆、持久记忆和 compact 的规则集合。 |
| **Feedback Sink** | 消费内核事件并转成日志、UI、通知或调试输出的下游接口。 |
| **Bootstrap Scope** | 本次施工必须完成的最小闭环范围。 |

---

## 3. 需求条款

### R1. 核心协议边界

| 编号 | 条款 |
|------|------|
| R1.1 | **MUST** 在 `turnmesh` 内定义独立于任何模型厂商 SDK 的核心域模型，包括 turn、event、tool call、tool result、approval、memory 和 task/agent 的基础概念。[需新增接口] |
| R1.2 | **MUST NOT** 在 `internal/core`、`internal/orchestrator`、`internal/executor`、`internal/memory`、`internal/feedback` 中直接 import 或暴露 Anthropic SDK 类型。[需新增接口] |
| R1.3 | **MUST** 使同一套核心协议可表达“模型输出文本”和“模型请求工具调用”两类事件。[需新增接口] |
| R1.4 | **SHOULD** 使核心事件模型可被未来的 MCP 和 subagent 直接复用，而无需再引入第二套事件协议。[需新增接口] |

### R2. 编排层闭环

| 编号 | 条款 |
|------|------|
| R2.1 | **MUST** 提供一个显式 turn runtime，使单轮处理可以从输入进入模型调用、接收增量事件、消费 tool call、执行工具并继续推进到稳定结果。[需新增接口] |
| R2.2 | **MUST** 使编排层依赖抽象的 provider session 和 tool runtime，而不是依赖具体模型厂商 API。[需新增接口] |
| R2.3 | **MUST** 在编排层中保留取消、错误和工具失败的显式状态通道，而不是仅通过日志或字符串说明失败。[需新增接口] |
| R2.4 | **MUST NOT** 要求 feedback 层或前端层来驱动 turn 状态迁移。[需新增接口] |

### R3. 执行层闭环

| 编号 | 条款 |
|------|------|
| R3.1 | **MUST** 提供独立的 tool runtime 抽象，使 orchestrator 只面向统一的 tool request / tool result 交互。[需新增接口] |
| R3.2 | **MUST** 支持命令执行的取消、超时、cwd、env 和 stdout/stderr 流式输出语义。[需新增接口] |
| R3.3 | **SHOULD** 为执行层保留 sandbox 和 approval 的扩展点，即使 bootstrap 阶段不完整实现全部策略。[需新增接口] |
| R3.4 | **MUST NOT** 把 UI 展示字段作为执行层返回值的必要组成部分。[需新增接口] |

### R4. 记忆与 compact 边界

| 编号 | 条款 |
|------|------|
| R4.1 | **MUST** 在核心协议中为工作记忆、持久记忆和 compact 预留明确抽象边界，而不是将其硬编码进某个 provider adapter。[需新增接口] |
| R4.2 | **MUST** 使 bootstrap 阶段的记忆能力至少可表达“读取当前工作记忆”和“写入会话派生记忆”两类操作。[需新增接口] |
| R4.3 | **SHOULD** 将 compact 设计为可插拔策略，而不是 query loop 内部不可替换的特例流程。[需新增接口] |
| R4.4 | **MUST NOT** 让记忆层在 bootstrap 阶段依赖 forked agent 才能工作；forked extraction 只能作为后续增强能力。 |

### R5. 反馈层与可观测性

| 编号 | 条款 |
|------|------|
| R5.1 | **MUST** 定义统一的 feedback sink，使模型流事件、工具执行事件、错误事件和生命周期事件都可被外部消费。[需新增接口] |
| R5.2 | **MUST** 支持在无 TUI、无 TS bridge 的情况下运行核心流程。 |
| R5.3 | **MUST NOT** 让 orchestrator 直接依赖终端 UI、React/Ink 或前端回调。 |

### R6. 多模型适配

| 编号 | 条款 |
|------|------|
| R6.1 | **MUST** 提供 provider adapter 接口，使 Anthropic 仅作为其中一个实现，而不是默认内核协议。 [需新增接口] |
| R6.2 | **MUST** 使新增第二家模型 provider 时，不需要修改 `internal/core`、`internal/orchestrator`、`internal/executor`、`internal/memory`、`internal/feedback` 的公共接口。 [需新增接口] |
| R6.3 | **SHOULD** 在 bootstrap 阶段至少为第二家模型预留占位 adapter 或可验证接口，以证明设计不是 Anthropic-only。 [需新增接口] |

### R7. MCP 与 Agent 的 bootstrap 范围

| 编号 | 条款 |
|------|------|
| R7.1 | **MUST** 为 MCP 和 agent/subagent 保留可扩展抽象边界，但 bootstrap 阶段 **MUST NOT** 以完整 MCP runtime 或完整 subagent runtime 作为最小闭环前置条件。 [需新增接口] |
| R7.2 | **MUST** 使未来的 MCP 看起来像可注册 capability provider，而不是新的主编排入口。 [需新增接口] |
| R7.3 | **MUST** 使未来的 subagent 看起来像复用同一 orchestrator/executor/event model 的 task，而不是第二套 agent engine。 [需新增接口] |

### R8. 构建与验证

| 编号 | 条款 |
|------|------|
| R8.1 | **MUST** 在 `turnmesh` 中形成可构建的 Go module。 [需新增接口] |
| R8.2 | **MUST** 使 bootstrap 代码在本地可执行 `go build ./...`。 [需新增接口] |
| R8.3 | **SHOULD** 通过单元测试或最小集成测试验证 provider/orchestrator/tool runtime 的最小闭环。 [需新增接口] |

---

## 4. Bootstrap 范围判定规则

```text
IF 某能力是单轮 turn 闭环运行的必要条件
THEN 该能力属于 Bootstrap Scope

IF 某能力要求外部长生命周期会话、远端调度、复杂 auth 或多进程协调
THEN 该能力不属于 Bootstrap Scope，但必须有抽象边界

IF 某能力是模型厂商特有字段或协议细节
THEN 该能力只能进入 provider adapter，不得进入核心域模型

IF 某能力只是 UI 呈现差异
THEN 该能力不属于 Bootstrap Scope
```

---

## 5. 验收条件（Acceptance Criteria）

### AC1. Go 模块可构建

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && go build ./...
```

**预期结果**：退出码为 `0`

**当前结果（2026-04-13）**：已通过

### AC2. 核心层无 Anthropic SDK 依赖

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "anthropic|Anthropic|openai|OpenAI|gemini|Gemini" internal/core internal/orchestrator internal/executor internal/memory internal/feedback
```

**预期结果**：无输出

**当前结果（2026-04-13）**：已满足；模型厂商实现仅存在于 `internal/model/openai` 与 `internal/model/anthropic`

### AC3. Provider 抽象存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "type (Provider|Session) interface" internal/model internal/core
```

**预期结果**：至少命中 `2` 处定义

**当前结果（2026-04-13）**：已满足

### AC4. 核心事件模型存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "type (TurnEvent|ToolCall|ToolResult|ApprovalRequest|MemoryEntry|TaskState)" internal/core
```

**预期结果**：至少命中 `6` 处定义

**当前结果（2026-04-13）**：已满足

### AC5. 编排层最小入口存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "type Engine struct|func \\(.*\\) StreamTurn" internal/orchestrator
```

**预期结果**：至少命中 `2` 处定义

**当前结果（2026-04-13）**：已满足

### AC6. 执行层抽象存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "type (ToolRuntime|CommandExecutor|ToolRegistry) interface|type CommandRequest struct|type CommandResult struct" internal/executor
```

**预期结果**：至少命中 `5` 处定义

**当前结果（2026-04-13）**：已满足

### AC7. Bootstrap 流程可测试

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && go test ./...
```

**预期结果**：退出码为 `0`

**当前结果（2026-04-13）**：已通过

---

## 6. 范围

### 6.1 In Scope
1. Go module 初始化与可构建骨架
2. 核心域模型与事件模型
3. Provider / session 抽象
4. 最小 orchestrator turn 入口
5. 最小 tool runtime 抽象
6. feedback sink 抽象
7. memory/compact 的抽象边界
8. MCP 和 subagent 的抽象占位

### 6.2 Out of Scope
1. 完整 TUI 或 TS bridge 集成
2. 完整 MCP client/runtime
3. 完整 local/remote subagent runtime
4. 完整 session memory forked extraction
5. 完整 compact/retry/approval 策略
6. 任一模型厂商的完整生产级 adapter

### 6.3 迁移策略（Migration Strategy）

| 编号 | 约束 |
|------|------|
| M1 | 每完成一个模块后，**MUST** 立即执行 `go build ./...` 验证 |
| M2 | 每新增一个公共接口后，**SHOULD** 立即检查其是否泄漏厂商特有语义 |
| M3 | 每新增一个事件类型后，**SHOULD** 立即检查其是否可被 MCP 和 subagent 复用 |

### 6.4 回滚策略（Rollback Strategy）

| 场景 | 处理方式 |
|------|----------|
| 核心层出现厂商耦合 | 回退到上一个无厂商依赖的接口版本，并将厂商语义下沉到 adapter |
| orchestrator 无法在无 UI 条件下运行 | 回退到仅保留事件接口和 mock provider 的最小闭环 |
| 执行层接口无法表达取消/超时 | 回退到更小的命令执行抽象并重新定义请求/结果模型 |

---

## 7. 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 过早复制 TS 的超大上下文对象 | 高 | 将核心依赖拆成 provider、runtime、memory、feedback 的小接口 |
| 过早实现完整 MCP | 高 | 仅保留 provider/runtime 边界，不引入完整 transport 生命周期 |
| 过早实现完整 subagent | 高 | 仅保留 task/agent 抽象，不把 worker runtime 作为 bootstrap 阶段前置 |
| 多模型要求停留在口号层 | 高 | 在 bootstrap 阶段即定义统一 provider/session 抽象，并保留第二 adapter 占位 |
| 没有最小测试，导致骨架可编译但不可演进 | 中 | 至少为 orchestrator/provider/tool runtime 留下最小测试覆盖 |

---

## 8. 实施后核验问询（Post-Implementation Interrogation）

> 本节定义在 **所有编码与任务完成之后**，必须能够被回答的问题集合。
> 若任一问题无法基于代码或运行结果作答，则视为 **Spec 未满足**。
>
> ⚠️ **重要**：这些问题 **不能在 Plan/Task 阶段提前回答**，只在最终验收时使用。

### Q1. 核心协议如何保证对任意单一模型厂商不产生编译期耦合？
- **问题**：哪些包被定义为核心层，它们如何避免直接依赖任一模型厂商 SDK？
- **期望回答形式**：
  - 文件路径
  - 类型/接口名称
  - `rg` 或 `go list` 证据

### Q2. 单轮 turn 的稳定完成由哪一层负责闭环？
- **问题**：从输入到模型输出、工具执行再到继续推进，哪一个入口负责状态闭环？它如何表达中断与失败？
- **期望回答形式**：
  - 文件路径
  - 函数/方法名称
  - 状态迁移说明

### Q3. 当模型请求工具调用时，系统如何保证 orchestrator 不直接执行具体命令？
- **问题**：哪一个抽象隔离了 orchestrator 与命令执行细节？证据在哪里？
- **期望回答形式**：
  - 接口名称
  - 调用路径
  - 责任边界说明

### Q4. 当命令执行被取消或超时时，哪一层返回什么形态的结果？
- **问题**：取消/超时是如何被编码进执行层结果模型的？哪一层消费它？
- **期望回答形式**：
  - 结构体/枚举名称
  - 返回路径
  - 错误或状态字段说明

### Q5. 记忆层如何在没有 forked agent 的前提下仍然保持可接入？
- **问题**：本次 bootstrap 中，记忆层的最小读写能力落在哪些接口上？哪些增强能力被明确留到后续？
- **期望回答形式**：
  - 文件路径
  - 接口/类型名称
  - in-scope / out-of-scope 说明

### Q6. compact 为什么没有被硬编码进某个 provider adapter？
- **问题**：代码中哪里体现 compact 被保留为策略边界而不是厂商特例？
- **期望回答形式**：
  - 接口/类型名称
  - 所属包
  - 责任说明

### Q7. MCP 将来如何接入而不形成第二套主编排入口？
- **问题**：当前代码在哪些抽象上预留了 MCP capability provider 的位置？它如何避免主流程旁路？
- **期望回答形式**：
  - 文件路径
  - 接口名称
  - 边界说明

### Q8. subagent 将来如何复用同一 kernel，而不是复制主循环？
- **问题**：当前代码中，agent/task 抽象如何指向同一 orchestrator/executor/event model？
- **期望回答形式**：
  - 文件路径
  - 类型名称
  - 生命周期说明

### Q9. 如果增加第二家模型 provider，哪些包不需要修改？
- **问题**：如何从代码结构上证明多模型支持不是口号？
- **期望回答形式**：
  - 包路径
  - 接口名称
  - 变更影响说明

### Q10. 在无 TUI、无 TS bridge 的条件下，最小内核是否仍可运行和测试？
- **问题**：有哪些测试或运行入口证明内核可以脱离前端独立工作？
- **期望回答形式**：
  - 命令
  - 测试文件路径
  - 运行结果说明
