# 首版兼容验证

2026-09-28：固定 Bifrost Core `v1.10.2-0.20260924033752-a8411cd54142`，Go 1.27.0；SQLite 使用纯 Go 驱动 modernc.org/sqlite v1.39.1，不要求 CGO（race 测试仍需要受支持的本机构建环境）。依赖锁在 go.mod/go.sum。

`go test ./internal/compat -count=1 -v` 使用本地模拟上游验证以下边界，没有调用付费模型，不证明任意远程服务都兼容。

| 输入/行为 | 结果与承诺 |
| --- | --- |
| 文本、图片 URL/内联 | 字段保留；图片 URL 服务零访问，SDK 不下载图片 |
| tools、required tool_choice、parallel_tool_calls=false | 在已测 OpenAI/gpt-4o-mini 转换路径保留；其他模型仍需逐请求验证转换是否有损 |
| temperature、top_p、stop、json_object、stream_options.include_usage | 已测转换保留，生产入口仍拒绝未知字段 |
| max_completion_tokens / max_tokens，整数 16–10000000 | 入口归一化为 max_completion_tokens，已测 JSON/SSE 转换保留 |
| 输出限制 <16 | Bifrost 会提高到 16；生产入口明确拒绝，固定连接测试使用 16 |
| 429、503、SSE 503、200 SSE error、中途断流 | 选定上游仅一次，其他已配置上游零次；错误可观察 |
| 签名/加密推理扩展、fallback、供应商覆盖 | 不在首版输入范围，生产入口必须拒绝 |

Jev 原生协议依据 [API reference](https://docs.typesafe.ai/api)：state/questions，choice 最多 255 选项，响应为选择和概率/置信度，没有自由文本理由。fixtures 是文档形状的合成样例，非真实 API 结果。输入上限依据 [Models](https://docs.typesafe.ai/models)，上线前必须按配置模型复核；没有精确 tokenizer 证据时只提供有余量估算，不保证上游接受。

生成参数需同时满足公布的 schema、模型能力声明和实际适配转换检查。此文件的 SDK 转换测试不是模型本身能力证明。本地测试已覆盖端到端取消、首包/空闲超时和有界事件缓冲；真实慢读 socket 的完整跨 SDK 压力场景仍待补充，实际模型质量需另行付费评测。

Jev 决策默认限制为 255 个候选、24,000 UTF-8 序列化字节（包含问题和选项），这是有余量的本地估算边界，不是精确 token 计数。保留全部候选、偏好、system/developer 指令、工具定义和最新 user 回合；较旧回合按完整组从近到远加入，溢出时标记省略。图片结构替换为 image_present，不保留 URL 或内联数据。必需部分超限直接失败，生成请求保持完整。

生成容量预检使用 UTF-8 字节数、每张图片额外 8192 估算单位、256 协议余量，以及请求输出限额（未指定时预留 4096）。`context_exact=false` 明确表示估算；图片实际 token 依模型和尺寸而异，上游仍可能拒绝。该预检不会裁剪或修改输入。

普通 `response_format.type=text` 不要求结构化输出能力，JSON 模式才要求。Jev 的必需输入同时包含输出格式/JSON Schema、tool_choice、parallel_tool_calls 和 max_completion_tokens，这些需求也占用决策预算。

SSE usage 按上游实际提供情况返回：未报告时省略，明确报告为零则保留。适配器仅在内存中检查 SDK 提供的原始帧以识别 usage 存在性，公开响应和记录不包含 SDK 原始元数据。空库或没有启用模型时 auto 返回 config_missing/503；启用模型均不满足当前请求时返回 no_candidates/422。

历史 assistant 工具调用允许 content:null 与省略 content 的等价表示；适配器附加的历史 tool_calls.index 不影响语义比较，非流式工具调用响应不输出该索引。流式 delta 保留 index；调用ID、函数名、参数字符串、消息顺序和工具结果仍必须完整保留。完整工具回合由跨 Bifrost/HTTP 的端到端测试覆盖。

## 真实端点冒烟验证

已使用官方 Jev `jev-1.13.0` 和阿里云 `qwen3.8-flash` / `qwen3.8-max` 验证多候选选模到生成及记录查询的闭环。两个生成模型均验证了 JSON、SSE、`tool_choice=auto` 工具调用和省略顶层 tools 的工具历史续写。供应商 base_url 包含完整兼容前缀，适配器仅追加 `/chat/completions`，JSON 与 SSE 路径均有严格回归测试。

本次阿里云 thinking 模式组合拒绝 `tool_choice=required`；网关明确返回失败，不静默修改参数。生成输出可能因 token 上限返回 `finish_reason=length`，HTTP 成功不代表任务质量通过。以上只是已测组合的冒烟结果，不承诺全部模型、字段组合或稳定性能；真实图片、流式工具增量及供应商取消/超时仍需专项验证。

## agent-sdk 输出限制兼容（Issue #18）

Chat Completions 与 `route.inspect` 接受 `max_tokens` 或 `max_completion_tokens`，二者只能出现一个，即使值相同或其中一个为 null 也拒绝。内部容量估算、Jev 决策输入和生成适配统一使用 `max_completion_tokens`，不会钳制或丢弃值。自动与显式选模均执行相同限制。

冲突返回 HTTP 400 / `invalid_request`，消息说明两个字段互斥；范围外、null、字符串、布尔、数组、对象和非整数值返回同一错误码，消息指出字段及整数范围 16–10000000。其他未支持字段仍返回 `unsupported_request`；schema 错误尽可能指出顶层字段，不回显字段值、对话内容或 schema 原始错误。异常长度或字符的未知字段名使用通用提示。

`go test ./internal/transport/http -run TestSDKOutputLimitThroughRouter -v` 使用生产 HTTP、路由和 Bifrost 路径加本地模拟生成端点，覆盖两种字段、两种选模路径、JSON/SSE、输出限制保留及无效请求不外发。路由测试另覆盖上下文预算、上下界和冲突。

2026-09-30 另用本地未经修改的 agent-sdk 4.0.0 `OpenAIProvider.createMessage` / `createMessageStream` 调用更新后的本地 Router，自动和显式选模的四次调用通过；未包装 fetch 或改写 SDK 请求。该项使用模拟生成端点，不代表部署环境或真实模型兼容验收。复现方式（SDK checkout 需已有 tsx）：

```sh
JEV_TEST_AGENT_SDK=/absolute/path/to/agent-sdk go test ./internal/transport/http -run TestUnmodifiedAgentSDK -count=1 -v
```

未设置变量时此 SDK 集成测试明确跳过，常规 Go 回归仍执行。`reasoning_effort`、`chat_template_kwargs` 与空 tools 数组未因本修复而获得支持。
