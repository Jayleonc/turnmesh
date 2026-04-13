# Go Kernel Bootstrap 后续演进技术实现计划

> **编译声明**：本 Plan 描述系统状态变换，不是操作指南。
> 具体的文件修改、命令执行，由后续 tasks.md 给出。

**基于 Spec 版本**: 1.1
**日期**: 2026-04-13

---

## 1. 当前系统状态

### 1.1 Kernel 基线现状

| 模块 | 方法/文件 | 当前状态 |
|------|----------|------|
| 入口装配 | `cmd/engine/main.go` | 只完成 `orchestrator.Engine` 启动，未形成 provider registry 和统一 runtime 装配入口。 |
| 核心协议 | `internal/core/types.go` | Turn、Event、Tool、Memory、Task、Approval 抽象已存在，且保持厂商中立。 |
| 编排层 | `internal/orchestrator/engine.go` | 已支持显式 turn loop、tool_call -> execute -> tool_result -> continue、多 pass 限流。 |
| 执行层 | `internal/executor/tool_dispatcher.go`、`registry.go` | 已有本地 tool registry 和统一 dispatcher，但当前 runtime 仍以本地注册工具为主。 |
| Provider 抽象 | `internal/model/provider.go`、`session.go` | Provider / Session 抽象已固定，OpenAI 与 Anthropic adapter 已接入。 |
| OpenAI adapter | `internal/model/openai/*` | 已支持 Responses API 最小闭环，但仍为非流式实现。 |
| Anthropic adapter | `internal/model/anthropic/*` | 已支持 Messages API tool_use 闭环，但未覆盖 SSE 与更复杂恢复语义。 |
| 记忆层 | `internal/memory/*` | 已有 `InMemoryStore`、`FileStore`，但尚无 retrieval ranking、compact policy、策略编排。 |
| 反馈层 | `internal/feedback/*` | 已有结构化 sink，当前足够支撑无 UI 的基线调试。 |
| MCP | `internal/mcp/client.go`、`stdio_transport.go` | 已有最小 stdio runtime 和 JSON-RPC request/response 关联，但未桥接到 executor tool surface。 |
| Agent | `internal/agent/*` | 仅有定义与 registry 边界，未形成 runtime。 |

### 1.2 当前阶段判断

- 截至 **2026-04-13**，`kernel-bootstrap` 的 **Bootstrap Baseline 已完成**。
- `go test ./...` 与 `go build ./...` 已再次本地验证通过。
- 当前阶段不再是“是否能跑起来”，而是 **如何把已存在的最小能力收口成统一 runtime，并为后续能力留出不返工的演进路径**。

### 1.3 不满足后续演进目标的缺口清单

| 约束/目标 | 当前缺口 |
|------|-------------|
| 单一统一装配入口 | Provider、Executor、Memory、MCP 尚未通过单一 assembly 入口绑定。 |
| MCP 进入统一工具面 | `internal/mcp` 仍是独立 runtime，`orchestrator` 还不能把 MCP 当普通工具调用。 |
| 记忆策略独立化 | Memory 只有存储，没有 retrieval / compact / policy 编排。 |
| Provider 运行时稳定性 | 两家 adapter 都是最小闭环，流式、重试、健壮错误恢复仍缺失。 |
| Agent 复用同一内核 | `internal/agent` 还没有 runtime，尚未验证“不复制第二套 orchestrator”。 |
| 外部接入能力 | 尚未形成 TS bridge / CLI 渐进接管所需的稳定边界。 |

---

## 2. 目标系统状态

### 2.1 目标阶段定义

本计划的目标不是再证明“baseline 可运行”，而是把当前系统推进到 **可扩展内核阶段**：

- 统一装配入口存在，运行时依赖以 registry / assembly 的方式被构造。
- MCP 能通过 executor 的统一工具面进入 turn loop。
- 记忆读取、写入、compact 成为独立策略面，而不是后续补丁。
- Provider 增强不改变核心公共接口。
- Agent / Subagent 以 task runtime 方式复用当前 orchestrator/executor/event model。
- TS bridge / CLI 可以在不污染核心层的前提下消费统一事件流。

### 2.2 目标约束达成方式

