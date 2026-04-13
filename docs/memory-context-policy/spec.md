# Memory Context Policy 需求规格说明书

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
| `turnmesh/internal/memory/types.go` | 已有 `Store`、`Policy`、`CompactRequest`、`CompactResult` 抽象，但没有 turn 级读写协调器。 |
| `turnmesh/internal/memory/inmemory_store.go` | 当前只有基础 CRUD，未承载 turn 前注入、turn 后写回、compact 触发逻辑。 |
| `turnmesh/internal/memory/file_store.go` | 已有文件持久化能力，适合继续作为 durable/project memory 的基础 backend。 |
| `turnmesh/internal/core/types.go` | `TurnInput.Memory` 与 `TurnEventMemoryRead/Write` 已存在，核心协议已经预留记忆边界。 |
| `turnmesh/internal/orchestrator/types.go` | 现有 `Preparer` 只支持 turn 前预处理，没有标准的 turn 后 memory write/compact 协调接口。[需新增接口] |
| `claude-code-source-code/src/services/SessionMemory/sessionMemory.ts` | TS 的 session memory 由阈值驱动，触发条件与 token 增长、tool call 数、turn 边界相关。 |
| `claude-code-source-code/src/services/compact/compact.ts` | TS 的 compact 是显式独立能力，发生在 turn 边缘，并带有 post-compact rehydrate 语义。 |
| `claude-code-source-code/src/services/extractMemories/extractMemories.ts` | durable memory 抽取与主 loop 解耦，但依然依赖统一消息上下文和显式权限边界。 |

### 0.2 现有实现模式

- **依赖注入**：Go 侧已使用 `Store` / `Policy` 抽象，但还缺 orchestrator 可消费的 memory runtime。
- **错误处理**：memory 层当前以显式 error 返回为主，没有 turn 级错误归一化或降级策略。
- **事务管理**：当前没有跨组件事务；memory 更新更适合采用 turn 级别的“读快照、写决策、压缩决策”模型。

### 0.3 潜在冲突点

- 当前 `Policy` 只能回答单条 entry 或一组 entries 是否 compact，但还不能表达 turn 级读写策略。[需新增接口]
- 当前 orchestrator 只有 turn 前 `Preparer`，没有 turn 后写回或 compact hook。[需新增接口]
- TS 的 session memory / compact 依赖完整消息窗口，而 Go 当前 memory 层还看不到 turn transcript。[需新增接口]
- 当前 baseline 不能依赖 forked agent 才有 memory 行为；forked extraction 只能是后续增强。

---

## 1. 背景与问题

### 1.1 症状 A: 记忆层只有存储，没有 turn 级策略
**位置**：`turnmesh/internal/memory/types.go`
**现象**：当前只有 `Store` 和 `Policy`，没有“本轮 turn 应该读什么、写什么、何时 compact”的协调器。
**危害**：后续 memory 行为会被散落到 orchestrator、provider 或 UI 侧，形成补丁系统。

### 1.2 症状 B: 上下文注入与上下文回收没有统一边界
**位置**：`turnmesh/internal/core/types.go`，`internal/orchestrator/types.go`
**现象**：`TurnInput.Memory` 已存在，但谁来填充、何时刷新、何时删减没有稳定语义。
**危害**：长会话上下文无法治理，compact 也无从接入。

### 1.3 症状 C: TS 里的 memory/compact 明确由 turn 边缘驱动
**位置**：`claude-code-source-code/src/services/SessionMemory/sessionMemory.ts`，`compact.ts`
**现象**：TS 用显式阈值和 turn 边界驱动 memory/compact，而不是让 provider 自己隐式处理。
**危害**：如果 Go 不先建立同类边界，后续 subagent、bridge 和长会话都会返工。

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
| **Memory Snapshot** | 在 turn 开始前被注入到 `TurnInput.Memory` 的记忆视图。 |
| **Writeback Decision** | turn 结束后决定哪些消息/事件需要落入 memory store 的规则。 |
| **Compact Plan** | 对当前记忆集合进行删减、保留、摘要的决策结果。 |
| **Memory Runtime** | 协调 snapshot、writeback、compact 的 turn 级运行时边界。 |

---

## 3. 需求条款

### R1. Turn 级 Memory Runtime

| 编号 | 条款 |
|------|------|
| R1.1 | 系统 **MUST** 提供 turn 级 `MemoryRuntime` 或等价抽象，用于在 turn 前生成 `Memory Snapshot`，在 turn 后生成 `Writeback Decision` 与 `Compact Plan`。[需新增接口] |
| R1.2 | `MemoryRuntime` **MUST** 建立在 `Store` / `Policy` 之上，而不是把 memory 行为直接塞进 orchestrator 或 provider。 |
| R1.3 | `MemoryRuntime` **MUST NOT** 依赖 forked agent 才能工作。 |
| R1.4 | 当 memory 子系统未配置时，系统 **MUST** 退化为 no-op，而不是让 turn loop 崩溃。[需新增接口] |

