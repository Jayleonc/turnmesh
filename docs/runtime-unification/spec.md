# Runtime Unification 需求规格说明书

**版本**: 1.0
**日期**: 2026-04-13
**状态**: Draft

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
| `turnmesh/cmd/engine/main.go` | 当前入口只创建 `orchestrator.Engine` 并注入 `feedback.StdoutSink`，还没有统一装配入口。 |
| `turnmesh/internal/orchestrator/types.go` | `orchestrator.Config` 目前直接持有 `model.Session`、`executor.Dispatcher`、`feedback.Sink`，说明运行时构造仍是平铺式依赖注入。 |
| `turnmesh/internal/orchestrator/engine.go` | Turn loop 已稳定，且只依赖抽象 `Session` 与 `Dispatcher`；这里不应再塞 provider/MCP 特例逻辑。 |
| `turnmesh/internal/model/provider.go` | 已有 `Provider` / `Session` 抽象，但没有 provider registry、默认选择策略或统一创建入口。 |
| `turnmesh/internal/model/openai/provider.go` | OpenAI adapter 已独立实现 `Provider`，说明 provider registry 可以建立在现有抽象之上。 |
| `turnmesh/internal/model/anthropic/provider.go` | Anthropic adapter 已独立实现 `Provider`，且不污染核心层。 |
| `turnmesh/internal/executor/types.go` | 当前 `Runtime.Execute()` 和 `ToolRuntime.Execute()` 都以 `CommandRequest` / `CommandResult` 为中心，执行层仍偏 command-centric。 |
| `turnmesh/internal/executor/tool_dispatcher.go` | `ToolDispatcher` 会把 `core.ToolInvocation` 解码成命令输入，再路由到 runtime，当前只适合本地命令类工具。 |
| `turnmesh/internal/executor/registry.go` | 已有按名称注册工具的内存 registry，可作为统一工具表的起点。 |
| `turnmesh/internal/mcp/client.go` | MCP client 已支持 `initialize`、`tools/list`、`tools/call`，具备被适配进统一工具面的最低条件。 |
| `turnmesh/internal/mcp/types.go` | `CapabilityProvider`、`Tool`、`CallRequest`、`CallResult` 已存在，但尚未与 executor 对接。 |
| `claude-code-source-code/src/services/mcp/client.ts` | TS 侧会把 MCP `tools/list` 的结果归一化为本地 `Tool`，而不是让 query loop 直接持有 transport 语义。 |
| `claude-code-source-code/src/services/mcp/mcpStringUtils.ts` | TS 侧用 `mcp__<server>__<tool>` 命名规则把 MCP 工具并入统一工具命名空间，并保留 server/tool 身份。 |
| `claude-code-source-code/src/services/tools/toolOrchestration.ts` | TS 主工具循环依赖统一工具集合和并发规则，而不是区分“本地工具路径”和“MCP 工具路径”。 |
| `claude-code-source-code/src/services/tools/StreamingToolExecutor.ts` | TS 的工具执行器对工具来源无感知，关注点是并发、安全、中断和结果顺序。 |

### 0.2 现有实现模式

- **依赖注入**：Go 侧目前以构造参数直接注入为主，入口层尚未形成 registry/assembly 级别的收口点。
- **错误处理**：Go 侧通过 `core.Error`、显式 error 返回和 turn event error 通道表达失败；MCP client 使用 request/response 关联并透传 transport 错误。
- **事务管理**：当前没有数据库事务；运行时状态主要由内存中的 turn loop、tool registry、session 实例和文件系统 side effect 组成。

### 0.3 潜在冲突点

- 当前 `executor` 的公共运行接口仍以 `CommandRequest` 为中心，这与 “MCP 作为统一工具面的一部分” 目标冲突。[需新增接口]
- 当前入口只有 `cmd/engine/main.go -> orchestrator.New(...)` 这一层，provider 选择、session 创建、tool registry 装配都没有稳定边界。[需新增接口]
- 当前 `internal/mcp` 只有 client/runtime，没有“把远端工具投影成本地统一 tool runtime”的适配层。[需新增接口]
- TS `ToolUseContext` 同时容纳 `tools`、`mcpClients`、permissions、UI 状态等；Go 不能用同样的大上下文复制边界污染。
- 如果直接让 provider 或 orchestrator 依赖 `mcp.Client`，会违反“单一主编排内核”和“执行层独立于模型层”的宪法约束。

---

## 1. 背景与问题

### 1.1 症状 A: 运行时入口仍是临时拼装
**位置**：`turnmesh/cmd/engine/main.go`
**现象**：入口只能手工创建 `orchestrator.Engine`，没有统一 runtime assembly。
**危害**：provider registry、tool registry、memory、MCP 生命周期会继续散落在各层，后续每加一个能力都要重新决定挂载点。

### 1.2 症状 B: Provider 已抽象，但还没有可治理的注册层
**位置**：`turnmesh/internal/model/provider.go`
**现象**：`Provider` 与 `Session` 已存在，但没有统一注册、枚举、默认选择和按名称构造 session 的机制。
**危害**：多模型目标只能停留在“接口上支持”，而不是运行时真正可装配。

