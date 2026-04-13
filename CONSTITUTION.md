# CONSTITUTION.md

本文件定义 `turnmesh/` 的仓库级稳定原则。

它不是路线图，也不是临时任务单。
它回答的是：无论后面谁来接手，这个项目有哪些原则不能轻易违背。

## 1. 作用

这个文件用于给后续 agent 和人类协作者提供一组高于实现细节的判断准则。

用途：

- 在多种实现方案之间给出裁决标准
- 在局部改动和全局一致性冲突时提供优先级
- 在新窗口或长时间中断后，防止项目方向漂移

## 2. 优先级

在本工作区内，默认优先级如下：

1. 当前会话中的系统 / 开发者 / 用户明确指令
2. `CONSTITUTION.md`
3. `AGENTS.md`
4. `docs/kernel-bootstrap/spec.md`
5. `DEV_LOG.md`
6. `ROADMAP.md`

说明：

- `CONSTITUTION.md` 是稳定原则，不替代会话级明确要求
- `DEV_LOG.md` 是事实快照，不是原则来源
- `ROADMAP.md` 是路线，不是硬法

## 3. 不可违背的原则

### 3.1 核心协议必须厂商中立

以下包不得泄漏任何单一厂商语义：

- `internal/core`
- `internal/orchestrator`
- `internal/executor`
- `internal/memory`
- `internal/feedback`

结论：

- 厂商字段、命名、请求/响应形状只能存在于 `internal/model/<provider>`

### 3.2 只能有一套主编排内核

整个项目只能有一套主 turn loop。

结论：

- `subagent` 不能复制第二套 orchestrator
- `MCP` 不能绕开 orchestrator 形成旁路主流程
- provider 不能把自己的特殊 loop 藏在 adapter 里替代主循环

### 3.3 执行层必须独立于模型层

工具执行、命令执行、超时、取消、cwd、sandbox 是执行层职责。

结论：

- provider 不直接执行命令
- orchestrator 不直接实现命令细节
- MCP 接入后也必须通过统一工具表进入执行面

### 3.4 事件模型优先于 UI 和展示

任何外部展示都只能消费事件，不能反过来定义核心状态。

结论：

- 不允许先做 TUI/TS bridge 再倒推状态机
- `feedback` 只负责消费和转发事件，不承载业务闭环

### 3.5 记忆是策略系统，不是补丁

记忆、compact、retrieval 不得作为 provider 私有技巧或 UI 侧补丁存在。

结论：

- 记忆必须保留独立边界
- compact 必须保持可插拔
- 不允许把“先忽略，未来再说”变成永远散落在各层的隐式逻辑

### 3.6 所有关键能力都要能被验证

只要某个能力被视为“当前基线的一部分”，就必须能被构建或测试验证。

结论：

- 当前基线至少保持 `go test ./...` 和 `go build ./...` 通过
- 如果新增关键模块但没有任何验证入口，默认视为未完成

## 4. 设计取舍规则

遇到方案冲突时，按下面顺序取舍：

1. 保持核心边界清晰
2. 保持单一主编排内核
3. 保持多模型可接入
4. 保持执行语义稳定
5. 保持实现可测试
6. 最后才考虑局部实现便利性

## 5. 对后续 agent 的硬要求

开始任何 substantial 工作前，必须先读：

1. `CONSTITUTION.md`
2. `DEV_LOG.md`
3. `AGENTS.md`

开始改动前，必须回答清楚：

- 这次改动会不会污染核心边界
- 这次改动会不会引入第二套主流程
- 这次改动会不会让 `MCP` 或 `subagent` 变成特例系统
- 这次改动如何验证

## 6. 什么时候修改本文件

只有在以下情况才应该改：

- 仓库级硬原则改变
- 现有原则互相冲突
- 新增了足以影响整个项目方向的全局约束

不应该因为下面这些原因修改：

- 某一轮临时实现不方便
- 某个局部模块想走捷径
- 某个 provider 的协议更容易直接照搬

## 7. 当前仓库的宪法性结论

截至 2026-04-13，本仓库的稳定结论是：

- Go kernel 已经有一版 bootstrap baseline
- `OpenAI` 与 `Anthropic` 都是 adapter，不是内核
- `MCP` 已有最小 runtime，但还未正式接入统一工具表
- `subagent` 还未开始，不允许为它复制新的 orchestrator
- 近期默认优先级最高的工程动作是：
  把 `internal/mcp` 接入 `internal/executor`，验证统一工具面
