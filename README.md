# Turnmesh

`turnmesh` 是一个用 Go 写的 agent runtime kernel。

它解决的不是“怎么做一个聊天产品”，而是“怎么把 model、tool、memory、MCP、agent task 放进同一套可嵌入运行时里”。如果你在做客服助手、知识库助手、Copilot、workflow agent，`turnmesh` 负责底层 turn loop 和 tool loop，你的业务系统只保留自己的数据、策略和交互壳。

## 它是什么

- 一个可嵌入的 Go agent runtime
- 一套单一主 turn loop
- 一个统一的 tool execution surface
- 一个厂商中立的 provider adapter 边界
- 一个可以继续扩展 memory、MCP、agent runtime 的内核

当前内置 provider：

- `openai`
- `anthropic`
- `openai-chatcompat`

其中 `openai-chatcompat` 专门面向 OpenAI-compatible `/chat/completions` 接口，适合接现有业务系统。

## 它不是什么

- 不是面向终端用户的聊天产品
- 不是工作流画布平台
- 不是多租户后台
- 不是“Claude Code 的逐文件 Go 翻译”

更准确地说，`turnmesh` 是产品下面那层 runtime kernel，不是产品本身。

## 核心概念

- `TurnInput -> TurnEvent`
  一次请求进来，运行时产出一条事件流，模型消息、tool call、tool result、错误都在同一条流里。
- `Provider -> Session`
  模型厂商只通过 adapter 接进来，内核不直接绑定某一家 SDK。
- `Tool`
  业务工具通过统一 schema 和 handler 注入，不需要在每个业务仓库里再重复写一套 tool loop。
- `Memory / MCP / Agent`
  这些能力继续沿同一套 runtime 扩展，而不是各自长出第二套主流程。

## 快速开始

```go
package main

import (
	"context"
	"fmt"

	"github.com/Jayleonc/turnmesh"
)

func main() {
	ctx := context.Background()

	runtime, err := turnmesh.New(ctx, turnmesh.Config{
		Provider: "openai-chatcompat",
		Model:    "gpt-4.1-mini",
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "your-api-key",
		Tools: []turnmesh.Tool{
			{
				Name:        "lookup_order",
				Description: "lookup one order by id",
				InputSchema: turnmesh.MustJSONSchema(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"order_id": map[string]any{"type": "string"},
					},
					"required": []string{"order_id"},
				}),
				Handler: func(ctx context.Context, call turnmesh.ToolCall) (turnmesh.ToolOutcome, error) {
					return turnmesh.ToolOutcome{
						Output: `{"status":"paid"}`,
						Status: turnmesh.ToolSucceeded,
					}, nil
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	defer runtime.Close()

	result, err := runtime.RunTurn(ctx, turnmesh.TurnRequest{
		Messages: []turnmesh.Message{
			{Role: turnmesh.RoleSystem, Content: "You are a support agent."},
			{Role: turnmesh.RoleUser, Content: "帮我查一下订单 A123"},
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Text)
}
```

## 对外 API

当前公开且建议直接依赖的是根包：

- `github.com/Jayleonc/turnmesh`

这里已经提供：

- `New`
- `RunTurn`
- `StreamTurn`
- `Message`
- `Tool`
- `ToolCall`
- `ToolOutcome`
- `TurnRequest`
- `TurnResult`

`internal/*` 仍然是实现层，不承诺对外稳定。

## ai-customer 接入

这次已经把 `ai-customer` 的手写 `/chat/completions` loop 改成了走 `turnmesh`：

- `ai-customer` 保留业务壳
  包括企微消息处理、会话归属、群上下文、客服工具、预检索和 query rewrite。
- `turnmesh` 接管 runtime
  包括 model 调用、tool call 调度、tool result 回填和多轮 tool loop。

详细方案见 [docs/ai-customer-integration.md](./docs/ai-customer-integration.md)。

## 仓库结构

- `turnmesh.go`
  当前公开 facade，外部仓库从这里接入。
- `internal/core`
  核心协议和状态模型。
- `internal/orchestrator`
  单一主 turn loop。
- `internal/executor`
  统一工具执行面。
- `internal/model/*`
  provider adapters。
- `internal/memory`
  memory runtime 与 store/policy 边界。
- `internal/mcp`
  最小 MCP runtime。
- `internal/agent`
  agent task runtime。
- `cmd/engine`
  参考装配入口和本地验证入口。
- `docs/*`
  规格、研究和设计记录。

## 当前状态

当前基线已经能通过：

```bash
go test ./...
go build ./...
```

这说明 `turnmesh` 已经不是空架子，而是一版可运行的 Go runtime baseline。

## 下一步

- 把 memory policy / compact policy 做成正式策略层
- 把更多 MCP resource / prompt 能力桥接进统一工具面
- 补更多对外稳定 API，而不是要求外部项目碰 `internal/*`
- 增加 examples，尤其是业务应用接入示例
