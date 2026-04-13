# Multi-Model Routing & Task Delegation (Concept)

## 1. 核心想法 (Core Idea)

系统支持同时配置多个模型（Provider），包括远程高性能模型（如 Claude 3.5, GPT-4o）和本地轻量级模型（如 Llama 3, Qwen, DeepSeek）。
通过一种 **"Routing Policy" (路由策略)**，根据任务的类型、复杂度、成本和隐私要求，动态调度最合适的模型。

## 2. 典型应用场景 (Use Cases)

- **Git 操作 (Local Preferred):**
    - `git status`, `git diff` 摘要生成, `git commit` 消息生成。
    - 优势：低延迟、零成本、高隐私、任务确定性强。
- **文件扫描与预处理 (Local Preferred):**
    - 在大规模代码库中进行 `grep` 或语义搜索筛选。
    - 优势：处理大量本地文件时无网络开销。
- **架构设计与复杂推理 (High-End Remote Preferred):**
    - 整体方案设计、跨文件逻辑重构、复杂 Bug 调试。
    - 优势：极强的逻辑推理和长上下文处理能力。
- **单元测试与 Boilerplate 生成 (Local/Medium Preferred):**
    - 编写标准化的测试代码或重复性的模板代码。
    - 优势：节省昂贵的 API Token。

## 3. 技术实现要点 (Key Requirements)

- **Capabilities Metadata:** 为每个 Provider 增加能力标签（如 `reasoning: high`, `git_expert: true`, `cost: zero`）。
- **Adaptive Orchestrator:** 升级编排层，支持在同一个 Session 中根据当前 `ToolInvocation` 的类型动态切换 Provider。
- **Context Delegation:**
    - **Main Agent (Brain):** 负责任务拆解。
    - **Sub-Worker (Hands):** 负责执行具体工具。
    - **Summarization:** 本地模型执行完后，将结果摘要（而非全量输出）反馈给主模型。
- **Runtime Overlay:** 允许在执行特定任务时，临时覆盖当前的模型配置。

## 4. 演进路线建议 (Roadmap Suggestions)

1.  **静态路由 (Static Routing):** 预定义某些工具（如 `git_*`）强制路由到指定的本地模型。
2.  **动态决策 (Dynamic Routing):** 主模型根据任务描述，显式决定调用哪个 Sub-Agent（对应不同模型）。
3.  **自适应路由 (Adaptive Routing):** 内核根据历史执行效果和成本预算，自动优化模型选择。

---
*记录时间: 2026-04-14*
*来源: 用户构思与架构讨论*
