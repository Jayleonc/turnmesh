# ai-customer 接入说明

## 目标

让 `ai-customer` 不再自己维护一套 OpenAI-compatible `/chat/completions` agent loop，而是把 runtime 下沉到 `turnmesh`。

这次接入的核心原则很简单：

- `ai-customer` 保留业务壳
- `turnmesh` 接管运行时

## 保留在 ai-customer 的内容

- 企微回调、消息归属、群聊并发去重、pending queue
- `SystemPrompt` 生成
- `preSearch`
- `query rewrite`
- 客服知识库工具实现
- 会话和消息落库

这些都是业务特有逻辑，不应该沉到 runtime kernel 里。

## 下沉到 turnmesh 的内容

- provider session
- model turn loop
- tool call dispatch
- tool result continuation
- 多轮 tool loop
- 统一事件语义

这些是 runtime 的公共问题，不应该每个业务仓库自己再写一套。

## 实际接法

`ai-customer/internal/agent/service.go` 现在改成：

1. 继续用本地 store 拼好历史消息
2. 继续执行 `preSearch` 和 `query rewrite`
3. 把最终消息列表转换成 `turnmesh.Message`
4. 把 `search_knowledge`、`read_document`、`check_feature_tag`、`ask_clarification` 注入成 `turnmesh.Tool`
5. 调 `turnmesh.New(...).RunTurn(...)`
6. 从 `TurnResult` 中拿最终回复文本

## 这次新增的 turnmesh 能力

为了让 `ai-customer` 能直接接进来，`turnmesh` 补了两块最关键的公开能力：

- 根包 facade：外部仓库可以直接 `import github.com/Jayleonc/turnmesh`
- `openai-chatcompat` provider：兼容 OpenAI-compatible `/chat/completions`

这样 `ai-customer` 不需要 import `turnmesh/internal/*`，也不需要自己再手写一套 HTTP loop。

## 兼容性说明

当前 `ai-customer` 仍保留原来的：

- token budget 裁剪
- 降级到预检索结果
- `ask_clarification` 的特殊输出约定

所以这次是“把 runtime 换掉”，不是“把业务行为全改掉”。

## 后续建议

- 把 `preSearch` / `rewrite` 的抽象继续上提，变成 turnmesh 的 `Preparer` 能力
- 给 `ask_clarification` 做正式事件，而不是继续依赖字符串约定
- 给 `ai-customer` 增加一个开关，支持旧 loop 和 turnmesh loop 并行对比