### 1.3 症状 C: 执行层仍把“工具”近似等同于“命令”
**位置**：`turnmesh/internal/executor/types.go`，`tool_dispatcher.go`
**现象**：当前 tool runtime 必须消费 `CommandRequest`，dispatcher 也会先把调用解码成命令输入。
**危害**：MCP 工具、未来资源工具、结构化工具都难以作为一等公民进入统一工具面。

### 1.4 症状 D: MCP 运行时存在，但还没有进入主内核
**位置**：`turnmesh/internal/mcp/client.go`，`types.go`
**现象**：已有 `tools/list` 与 `tools/call`，但主 turn loop 仍无法把 MCP 当普通工具调用。
**危害**：当前的 provider-agnostic 设计还没有被统一工具面真正验证。

### 1.5 症状 E: TS 参考明确采用统一工具命名空间
**位置**：`claude-code-source-code/src/services/mcp/client.ts`，`mcpStringUtils.ts`
**现象**：TS 侧使用 `mcp__<server>__<tool>` 规则把 MCP 工具并入统一工具系统，并通过适配层而不是 query loop 直接调用 transport。
**危害**：如果 Go 不建立类似的边界，后续权限、日志、bridge、subagent 都会被迫同时兼容本地工具和 MCP 特例路径。

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
| **Runtime Assembly** | 把 provider、session、tool registry、feedback、memory 等运行时依赖收口成唯一装配入口的边界。 |
| **Provider Registry** | 负责 provider 注册、查找、枚举和基于配置创建 session 的运行时组件。 |
| **Unified Tool Surface** | 对 orchestrator 暴露的统一工具执行面，既能执行本地工具，也能执行 MCP 工具。 |
| **Tool Catalog** | 当前 runtime 可见的工具集合与其规范信息的归一化视图。 |
| **MCP Tool Adapter** | 把 MCP `tools/list` / `tools/call` 转换成统一工具系统中可注册、可调用能力的适配层。 |
| **Tool Namespace** | 用于唯一标识工具的命名规则；本 Spec 中包含 MCP 工具的 fully-qualified name 规则。 |

---

## 3. 需求条款

### R1. Runtime Assembly 与入口收口

| 编号 | 条款 |
|------|------|
| R1.1 | 系统 **MUST** 提供唯一合法的 runtime assembly 入口，用于构造 provider registry、tool runtime、session factory、feedback sink 和其他运行时依赖。[需新增接口] |
| R1.2 | `cmd/engine` **MUST** 通过 runtime assembly 获取可运行内核，而不是在入口层手工拼装 `orchestrator.Config` 的所有关键依赖。[需新增接口] |
| R1.3 | runtime assembly **MUST NOT** 把厂商特有配置、MCP transport 细节或 UI 状态泄漏到 `internal/core`、`internal/orchestrator`、`internal/executor`。[需新增接口] |
| R1.4 | runtime assembly **SHOULD** 允许最小配置启动默认 provider 和默认工具表，以支持无 UI 的本地验证。[需新增接口] |

### R2. Provider Registry 与会话装配

| 编号 | 条款 |
|------|------|
| R2.1 | 系统 **MUST** 提供 `ProviderRegistry` 抽象，支持按名称注册、查询、列举 provider。[需新增接口] |
| R2.2 | 系统 **MUST** 提供基于 provider 名称、model 名称和 session options 创建 `model.Session` 的统一入口，而不是让上层直接依赖具体 provider 构造函数。[需新增接口] |
| R2.3 | 新增或替换 provider 时，**MUST NOT** 需要修改 `internal/core`、`internal/orchestrator`、`internal/executor`、`internal/feedback` 的公共接口。 |
| R2.4 | `ProviderRegistry` **SHOULD** 支持枚举可用 provider 与其 model 信息，用于后续 bridge/CLI 消费。[需新增接口] |

### R3. Unified Tool Surface

| 编号 | 条款 |
|------|------|
| R3.1 | 系统 **MUST** 提供与 `CommandRequest` 解耦的统一工具运行抽象，使 tool runtime 可以承载本地命令工具与非命令工具。[需新增接口] |
| R3.2 | `orchestrator` **MUST** 继续只依赖统一的 tool dispatch 接口，而不感知工具来自本地还是 MCP。 |
| R3.3 | 系统 **MUST** 保留命令执行的取消、超时、cwd、env、stdout/stderr 语义，但这些语义 **MUST NOT** 成为所有工具的强制输入模型。 |
| R3.4 | Unified Tool Surface **SHOULD** 暴露 Tool Catalog 视图，以便 provider session 在构造时拿到当前可见工具规范。[需新增接口] |
| R3.5 | Tool Catalog **MUST NOT** 依赖 UI、权限对话框或 MCP transport 对象本身。 |

### R4. MCP Tool Bridge