| 目标约束 | 达成方式 |
|------|----------|
| 厂商中立核心不被污染 | 所有新增 provider 差异继续收敛在 `internal/model/*`。 |
| 单一主编排内核 | MCP、Agent、TS bridge 全部通过现有 orchestrator / executor / event model 接入。 |
| 统一工具执行面 | 本地工具与 MCP 工具共享 dispatcher / registry 抽象。 |
| 可验证 | 每个阶段都保持 `go test ./...`、`go build ./...` 通过，并补充必要测试。 |
| 后续可扩展 | 将 assembly、policy、bridge、agent 都建成边界，而非特例实现。 |

---

## 3. 状态变换（Phases）

### Phase 1: Runtime Assembly 收口

**目标约束**：
- 单一主编排内核
- 统一装配入口
- 保持核心边界清晰

**输入系统状态**：
- `cmd/engine/main.go` 只能启动裸 `orchestrator.Engine`
- Provider、tools、memory、feedback、MCP 未形成统一构造路径

**输出系统状态**：
- 存在唯一合法的 runtime assembly 入口
- Engine 不再依赖临时拼装，而是消费标准化装配结果
- **强制产物：Kernel Assembly Contract**

**强制产物：Kernel Assembly Contract**

| 字段 | 说明 |
|------|------|
| Provider Registry | Provider 的注册、查找、默认选择规则 |
| Session Factory | 基于 provider/model/options 创建 session |
| Tool Runtime | 本地工具与未来 MCP 工具的统一运行面 |
| Memory Services | 工作记忆与持久记忆的装配句柄 |
| Feedback Sink | 默认 sink 与可替换 sink 的边界 |

**保持不变量**：
- `internal/core`、`internal/orchestrator` 不引入厂商字段
- `cmd/engine` 只是装配入口，不承担业务状态机
- 现有 `Engine.StreamTurn()` 契约不被破坏

### Phase 2: MCP 统一工具面桥接

**目标约束**：
- MCP 不拥有主编排权
- 所有工具经统一执行面进入 turn loop
- provider-agnostic 设计得到真实验证

**输入系统状态**：
- `internal/mcp` 可独立 `initialize`、`tools/list`、`tools/call`
- `internal/executor` 只能消费本地注册工具

**输出系统状态**：
- MCP 服务器暴露能力可转换为 executor 可注册工具
- `orchestrator` 对 MCP 与本地工具无感知差异
- **强制产物：MCP Tool Adapter Boundary**

**强制产物：MCP Tool Adapter Boundary**

| 字段 | 说明 |
|------|------|
| Server Identity | MCP 服务器实例标识与生命周期 |
| Tool Name Mapping | 远端 tool 名称到本地统一命名空间的映射 |
| Argument Translation | `core.ToolInvocation` 与 MCP `CallRequest` 的转换规则 |
| Result Normalization | MCP 返回内容到 `core.ToolResult` 的归一化规则 |
| Failure Semantics | transport error、timeout、server error 的统一错误模型 |

**关键约束：MCP 不得形成旁路入口**

> MCP 客户端只能通过 executor 暴露能力，不能让 orchestrator、provider 或 bridge 直接依赖 MCP transport。
>
> 这个约束防止 Phase 2 建出第二套运行时入口，导致后续 Agent 和 TS bridge 需要兼容双路径。

**具体约束**：
- `orchestrator` 只看 `executor.Dispatcher`
- provider adapter 不直接持有 `mcp.Client`
- MCP server 生命周期由 assembly/runtime 持有，不由单次 turn 临时创建

### Phase 3: Memory Policy 与 Compact 策略化

**目标约束**：
- 记忆是独立策略系统
- compact 可插拔
- 不依赖 forked agent 才能工作

**输入系统状态**：
- memory 只有 store 抽象和最小实现
- turn loop 未感知工作记忆读取、持久记忆写入、compact 边界

**输出系统状态**：
- 记忆读取、写入、compact 都成为显式策略面
- turn 前/turn 后/阈值触发点明确
- **强制产物：Memory Policy Contract**

**强制产物：Memory Policy Contract**

| 字段 | 说明 |
|------|------|
| Read Path | Turn 开始前哪些记忆进入 `core.TurnInput.Memory` |
| Write Path | Turn 结束后哪些事件或消息会落入 store |
| Compact Hook | 何时允许 compact，输入输出边界是什么 |
| Policy Inputs | token、message count、tool count、session metadata 等决策输入 |
| Degrade Path | 无 compact / 无 retrieval 时的退化行为 |

