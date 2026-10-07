# HTTP 入口

分别承载 Chat Completions、Anthropic Messages 推理入口和 CLI/UI 共用的管理 API。推理响应保持各自 JSON/SSE 格式，管理响应采用结构化信封；认证与 scopes 明确区分。

负责请求解码、响应编码、工具调用增量、usage、错误与取消传播。共享 inference 服务负责一次快照、规划计时和路由记录；路由和管理业务交给 routing/management。Bifrost 协议转换封装在 provider。Messages 路径在认证之前选择其独立错误编码，始终使用 Router 请求 ID；上游错误体不直接透传。
