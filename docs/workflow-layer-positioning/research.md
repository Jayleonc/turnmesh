# Workflow 层与 Core 定位 技术预研报告

**日期**: 2026-04-14

---

## 1. 任务理解

- 核心目标：
  - 判断 `base/aiadminserver/impl` 中的 workflow 是否类似我们后面提到的“workflow 编排层”。
  - 澄清 `turnmesh` 的 core 到底应该被定义成什么：API 服务、SDK、框架、还是类似 langchaingo 的东西。
- 预期价值：
  - 统一工程定位，避免后续把 app 层、workflow 层和 core 层混在一起。

## 2. 代码分析

### 相关模块

| 模块 | 路径 | 职责 | 需变更 |
|------|------|------|--------|
| 当前 core 宪法 | `turnmesh/CONSTITUTION.md` | 定义单一主编排内核、厂商中立、执行层独立等硬原则 | 作为定位判断基线 |
| 当前 core 事实 | `turnmesh/DEV_LOG.md` | 已落地 provider registry、executor、memory runtime、agent runtime | 作为当前能力边界 |
| 当前 core 运行时总装 | `turnmesh/cmd/engine/bootstrap.go` | 组装 engine/runtime | 证明 core 当前是可嵌入 runtime，而不是产品 API |
| 当前 core agent runner | `turnmesh/cmd/engine/runtime_components.go` | 将 agent runtime 复用到单一 orchestrator | 证明 core 更像 runtime kernel |
| aiadminserver workflow 主入口 | `base/aiadminserver/impl/workflow.go` | 工作流 CRUD、版本、成员、运行等管理 API | 属于业务系统/平台层 |
| aiadminserver workflow factory | `base/aiadminserver/impl/internal/workflow/factory.go` | 根据节点类型构建节点 | 属于图式 workflow 引擎 |
| aiadminserver workflow state | `base/aiadminserver/impl/internal/workflow/types/context.go` | 工作流运行上下文（corp/app/chat 等） | 明显业务上下文驱动 |
| aiadminserver LLM 节点 | `base/aiadminserver/impl/internal/workflow/node/node_llm.go` | 在工作流节点中调用模型 | 把 LLM 当成图节点能力 |
| aiadminserver 知识查询节点 | `base/aiadminserver/impl/internal/workflow/node/node_query_knowledge.go` | 在工作流节点中调知识服务 | 把知识检索当成图节点能力 |
| aiadminserver trigger 节点 | `base/aiadminserver/impl/internal/workflow/node/node_trigger.go` | 将外部消息事件转成 workflow payload | 典型业务编排入口 |

### 关键入口

- `turnmesh/CONSTITUTION.md`
- `turnmesh/DEV_LOG.md`
- `turnmesh/cmd/engine/runtime_components.go`
- `base/aiadminserver/impl/workflow.go:CreateWorkflow`
- `base/aiadminserver/impl/internal/workflow/factory.go:CreateNodeBuilderFromDefinition`
- `base/aiadminserver/impl/internal/workflow/node/node_llm.go:Build`
- `base/aiadminserver/impl/internal/workflow/node/node_query_knowledge.go:Build`

### 关键事实

- `turnmesh` 当前已经有：
  - provider registry
  - unified tool runtime
  - memory runtime
  - agent runtime
  - runtime assembly
- 它的边界是：
  - 单一主 turn loop
  - 厂商中立
  - 执行层独立
  - 事件优先于 UI
- `aiadminserver/impl` 的 workflow：
  - 有数据库持久化工作流定义、版本、成员、运行日志
  - 有 node factory、trigger node、llm node、knowledge node、send_message node、transfer_to_human node
  - 用 `cloudwego/eino` 作为底层编排/graph 组件
  - 明确绑定 `corpId/appId/robotUid/chatId` 等业务上下文
- 因此，`aiadminserver` workflow 不是 core kernel，而是一个**上层业务工作流平台**。

## 3. 技术方案

### Option A: 把 `turnmesh` 定义成 API 服务

