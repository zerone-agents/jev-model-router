# 首版兼容验证

2026-09-28：固定 Bifrost Core `v1.10.2-0.20260924033752-a8411cd54142`，Go 1.27.0；SQLite 使用纯 Go 驱动 modernc.org/sqlite v1.39.1，不要求 CGO（race 测试仍需要受支持的本机构建环境）。依赖锁在 go.mod/go.sum。

`go test ./internal/compat -count=1 -v` 使用本地模拟上游验证以下边界，没有调用付费模型，不证明任意远程服务都兼容。

| 输入/行为 | 结果与承诺 |
| --- | --- |
| 文本、图片 URL/内联 | 字段保留；图片 URL 服务零访问，SDK 不下载图片 |
| tools、required tool_choice、parallel_tool_calls=false | 在已测 OpenAI/gpt-4o-mini 转换路径保留；其他模型仍需逐请求验证转换是否有损 |
| temperature、top_p、stop、json_object、stream_options.include_usage | 已测转换保留，生产入口仍拒绝未知字段 |
| max_completion_tokens >=16 | 已测转换保留 |
| max_completion_tokens <16 | SDK 会提高到 16；生产必须明确拒绝该组合，固定连接测试使用 16 |
| 429、503、SSE 503、200 SSE error、中途断流 | 选定上游仅一次，其他已配置上游零次；错误可观察 |
| 签名/加密推理扩展、fallback、供应商覆盖 | 不在首版输入范围，生产入口必须拒绝 |

Jev 原生协议依据 [API reference](https://docs.typesafe.ai/api)：state/questions，choice 最多 255 选项，响应为选择和概率/置信度，没有自由文本理由。fixtures 是文档形状的合成样例，非真实 API 结果。输入上限依据 [Models](https://docs.typesafe.ai/models)，上线前必须按配置模型复核；没有精确 tokenizer 证据时只提供有余量估算，不保证上游接受。

生成参数需同时满足公布的 schema、模型能力声明和实际适配转换检查。此文件的 SDK 转换测试不是模型本身能力证明。端到端取消、首包/空闲超时、客户端背压及实际模型质量由后续任务验证。
