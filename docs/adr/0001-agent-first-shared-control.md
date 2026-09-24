---
status: accepted
---

# Agent 优先的共享管理能力

产品首先服务 Agent：以自描述 CLI 加配套 SKILL 暴露管理能力，UI 通过同一服务完成查看与调整。生成接口保持 Chat Completions 格式，管理操作采用 AgentUse 风格的结构化契约；两者分开，避免为适配管理协议破坏现有模型客户端。

相比先做 UI 再补自动化接口，这要求能力发现、认证、副作用、幂等性和结果语义先于页面实现。相比为 CLI/UI 各写一套逻辑，两种入口共享鉴权、配置变更和校验；SKILL 引用运行时 schema，避免维护另一份命令契约。

参考：[AgentUse 协议](https://www.zerone.run/zh/protocol)。采用其设计原则不构成认证声明。
