# 外部 Agent 实现复用 技术预研报告

**日期**: 2026-04-13

---

## 1. 任务理解

- 核心目标：评估 `ai-customer` 与 `knowledge-hub` 中现有 agent 实现，哪些可以接入当前 `turnmesh` 内核，哪些不能直接接。
- 预期价值：减少重复造轮子，优先复用已经成熟的工具、策略与交互语义，而不是把业务系统整块搬进内核。

## 2. 代码分析

### 相关模块

| 模块 | 路径 | 职责 | 需变更 |
|------|------|------|--------|
| 当前 Go 内核 agent runtime | `turnmesh/internal/agent/types.go` | 提供 `Start/Stop/GetTask/ListTasks` 与 injected runner 边界 | 作为接入目标，不应被外部实现反向覆盖 |
| 当前 Go 内核 tool runtime | `turnmesh/internal/executor/types.go` | 提供统一 `ToolSpec/ToolRuntime/ToolRequest` 执行面 | 可承接外部工具实现 |
| 当前 Go 内核 memory runtime | `turnmesh/internal/memory/runtime.go` | turn 前 snapshot、turn 后 writeback/compact | 可吸收外部上下文治理策略 |
| 当前总装层 | `turnmesh/cmd/engine/runtime_components.go` | memory coordinator、agent runner、工具过滤 | 外部复用应通过这一层或其上层完成 |
| `ai-customer` agent service | `ai-customer/internal/agent/service.go` | 直接调 OpenAI-compatible `/chat/completions`，自带 tool loop、pre-search、rewrite、budget | **不能直接接入 core** |
| `ai-customer` tools | `ai-customer/internal/agent/tools.go` | 面向客服知识库的业务工具执行器 | 可抽成 app-level tool handlers |
| `ai-customer` query rewrite / budget | `ai-customer/internal/agent/rewrite.go`, `budget.go` | 查询重写、token budget、历史裁剪 | 可迁移思路，不能原样塞 core |
| `knowledge-hub` agent service | `knowledge-hub/internal/agent/service.go` | 基于 langchaingo 的 Agent 主循环、memory、SSE、会话落库 | **不能直接接入 core** |
| `knowledge-hub` agent tools | `knowledge-hub/internal/agent/tools/*` | 检索、读文档、目录浏览等能力，依赖 ports | **最适合迁移** |
| `knowledge-hub` clarification / observer | `knowledge-hub/internal/agent/clarification_tool.go`, `runtime/observer.go`, `sse_observer.go` | 澄清语义与流式观察者协议 | 可作为事件模型参考或局部复用 |
| `knowledge-hub` MCP/tool contracts | `knowledge-hub/pkg/mcp/tool.go`, `pkg/tools/registry.go` | 统一工具契约与 registry | 与当前 executor 高度同构，可参考但没必要原样引入 |

### 关键入口

- `turnmesh/internal/agent/types.go`
- `turnmesh/internal/executor/types.go`
- `turnmesh/cmd/engine/runtime_components.go`
- `ai-customer/internal/agent/service.go:Execute`
- `ai-customer/internal/agent/tools.go:Execute`
- `knowledge-hub/internal/agent/service.go:Execute`
- `knowledge-hub/internal/agent/tools/search.go:Execute`
- `knowledge-hub/internal/agent/tools/read_document.go:Execute`
- `knowledge-hub/internal/agent/clarification_tool.go:Execute`

### 关键事实

- 两个外部实现都位于各自 module 的 `internal/...` 下。
  - `git.pinquest.cn/ai-customer/internal/agent`
  - `github.com/Jayleonc/knowledge-hub/internal/agent`
- 因 Go 的 `internal` 可见性规则，`turnmesh` **不能直接 import** 这两个包。
- `ai-customer/internal/agent/service.go` 把 provider HTTP 调用、工具循环、群聊业务上下文、历史消息裁剪、query rewrite 全写在一个服务里，属于业务成品，不是可拔插 runtime。
- `knowledge-hub/internal/agent/service.go` 自己 new `langchaingo` agent + executor + memory，也拥有另一套主循环，与当前“只能有一套主编排内核”的宪法冲突。
- `knowledge-hub/internal/agent/tools/ports.go` 的消费者定义接口模式很好，工具层比 service 层干净得多。
- `knowledge-hub/internal/agent/clarification_tool.go` 的“触发澄清 -> 中断当前回合 -> 把控制权交还 UI”语义非常有价值，但当前 `turnmesh` 还没有等价的一等事件。