**保持不变量**：
- Memory policy 不依赖特定 provider
- Compact 不是 provider adapter 内部技巧
- 没有 subagent runtime 时也能工作

### Phase 4: Provider Runtime 强化

**目标约束**：
- 新增 provider 不修改核心公共接口
- turn loop 能承接更真实的流式与失败语义

**输入系统状态**：
- OpenAI / Anthropic adapter 都已可用，但偏最小闭环
- 流式、复杂错误恢复、限流与重试策略不完备

**输出系统状态**：
- provider 增强只发生在 adapter 层
- 流式事件、恢复语义、结构化失败路径被统一映射进 `core.TurnEvent`
- **强制产物：Provider Compatibility Matrix**

**强制产物：Provider Compatibility Matrix**

| 字段 | 说明 |
|------|------|
| Stream Support | 是否支持增量输出与事件顺序要求 |
| Tool Loop Semantics | 各 provider 的 tool call continuation 差异 |
| Retry / Timeout Policy | 失败重试与超时边界 |
| Stateful Session | 会话 continuation 与 provider-specific state 的封装方式 |
| Unsupported Features | 当前明确不支持的差异项 |

**关键约束：增强不能反向污染核心**

> 任何为了流式、thinking、parallel tool use 增加的字段，都只能停留在 adapter 内部映射层。

### Phase 5: Agent / Subagent Runtime 复用化

**目标约束**：
- Agent 不复制第二套 orchestrator
- task 生命周期进入统一事件模型

**输入系统状态**：
- `internal/agent` 只有 definition / registry 边界
- 尚未证明 subagent 能作为同一内核之上的 task 运行

**输出系统状态**：
- agent/subagent 作为 task runtime 复用当前 orchestrator、executor、feedback、memory policy
- spawn / join / fail / cancel 事件进入统一 `TurnEvent` / `TaskState` 体系
- **强制产物：Agent Task Runtime Contract**

**⚠️ 入口裁决规则（单入口强制）**

对于以下操作，对外入口只能是 **kernel runtime / orchestrator facade**：

| 操作 | 入口 | 内部方法 | 可见性 |
|------|------|---------|--------|
| 主 turn 执行 | runtime facade | orchestrator core methods | 内部 |
| subagent 创建 | runtime facade | agent task launcher | 内部 |
| tool 调用 | runtime facade | executor dispatcher | 内部 |
| 事件转发 | runtime facade | feedback sink / task event bridge | 内部 |

**入口规则**：
1. 外部系统不直接 new 第二个 agent engine
2. Subagent 只是在共享核心协议上的另一类 task
3. 跨 agent 的工具、记忆、事件策略继续走统一边界

### Phase 6: TS Bridge / CLI 渐进接管

**目标约束**：
- 先内核，后前端
- UI 只能消费事件，不能定义核心状态

**输入系统状态**：
- Go kernel 已具备 baseline，但没有外部稳定接入协议
- TS/CLI 尚无法系统性消费 turn/task/memory 事件

**输出系统状态**：
- bridge 作为边界层消费 kernel 事件并暴露稳定协议
- CLI/TUI 的状态从事件流派生，而不是反向驱动内核
- **强制产物：Bridge Event Contract**

**强制产物：Bridge Event Contract**

| 字段 | 说明 |
|------|------|
| Transport | stdio / RPC / socket 等桥接方式 |
| Event Envelope | 对外暴露的事件序列化格式 |
| Request Envelope | 外部输入如何形成 `TurnInput` / runtime request |
| Lifecycle | session open/turn start/interrupt/shutdown 的语义 |
| Compatibility | Go kernel 版本变化时的兼容策略 |

**保持不变量**：
- Bridge 不持有主状态机
- TS CLI 可以替换，但 kernel 契约保持稳定

---

## 4. 技术决策（ADR）

### ADR-1: 先做 Runtime Assembly，再接 MCP

**背景**：当前最急的表面任务是把 MCP 接入 executor，但入口装配还没收口。

**选项**：
- A：先定义统一 assembly，再把 MCP 接进 tool runtime
- B：直接把 `mcp.Client` 塞进现有 executor/orchestrator

