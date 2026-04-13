# Agent Runtime 需求规格说明书

**版本**: 1.0
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
| `turnmesh/internal/agent/types.go` | 已有 `Definition`、`TaskStatus`、`Event`、`Task`、`Runtime` 的最小边界，但缺任务注册、生命周期状态机和 runner 注入模型。 |
| `turnmesh/internal/agent/runtime.go` | 当前只有 `Registry` 抽象，没有实际 runtime 实现。 |
| `turnmesh/internal/core/types.go` | `TaskState`、`AgentDefinition`、task 相关 `TurnEvent` 已存在，适合继续复用。 |
| `claude-code-source-code/src/tools/AgentTool/runAgent.ts` | TS 的 agent 运行会复用主 query loop，并允许 agent-specific MCP servers、tool pool 和 cleanup。 |
| `claude-code-source-code/src/tools/AgentTool/forkSubagent.ts` | TS 的 fork/subagent 仍然基于同一会话语义，只是继承上下文和工具池。 |
| `claude-code-source-code/src/tasks/LocalAgentTask/LocalAgentTask.tsx` | TS 的本地 agent task 具有显式任务状态、进度、消息队列、后台化和通知生命周期。 |

### 0.2 现有实现模式

- **依赖注入**：Go 侧 agent 目前只有接口，适合通过 injected runner 复用现有 kernel runtime，而不是复制一套 orchestrator。
- **错误处理**：已有 `TaskStatus` / `Event`，但没有统一的 start/stop/fail/cancel 状态机实现。
- **生命周期**：当前 agent 没有任务注册、查询、背景运行或清理模型。

### 0.3 潜在冲突点

- 如果 agent runtime 直接自己 new 一套 orchestrator，会违反单一主编排内核原则。[需新增接口]
- 如果 agent task 没有 registry，就很难支撑 stop/join/status/query 等语义。[需新增接口]
- TS 的 agent 能继承工具池、MCP 和上下文；Go 当前还没有表达“agent-specific runtime overlay”的边界。[需新增接口]

---

## 1. 背景与问题

### 1.1 症状 A: agent 只有类型，没有运行时
**位置**：`turnmesh/internal/agent/types.go`
**现象**：当前只有抽象定义，无法真正 start/stop 一个 agent task。
**危害**：subagent 仍然停留在“留了边界”的阶段，没有被真实验证。

### 1.2 症状 B: 缺少任务生命周期状态机
**位置**：`turnmesh/internal/agent/types.go`
**现象**：虽然有 `TaskStatus` 和 `Event`，但没有任务注册、状态迁移、清理与查询能力。
**危害**：后续 bridge/CLI 无法稳定消费 agent 运行时。

### 1.3 症状 C: TS 的 agent 是复用主内核的 task
**位置**：`claude-code-source-code/src/tools/AgentTool/runAgent.ts`，`LocalAgentTask.tsx`
**现象**：TS 并没有复制第二套 query engine，而是把 agent 作为带 task lifecycle 的运行单元。
**危害**：如果 Go 不按这个方向做，很容易长出第二套 orchestrator。

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
| **Agent Task** | 一次可追踪、可停止、可查询状态的 agent/subagent 运行实例。 |
| **Task Registry** | 跟踪 agent task 生命周期、状态和事件通道的运行时组件。 |
| **Runner** | 真正执行 agent 逻辑的注入式执行边界；本 Spec 中它必须复用现有 kernel，而不是自建第二套主循环。 |
| **Runtime Overlay** | 某个 agent 在工具池、MCP、system prompt、metadata 上对父运行时的局部覆盖。 |

---

## 3. 需求条款

### R1. Agent Runtime 基础能力

| 编号 | 条款 |
|------|------|
| R1.1 | 系统 **MUST** 提供可用的 `agent.Runtime` 实现，而不仅是接口定义。[需新增接口] |
| R1.2 | 该 runtime **MUST** 支持 `Start`、`Stop` 以及任务状态查询所需的内部注册能力。[需新增接口] |
| R1.3 | runtime **MUST NOT** 直接复制第二套 orchestrator 或 turn loop。 |

### R2. 任务生命周期与状态机

| 编号 | 条款 |
|------|------|
| R2.1 | Agent task **MUST** 至少具备 `pending -> running -> completed/failed/stopped` 的显式状态迁移。 |
| R2.2 | runtime **MUST** 能按 task ID 查询任务、列出任务并安全停止任务。[需新增接口] |
| R2.3 | task event 流 **MUST** 包含启动、状态变化、进度、完成和失败的显式事件。[需新增接口] |
| R2.4 | 任务结束后 **SHOULD** 保留足够的最终状态以供后续 bridge/CLI 查询。[需新增接口] |

### R3. Runner 注入与复用主内核

| 编号 | 条款 |
|------|------|
| R3.1 | Agent runtime **MUST** 通过 injected runner 或等价抽象复用现有 kernel runtime，而不是在 agent 包中硬编码 provider/executor/orchestrator 组合。[需新增接口] |
| R3.2 | 该 runner 抽象 **MUST** 足以表达 agent definition、input、context、runtime overlay 和事件回传。[需新增接口] |
| R3.3 | agent runtime **MUST NOT** 依赖特定 provider adapter。 |

### R4. Runtime Overlay

| 编号 | 条款 |
|------|------|
| R4.1 | 系统 **MUST** 允许 agent definition 对 system prompt、工具池、metadata、背景运行属性做局部覆盖。[需新增接口] |
| R4.2 | runtime overlay **MUST** 是对父运行时的显式派生，而不是重新定义一整套全局状态。 |
| R4.3 | runtime overlay **SHOULD** 为后续 agent-specific MCP server 和隔离工作区预留边界。[需新增接口] |

### R5. 可观测性与验证

| 编号 | 条款 |
|------|------|
| R5.1 | 系统 **MUST** 为任务状态机、stop 路径、事件发射和 runner 注入路径提供测试。 |
| R5.2 | 当前基线 **MUST** 保持 `go test ./...` 和 `go build ./...` 通过。 |

---

## 4. 决策规则

```text
IF agent runtime 需要自己 new 一套 orchestrator/executor/provider
THEN 该设计不满足单一主编排内核约束

IF 一个 task 无法被查找、停止或确认最终状态
THEN 该设计不满足 task lifecycle 约束

IF runtime overlay 会覆盖全局状态而不是派生局部配置
THEN 该设计不满足 agent overlay 约束
```

---

## 5. 验收条件（Acceptance Criteria）

### AC1. Agent Runtime 实现存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "type .*Runtime struct|func New.*Runtime" internal/agent
```

**预期结果**：至少命中 `1` 处 agent runtime 实现或构造入口

### AC2. Task Registry / 状态查询入口存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "(GetTask|ListTasks|LookupTask|Stop\\(|Start\\(|TaskStatus)" internal/agent
```

**预期结果**：至少命中 `5` 处与任务生命周期相关的实现或测试

### AC3. Agent 自测通过

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && go test ./internal/agent/...
```

**预期结果**：退出码为 `0`

### AC4. 全量可构建

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && go build ./...
```

**预期结果**：退出码为 `0`

---

## 6. 实施后核验问询（Post-Implementation Interrogation）

1. 当前 agent runtime 是否已经可以 start/stop/query 一个 agent task，而不是只停留在接口定义？
2. task 生命周期是否已经有显式状态机和事件流？
3. agent 是否通过 injected runner 复用现有 kernel，而不是自带第二套 orchestrator？
4. runtime overlay 是否已经能表达局部工具池/system prompt/metadata 覆盖，而不污染全局运行时？
