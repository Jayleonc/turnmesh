# Subagent Example

这个例子演示最小 subagent 生命周期：

- 定义一个 `agent.Definition`
- 用 `agent.NewAgentRuntime(...)` 创建 runtime
- 调 `Start(...)` 启动 task
- 从事件通道读取 `started`、`status_changed`、`progress`、`message`、`completed`
- 用 `GetTask(...)` 读取最终状态

运行：

```bash
go run ./examples/subagent
```

这个示例没有调用真实模型，也没有接 tool runtime。它的目的不是模拟完整 kernel，而是先把 subagent 的状态机和事件流讲清楚。

下一步适合补 `subagent-kernel` 示例：让 runner 内部复用 `orchestrator.Engine`、provider registry、tool runtime 和 memory runtime。