**决策**：选择 A

**理由**：
- 先收口入口，后续 MCP、memory、agent 才有稳定挂载点
- 避免为了快接 MCP 把 transport 生命周期散落到各层
- 能更早定义“唯一合法构造路径”，减少返工

### ADR-2: MCP 作为 Tool Adapter，而不是新 Runtime

**背景**：MCP 既像远端能力源，又可能诱导出独立调用路径。

**选项**：
- A：将 MCP server 暴露能力适配成 executor 工具
- B：让 orchestrator 或 provider 直接调用 MCP client

**决策**：选择 A

**理由**：
- 保持单一工具执行面
- 可以让 OpenAI / Anthropic / future provider 共享同一工具入口
- 防止形成 “本地工具一套、MCP 工具一套” 的双系统

### ADR-3: Memory 先策略化，再高级化

**背景**：当前 store 已有，但 retrieval ranking、compact 和抽取策略没有边界。

**选项**：
- A：先建立 policy / hook / degrade path，再逐步增强 retrieval 与 compact
- B：等 forked agent/subagent 做好后再统一补记忆能力

**决策**：选择 A

**理由**：
- 记忆是主循环边界问题，不是后置增强插件
- 先把策略入口固定，后续高级能力才不会污染 orchestrator
- 可先用简单策略跑通，再演进复杂提取

### ADR-4: Agent 必须表现为 Task Runtime

**背景**：subagent 很容易演化成第二套 loop。

**选项**：
- A：agent 复用当前 orchestrator/executor/event model
- B：为 agent 独立设计新的运行时主循环

**决策**：选择 A

**理由**：
- 符合单一主编排内核原则
- 共享工具、记忆、反馈和取消语义
- 降低 bridge、测试、观测体系的复杂度

---

## 5. 风险与退化路径

| 风险 | 影响 | 退化路径 |
|------|------|----------|
| 先接 MCP 再收口 assembly | 生命周期散落、入口失控 | 回退到统一 assembly 设计后再桥接 MCP |
| Memory policy 设计过早绑定 provider | 后续多模型扩展受阻 | 将 provider 特有逻辑收回 adapter，只保留通用策略输入 |
| Provider 流式增强带入厂商字段 | 核心协议被污染 | 在 adapter 内做事件映射，核心只接收统一事件 |
| Agent runtime 复制 orchestrator | 形成双主流程、测试面倍增 | 强制以 task runtime contract 收口，并复用既有 loop |
| Bridge 反向定义状态 | UI 绑架内核演进 | 只暴露 event/request contract，拒绝前端反向注入核心状态 |

---

## 6. 验收映射

### 6.1 Spec AC 映射

| 验收项 | 当前状态 | 后续计划中的保持方式 |
|------|------|----------|
| AC1 `go build ./...` | 已满足 | 每一 Phase 保持可构建，不允许阶段性破坏基线 |
| AC2 核心层无厂商依赖 | 已满足 | Provider 增强、MCP、Agent、Bridge 全部继续隔离在边界层 |
| AC3 Provider 抽象存在 | 已满足 | 通过 assembly 与 compatibility matrix 巩固新增 provider 的稳定接入 |
| AC4 核心事件模型存在 | 已满足 | 后续 task、memory、bridge 都继续复用同一事件模型 |
| AC5 编排层最小入口存在 | 已满足 | 继续保留 `Engine.StreamTurn()` 作为核心 turn 入口 |

### 6.2 下一阶段优先级排序

1. **Phase 1 Runtime Assembly 收口**
2. **Phase 2 MCP 统一工具面桥接**
3. **Phase 3 Memory Policy 与 Compact 策略化**
4. **Phase 4 Provider Runtime 强化**
5. **Phase 5 Agent / Subagent Runtime 复用化**
6. **Phase 6 TS Bridge / CLI 渐进接管**

### 6.3 里程碑判断

- 完成 Phase 1-2 后：项目从“baseline 可运行”进入“统一 runtime 可扩展”
- 完成 Phase 3-4 后：项目从“最小闭环”进入“真实 agent kernel 雏形”
- 完成 Phase 5-6 后：项目从“内核实验仓库”进入“可对外接管 CLI/Bridge 的内核系统”
