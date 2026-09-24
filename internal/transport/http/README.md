# HTTP 入口

分别承载 Chat Completions 推理入口和 CLI/UI 共用的管理 API。推理响应保持兼容 JSON/SSE，管理响应采用结构化信封；认证与 scopes 明确区分。

负责请求解码、响应编码、工具调用增量、usage、错误与取消传播。路由和管理业务分别交给 routing/management；不把 SDK 内部结果直接写给客户端。当前尚未提供 handler。
