# Streaming Tool Runtime 需求规格说明书

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
| `turnmesh/internal/executor/types.go` | 当前已从 command-centric 调整为 generic tool surface，但还没有批处理/流式执行抽象。 |
| `turnmesh/internal/executor/tool_dispatcher.go` | dispatcher 仍是“单次调用 -> 单次结果”的同步形态。 |
| `turnmesh/internal/orchestrator/engine.go` | 现有 turn loop 会逐个执行收集到的 tool call，不支持并发分组、失败级联或 discard/fallback。 |
| `claude-code-source-code/src/services/tools/toolOrchestration.ts` | TS 会先按 `isConcurrencySafe` 对工具分批，再统一执行工具批次。 |
| `claude-code-source-code/src/services/tools/StreamingToolExecutor.ts` | TS 有显式 streaming tool executor，负责并发、取消、失败级联、丢弃和结果顺序。 |
| `claude-code-source-code/src/query.ts` | TS 在工具执行完成后统一回灌 tool_result，并允许中途刷新工具表和执行 post-tool attachments。 |

### 0.2 现有实现模式

- **依赖注入**：Go 侧 orchestrator 只依赖 `executor.Dispatcher`，当前边界正确，但能力不够。
- **错误处理**：Go 已有 `core.Error`、`ToolStatus`，适合继续扩展语义失败、取消和超时。
- **并发模型**：当前还没有显式 tool batch / streaming 执行模型。

### 0.3 潜在冲突点

- 如果直接把并发/流式逻辑塞进 orchestrator，会污染主 loop 边界。[需新增接口]
- 现有 `ToolSpec.ConcurrencySafe` 已存在，但还没有 runtime 利用它决定批处理策略。[需新增接口]
- MCP、本地命令、未来 subagent 工具都需要共享同一 tool runtime 语义，不能为不同来源分叉实现。

---

## 1. 背景与问题

### 1.1 症状 A: 统一工具面已经存在，但运行语义仍然单步同步
**位置**：`turnmesh/internal/executor/*`
**现象**：当前统一工具面能跑单次调用，但不能表达工具批次、并发安全分组、结果缓冲和失败级联。
**危害**：后续 MCP、多工具回合和 subagent 都会被迫退化成最慢路径。

### 1.2 症状 B: orchestrator 现在仍自己决定逐个执行工具
**位置**：`turnmesh/internal/orchestrator/engine.go`
**现象**：tool loop 逻辑还没有外包给专门的 tool runtime。
**危害**：越往后加 streaming/cancel/fallback，越容易把 orchestrator 变成大杂烩。

### 1.3 症状 C: TS 的工具执行是独立运行时
**位置**：`claude-code-source-code/src/services/tools/toolOrchestration.ts`，`StreamingToolExecutor.ts`
**现象**：TS 明确把工具批处理和 streaming execution 从 query loop 中抽出来。
**危害**：如果 Go 不做同样的边界收口，就很难达到 Claude Code 的真实 tool runtime 语义。

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
| **Tool Batch** | 一组在同一 tool round 中执行的工具调用集合。 |
| **Streaming Tool Runtime** | 负责批处理、并发、安全分组、取消和结果顺序的统一工具运行时。 |
| **Failure Cascade** | 某个工具失败后，对同批或后续工具的取消/丢弃规则。 |
| **Discard/Fallback** | 流式路径失败时，对尚未完成结果的丢弃或降级策略。 |

---

## 3. 需求条款

### R1. 批处理与运行时边界

| 编号 | 条款 |
|------|------|
| R1.1 | 系统 **MUST** 提供独立于 orchestrator 的 `Streaming Tool Runtime` 或等价抽象，用于执行一轮工具批次。[需新增接口] |
| R1.2 | 该 runtime **MUST** 接受统一工具调用集合，而不是只接受某一种工具来源。 |
| R1.3 | orchestrator **MUST NOT** 直接负责并发分组、失败级联和 discard/fallback 的实现细节。[需新增接口] |

### R2. 并发与分组规则

| 编号 | 条款 |
|------|------|
| R2.1 | 系统 **MUST** 基于 `ToolSpec.ConcurrencySafe` 或等价信号，把工具调用划分为“可并发批”和“必须串行批”。[需新增接口] |
| R2.2 | 可并发批中的工具 **MUST** 支持并行执行。 |
| R2.3 | 非并发安全工具 **MUST** 以串行或独占方式运行。 |
| R2.4 | 结果输出 **MUST** 保持稳定顺序，至少能按工具接收顺序重新组装。 |

### R3. 取消、失败级联与降级

| 编号 | 条款 |
|------|------|
| R3.1 | 系统 **MUST** 支持在工具运行期间响应 `context.Context` 取消。 |
| R3.2 | 当某个工具失败时，runtime **MUST** 有显式 failure cascade 语义，而不是让后续工具行为变成未定义。[需新增接口] |
| R3.3 | runtime **SHOULD** 支持流式路径失败时的 discard/fallback 语义。[需新增接口] |
| R3.4 | 这些语义 **MUST** 对本地工具和 MCP 工具一视同仁。 |

### R4. 与 orchestrator 的合作边界

| 编号 | 条款 |
|------|------|
| R4.1 | orchestrator **MUST** 只消费统一的工具批次执行结果，而不是了解内部并发细节。[需新增接口] |
| R4.2 | tool runtime 的输出 **MUST** 能被回填成现有 `tool_call -> tool_result -> continue` 闭环。 |
| R4.3 | 该边界 **MUST NOT** 引入第二套 turn loop。 |

### R5. 可观测性与验证

| 编号 | 条款 |
|------|------|
| R5.1 | 系统 **MUST** 为批次分组、结果顺序、取消和 failure cascade 提供测试。 |
| R5.2 | 当前基线 **MUST** 保持 `go test ./...` 和 `go build ./...` 通过。 |

---

## 4. 决策规则

```text
IF 某工具运行语义需要 orchestrator 直接判断线程或批次
THEN 该设计不满足 tool runtime 边界

IF 并发工具的结果顺序不可预测且不可恢复
THEN 该设计不满足稳定结果顺序约束

IF 某类工具来源拥有独立的取消/失败语义
THEN 该设计不满足统一工具面约束
```

---

## 5. 验收条件（Acceptance Criteria）

### AC1. Streaming Tool Runtime 抽象存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "type (StreamingToolRuntime|BatchExecutor|BatchRuntime|StreamingExecutor) (struct|interface)" internal/executor
```

**预期结果**：至少命中 `1` 处批处理/流式工具执行抽象

### AC2. 并发分组实现存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "(ConcurrencySafe|partition|batch|concurrent|serial)" internal/executor
```

**预期结果**：至少命中 `4` 处与工具分组/批处理相关的实现或测试

### AC3. Executor 自测通过

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && go test ./internal/executor/...
```

**预期结果**：退出码为 `0`

### AC4. 全量可构建

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && go build ./...
```

**预期结果**：退出码为 `0`

---

## 6. 实施后核验问询（Post-Implementation Interrogation）

1. 当前工具运行时是否已经从“单次同步调用”升级为“批次 + 并发 + 顺序保证”的独立边界？
2. orchestrator 是否仍然只负责 turn loop，而不是亲自处理并发和 failure cascade？
3. 本地工具与 MCP 工具是否已经共享同一套取消、失败和结果语义？
4. 流式路径失败时，系统是否至少已经定义出显式的 discard/fallback 行为？