| 编号 | 条款 |
|------|------|
| R4.1 | 系统 **MUST** 提供 `MCPToolAdapter` 或等价抽象，把 MCP `tools/list` 发现到的远端工具转为 Unified Tool Surface 中可注册、可调用的工具能力。[需新增接口] |
| R4.2 | `internal/mcp` **MUST** 继续只承担 transport、client、capability discovery、call 语义；它 **MUST NOT** 成为第二套主编排入口。 |
| R4.3 | MCP 工具调用 **MUST** 通过 executor/tool runtime 进入 orchestrator 的现有 `tool_call -> tool_result -> continue` 闭环。 |
| R4.4 | MCP 工具的错误、超时、server error 和 transport error **MUST** 被归一化为统一 `core.ToolResult` / `core.Error` 语义。 |
| R4.5 | 系统 **SHOULD** 允许一个 MCP server 暴露多个工具并批量注册到统一工具表。[需新增接口] |

### R5. Tool Namespace 与标识规则

| 编号 | 条款 |
|------|------|
| R5.1 | 系统 **MUST** 为 MCP 工具建立 fully-qualified tool name 规则，等价于 TS 侧的 `mcp__<server>__<tool>` 语义，以避免与内建工具重名。[需新增接口] |
| R5.2 | 系统 **MUST** 同时保留 MCP 工具的原始 server name 与 tool name 元数据，用于日志、权限和后续 bridge。[需新增接口] |
| R5.3 | Tool Namespace **MUST NOT** 让 provider session、orchestrator 或 feedback 直接依赖 MCP transport 标识细节。 |
| R5.4 | 系统 **SHOULD** 提供 tool name 的归一化/解析辅助函数，以便后续资源、prompt、权限规则复用。[需新增接口] |

### R6. Runtime 可观测性与验证

| 编号 | 条款 |
|------|------|
| R6.1 | 系统 **MUST** 保持 `go build ./...` 可通过。 |
| R6.2 | 系统 **MUST** 保持 `go test ./...` 可通过。 |
| R6.3 | 系统 **MUST** 通过最小单元测试或集成测试验证 provider registry、runtime assembly、MCP tool bridge 和 unified tool dispatch 的关键行为。[需新增接口] |
| R6.4 | 运行时新增抽象 **MUST NOT** 破坏现有 orchestrator turn loop 的公开行为：`tool_call -> tool_result -> continue` 仍需成立。 |

---

## 4. 决策规则

```text
IF 某能力需要让 orchestrator 判断“这是本地工具还是 MCP 工具”
THEN 该设计不满足 Unified Tool Surface

IF 某能力要求 provider 直接持有 mcp.Client 或 transport
THEN 该设计不满足执行层独立原则

IF 某工具名称无法在全局命名空间中唯一标识
THEN 该工具不满足 Tool Namespace 约束

IF 新增第二家 provider 需要修改 orchestrator/executor/core 的公共接口
THEN 该设计不满足多模型装配约束
```

---

## 5. 验收条件（Acceptance Criteria）

### AC1. 运行时代码可构建

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && go build ./...
```

**预期结果**：退出码为 `0`

### AC2. 全量测试通过

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && go test ./...
```

**预期结果**：退出码为 `0`

### AC3. Provider Registry 抽象存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "type (ProviderRegistry|Registry) (struct|interface)" internal
```

**预期结果**：至少命中 `1` 处可用于 provider 注册/查找的定义

### AC4. Runtime Assembly 入口存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "(type .*Assembly|func New.*Runtime|func New.*Kernel|func Build.*Runtime)" internal cmd
```

**预期结果**：至少命中 `1` 处运行时装配入口定义

### AC5. 统一工具运行抽象不再只接受 CommandRequest

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "Execute\\(ctx context.Context, name string, request CommandRequest\\)|Execute\\(ctx context.Context, request CommandRequest\\)" internal/executor
```

**预期结果**：命中数 **少于** 当前基线；且存在新的通用工具执行抽象定义

### AC6. MCP Tool Adapter 或等价桥接存在

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "MCPTool|McpTool|mcp__|BuildMCP|BuildMcp|NormalizeMCP|NormalizeMcp" internal
```

**预期结果**：至少命中 `2` 处与 MCP 工具命名/适配/桥接相关的实现定义

### AC7. Unified Tool Surface 被 orchestrator 闭环消费

```bash
cd /Users/jayleonc/Developer/ts/turnmesh && rg -n "ExecuteTool\\(" internal/orchestrator internal/executor
```

**预期结果**：命中现有 orchestrator 调度路径，且测试覆盖本地工具与 MCP 工具两类场景

---

## 6. 实施后核验问询（Post-Implementation Interrogation）

1. 当前 runtime 是否已经存在唯一合法装配入口，而不是让 `cmd/engine` 继续手工拼装核心依赖？
2. 如果新增第三家 provider，是否只需要注册 provider，而不需要改 `orchestrator` 与 `executor` 公共接口？
3. orchestrator 是否已经完全不需要知道某个工具来自本地命令还是 MCP server？
4. MCP server 生命周期是否被收敛在 runtime / adapter 层，而不是泄漏给 provider 或 turn loop？
5. 当前工具命名空间是否已经能稳定区分内建工具与来自不同 MCP server 的同名工具？
6. 当前实现是否已经通过测试证明：同一条 `tool_call -> tool_result -> continue` 路径既能跑本地工具，也能跑 MCP 工具？