不推荐。

原因：

- 当前代码主体是 `internal/*` runtime，不是面向外部协议的 API 产品。
- 一旦把它定义成 API 服务，租户、鉴权、审计、secret、workflow、UI 等都会被迫往 core 里塞。
- 这会破坏“核心边界清晰”和“单一主编排内核”的设计目标。

### Option B: 把 `turnmesh` 定义成可嵌入的 AI runtime SDK / kernel

推荐。

定义：

- 它是一个 **可嵌入的 AI 运行时内核**
- 不是最终产品
- 不是直接面向业务用户的 API 服务
- 也不是纯算法库

它提供的是：

- provider-agnostic session/runtime
- 单一 turn loop
- 统一工具面
- memory/context runtime
- agent/subagent runtime
- 未来的 approval/policy/hooks 边界

其他应用通过 import/adapter 使用它，就像：

- `kh` 这样的知识助手产品
- `ai-customer` 这样的客服助手产品
- 未来的企业内部 copilot / ops assistant / workflow node

### Option C: 把 `turnmesh` 定义成“像 langchaingo 一样的框架”

部分像，但不能简单等同。

相似点：

- 都是给应用层复用的基础能力，而不是最终业务产品
- 都提供模型、工具、链路的抽象

不同点：

- `langchaingo` 是通用 LLM framework
- `turnmesh` 更像 **Claude Code 风格的 agent runtime kernel**
- 它比 langchaingo 更“运行时导向”，而不是“组件拼装导向”
- 它更强调：
  - 单一主 loop
  - tool call / tool result 闭环
  - memory/compact 边界
  - subagent 生命周期
  - provider/mcp/tool 的统一运行语义

### 推荐

推荐最终定位：

- `turnmesh` = **AI Runtime Kernel / Embeddable SDK**
- `kh` / `ai-customer` / 企业助手 = **基于 core 的应用层**
- `aiadminserver` workflow = **位于 core 之上的 workflow / orchestration 平台**

最合理的分层是：

1. **Core 层**
   - provider
   - orchestrator
   - executor
   - memory
   - agent runtime
   - mcp runtime

2. **App/Adapter 层**
   - knowledge tool adapters
   - tenant context
   - permission / approval implementation
   - audit sink
   - secret provider
   - product-specific system prompt / policy

3. **Workflow/API/UI 层**
   - 可视化工作流
   - workflow version / run log
   - webhook / trigger / schedule
   - SSE / HTTP / admin API
   - human-in-the-loop

## 4. 风险

- 如果把 workflow、租户、权限、审计直接塞进 core，会导致 core 失去可复用性。
- 如果把 core 做成“又是 SDK 又是完整平台”，后面会出现两套设计语言：runtime language 和 product language。
- 如果未来 workflow 引擎直接绕开 core 去调模型或工具，会重新长出第二套主循环。

## 5. 依赖

- 如果后续真做 workflow 层，需要 core 先补：
  - approval/policy hooks
  - 更完整的 tool/resource/prompt surface
  - 更强的 agent/subagent orchestration
  - 运行日志/审计 sink 接口
- 如果借鉴 `aiadminserver`，应借的是：
  - workflow graph/version/run 模型
  - node factory / node compiler
  - 上层 orchestration 形态
  - 不应借它对底层模型调用的实现方式

## 6. 开放问题

- 你想让 `turnmesh` 未来暴露成 Go package 为主，还是 package + thin service wrapper 双形态？
- workflow 层是要“图形化可编排”，还是先做“代码式 workflow”？
- human approval / audit / secret 这些企业能力，是想先落在 app 层，还是要先在 core 定义标准接口？

## 7. 下一步

- [ ] 先确认 core 的正式定位：`AI runtime kernel / embeddable SDK`
- [ ] 如果确认 workflow 层存在，再写 `workflow-platform` 的 spec，明确它位于 core 之上
- [ ] 后续企业能力优先顺序建议：approval/policy hooks -> audit/secret interfaces -> resources/prompts -> workflow layer