### R2. Snapshot 与读取策略

| 编号 | 条款 |
|------|------|
| R2.1 | 系统 **MUST** 支持基于 scope/kind/metadata/query 的 snapshot 生成规则，而不是简单把 store 全量塞进 `TurnInput.Memory`。[需新增接口] |
| R2.2 | Snapshot 选择规则 **MUST** 保持 provider-agnostic，不允许依赖 OpenAI/Anthropic 专有字段。 |
| R2.3 | Snapshot 结果 **SHOULD** 保持稳定排序和 limit 语义，以便测试验证和 bridge 消费。 |

### R3. Writeback 与 turn 后策略

| 编号 | 条款 |
|------|------|
| R3.1 | 系统 **MUST** 允许基于 turn transcript、tool result、metadata 和 policy 生成 writeback 请求。[需新增接口] |
| R3.2 | Writeback 规则 **MUST** 能区分 working/session/durable/project 等不同 scope。 |
| R3.3 | Writeback 规则 **MUST NOT** 要求 provider adapter 自己写 memory。 |
| R3.4 | 当 writeback 被拒绝或跳过时，系统 **SHOULD** 返回显式决策结果，便于测试和后续反馈层消费。[需新增接口] |

### R4. Compact Policy

| 编号 | 条款 |
|------|------|
| R4.1 | 系统 **MUST** 支持显式 compact 触发条件，至少能基于 entry 数量、scope、reason 或外部 budget 触发 compact 计划。[需新增接口] |
| R4.2 | Compact **MUST** 先产生 `Compact Plan`，再由 runtime 决定是否执行删除/保留/摘要，而不是直接隐式删数据。[需新增接口] |
| R4.3 | Compact **MUST NOT** 依赖 UI 或 bridge 事件作为唯一触发来源。 |
| R4.4 | Compact **SHOULD** 支持“只计划不执行”的 dry-run 验证路径。[需新增接口] |

### R5. 与 orchestrator 的合作边界

| 编号 | 条款 |
|------|------|
| R5.1 | Memory Runtime **MUST** 通过显式输入输出与 orchestrator 合作，而不是让 `internal/memory` 直接拥有主 turn loop。[需新增接口] |
| R5.2 | orchestrator 与 memory 的合作接口 **MUST** 能表达 turn 前 snapshot 注入和 turn 后 writeback/compact 结果回传。[需新增接口] |
| R5.3 | 这套接口 **MUST NOT** 引入第二套 turn 状态机。 |

### R6. 可验证性

| 编号 | 条款 |
|------|------|
| R6.1 | 系统 **MUST** 为 snapshot、writeback、compact plan、degrade path 提供测试。 |
| R6.2 | 当前基线 **MUST** 保持 `go test ./...` 和 `go build ./...` 通过。 |

---

## 4. 决策规则

```text
IF 某记忆行为只能由 provider adapter 知道
THEN 该行为不满足 provider-agnostic memory boundary

IF compact 在执行前无法得到显式 plan
THEN 该设计不满足 compact policy 约束

IF memory 未配置会导致 turn loop 无法运行
THEN 该设计不满足 degrade path 约束
```

---

## 5. 验收条件（Acceptance Criteria）

### AC1. Memory Runtime 抽象存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "type (MemoryRuntime|Runtime|Coordinator|Manager) (struct|interface)" internal/memory
```

**预期结果**：至少命中 `1` 处可表达 turn 级 memory 协调的定义

### AC2. Snapshot/Writeback/Compact 入口存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "(Snapshot|PrepareTurn|Writeback|CompactPlan|PlanCompact|ApplyCompact)" internal/memory
```

**预期结果**：至少命中 `4` 处与 snapshot/writeback/compact 相关的实现或接口

### AC3. Memory 自测通过

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && go test ./internal/memory/...
```

**预期结果**：退出码为 `0`

### AC4. 全量可构建

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && go build ./...
```

**预期结果**：退出码为 `0`

---

## 6. 实施后核验问询（Post-Implementation Interrogation）

1. 当前内核是否已经存在 turn 前注入 memory snapshot、turn 后生成 writeback/compact 决策的稳定边界？
2. 如果没有配置任何 memory backend，主 turn loop 是否仍能正常运行？
3. memory/compact 的核心决策是否已经完全不依赖 provider adapter？
4. 当前 compact 是否已经从“隐式清理”变成“先有 plan，再决定执行”的显式策略？
