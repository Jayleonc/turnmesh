# One-Shot API

## 目标

`RunOneShot(...)` 面向“只需要一次模型调用”的场景。

它解决的问题不是多轮 agent 编排，而是：

- query rewrite
- classify / route
- summarize
- 从上下文里抽取检索关键词

如果你需要 tool call -> tool result -> continue 的闭环，请用 `New(...).RunTurn(...)`。

## 最小用法

```go
result, err := turnmesh.RunOneShot(ctx, turnmesh.Config{
	Provider: "openai-chatcompat",
	Model:    "gpt-4.1-mini",
	BaseURL:  "https://api.openai.com/v1",
	APIKey:   os.Getenv("OPENAI_API_KEY"),
}, turnmesh.OneShotRequest{
	SystemPrompt: "Rewrite vague support questions into KB search queries.",
	Messages: []turnmesh.Message{
		{Role: turnmesh.RoleUser, Content: "这个怎么开"},
	},
})
if err != nil {
	return err
}

fmt.Println(result.Text)
```

## 输入

- `Config`
  复用和 `RunTurn(...)` 同一套 provider 配置。
- `OneShotRequest.SystemPrompt`
  本次调用的 system prompt。
- `OneShotRequest.Messages`
  输入消息列表。
- `OneShotRequest.Metadata`
  透传到底层 provider request 的 metadata。

## 输出

- `OneShotResult.Text`
  第一条 assistant 文本消息。
- `OneShotResult.Message`
  第一条 assistant 公共消息结构。
- `OneShotResult.Events`
  底层 provider 事件流的公开映射。
- `OneShotResult.Status`
  本次调用结束状态。

## 语义边界

- `RunOneShot(...)` 不负责 tool execution。
- 即使 `Config.Tools` 非空，one-shot 也不会把工具声明发给模型。
- 如果底层 provider 仍返回了 tool call，`RunOneShot(...)` 会直接返回错误，提示调用方改用 `RunTurn(...)`。

## 何时用它

用 `RunOneShot(...)`：

- 只想复用 provider/session 边界
- 不需要工具
- 不需要多轮 continuation

用 `RunTurn(...)`：

- 需要工具调用
- 需要多轮模型继续
- 需要统一收集 tool result / turn event