## 3. 技术方案

### Option A: 直接搬 `ai-customer` 或 `knowledge-hub` 的 agent service

不可取。

原因：

- 语言层面就卡住：`internal` 包不能直接 import。
- 架构层面冲突：
  - `ai-customer/internal/agent/service.go` 自带 provider loop
  - `knowledge-hub/internal/agent/service.go` 自带 langchaingo loop
- 会违反当前仓库的三条硬原则：
  - 厂商中立
  - 只能有一套主编排内核
  - 执行层独立于模型层

### Option B: 只迁移 `knowledge-hub` 的工具层与澄清语义

这是最可行的复用方式。

可迁移对象：

- `internal/agent/tools/search.go`
- `internal/agent/tools/read_document.go`
- `internal/agent/tools/folder.go`
- `internal/agent/clarification_tool.go`
- `internal/agent/tools/ports.go`

迁移方式：

- 保留“消费者定义 ports”的思路
- 将工具实现改写为 `turnmesh/internal/executor.ToolRuntime` 或 `executor.NewHandlerTool(...)`
- 将业务上下文（tenant/project/dataset/user）从外部 app 层注入，而不是塞进 core
- 澄清工具先以 app-level 约定实现，再决定是否上升为 core event

### Option C: 从 `ai-customer` 迁移策略，而不是迁移 service

这是第二优先级。

可迁移对象：

- `budget.go` 的历史裁剪 / token budget 启发式
- `rewrite.go` 的 query rewrite/fallback 思路
- `tools.go` 里的客服场景工具定义

迁移方式：

- 预算与裁剪：吸收到未来 `memory policy / compact policy / context policy`
- query rewrite：放在 app-level preprocessor 或 tool policy，不进 core provider 层
- 工具定义：改造成当前统一工具面 handler

### 推荐

推荐采用 **“B 为主，C 为辅”**：

- **不要接 service**
- **优先接 `knowledge-hub` 的工具层**
- **补充接 `ai-customer` 的策略层**

一句话判断：

- `ai-customer`：**不能整块接，能拆策略和业务工具**
- `knowledge-hub/internal/agent`：**不能整块接，能拆工具层和澄清语义，而且价值更高**

## 4. 风险

- `knowledge-hub` 工具广泛依赖 `module.GetSpace(ctx)`、ACL、conversation、dataset 等上下文，迁移时必须先定义 app-level context contract。
- `ask_clarification` 的语义在当前 `turnmesh` 里没有一等事件，直接搬会退化成普通工具结果或 message hack。
- `ai-customer` 的 query rewrite 直接调用 OpenAI-compatible HTTP；如果不先抽象 provider/runtime，会把厂商耦合重新带回内核。
- 两边都有自己的历史记忆与会话落库模型，不能直接覆盖当前 `internal/memory/runtime.go`。

## 5. 依赖

- 如果迁移 `knowledge-hub` 工具层，需要先提供这些 app-level ports：
  - `KnowledgeSearcher`
  - `DocumentContentReader`
  - `ProjectNodeReader`
  - `DocumentAccessChecker`
- 如果迁移 `ai-customer` 客服工具，需要提供：
  - `khClient`
  - group/customer feature 数据读取
  - conversation/message store
- 如果要正式承接 `ask_clarification`，需要扩展：
  - `feedback` / bridge 事件协议
  - app/UI 侧双阶段澄清握手

## 6. 开放问题

- 你想接到 **内核仓库** 里，还是接到以后会基于内核构建的 **业务应用层**？
- `knowledge-hub` 的工具是想面向“知识库助手”场景复用，还是只借它的接口设计？
- `ai-customer` 的 query rewrite / pre-search 是不是要作为“客服专用策略”，而不是全局默认策略？

## 7. 下一步

- [ ] 先决定“接 service”还是“接 tool/policy”；我的建议是只接 `tool/policy`
- [ ] 如果确认复用 `knowledge-hub` 工具层，先写一个 `kh-tool-adapter` 的 spec
- [ ] 如果确认复用 `ai-customer` 的预算/重写策略，再写一个 `context-policy` 的 spec
