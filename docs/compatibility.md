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

Jev 决策默认限制为 255 个候选、32,000 UTF-8 序列化字节（包含问题和选项），可通过 decision_max_bytes / JEV_ROUTER_DECISION_MAX_BYTES 修改。默认值参照官方单问题 32k token 上限采用保守字节估算；官方未提供字节推荐值，这不是精确 token 计数，详见 [启动配置](configuration.md#jev-决策输入预算)。保留全部候选、偏好、system/developer 指令和最新 user query；较旧回合按完整组从近到远加入，溢出时标记省略。Jev 不接收工具定义，工具执行仅保留调用 ID、工具名称和结果关联记录，不保留参数、输出及执行消息附带文本。图片结构替换为 image_present，不保留 URL 或内联数据。必需部分超限直接失败，生成请求保持完整。

生成容量预检使用 UTF-8 字节数、每张图片额外 8192 估算单位、256 协议余量，以及请求输出限额（未指定时预留 4096）。`context_exact=false` 明确表示估算；图片实际 token 依模型和尺寸而异，上游仍可能拒绝。该预检不会裁剪或修改输入。

普通 `response_format.type=text` 不要求结构化输出能力，JSON 模式才要求。Jev 的必需输入同时包含输出格式/JSON Schema、tool_choice、parallel_tool_calls 和 max_completion_tokens，这些需求也占用决策预算。

SSE usage 按上游实际提供情况返回：未报告时省略，明确报告为零则保留。适配器仅在内存中检查 SDK 提供的原始帧以识别 usage 存在性，公开响应和记录不包含 SDK 原始元数据。空库或没有启用模型时 auto 返回 config_missing/503；启用模型均不满足当前请求时返回 no_candidates/422。

历史 assistant 工具调用允许 content:null 与省略 content 的等价表示；适配器附加的历史 tool_calls.index 不影响语义比较，非流式工具调用响应不输出该索引。流式 delta 保留 index；调用ID、函数名、参数字符串、消息顺序和工具结果仍必须完整保留。完整工具回合由跨 Bifrost/HTTP 的端到端测试覆盖。

## 真实端点冒烟验证

已使用官方 Jev `jev-1.13.0` 和阿里云 `qwen3.8-flash` / `qwen3.8-max` 验证多候选选模到生成及记录查询的闭环。两个生成模型均验证了 JSON、SSE、`tool_choice=auto` 工具调用和省略顶层 tools 的工具历史续写。供应商 base_url 包含完整兼容前缀，适配器仅追加 `/chat/completions`，JSON 与 SSE 路径均有严格回归测试。

本次阿里云 thinking 模式组合拒绝 `tool_choice=required`；网关明确返回失败，不静默修改参数。生成输出可能因 token 上限返回 `finish_reason=length`，HTTP 成功不代表任务质量通过。以上只是已测组合的冒烟结果，不承诺全部模型、字段组合或稳定性能；真实图片、流式工具增量及供应商取消/超时仍需专项验证。

## agent-sdk 输出限制兼容（Issue #18）

Chat Completions 与 `route.inspect` 接受 `max_tokens` 或 `max_completion_tokens`，二者只能出现一个，即使值相同或其中一个为 null 也拒绝。内部容量估算、Jev 决策输入和生成适配统一使用 `max_completion_tokens`，不会钳制或丢弃值。自动与显式选模均执行相同限制。

Chat Completions 与 `route.inspect` 共享以下字段校验与诊断：冲突返回 HTTP 400 / `invalid_request`，消息说明两个字段互斥；范围外、null、字符串、布尔、数组、对象和非整数值返回同一错误码，消息指出字段及整数范围 16–10000000。其他未支持字段仍返回 `unsupported_request`；schema 错误尽可能指出顶层字段，不回显字段值、对话内容或 schema 原始错误。异常长度或字符的未知字段名使用通用提示。

`go test ./internal/transport/http -run TestSDKOutputLimitThroughRouter -v` 使用生产 HTTP、路由和 Bifrost 路径加本地模拟生成端点，覆盖两种字段、两种选模路径、JSON/SSE、输出限制保留及无效请求不外发。路由测试另覆盖上下文预算、上下界和冲突。

2026-09-30 另用本地未经修改的 agent-sdk 4.0.0 `OpenAIProvider.createMessage` / `createMessageStream` 调用更新后的本地 Router，自动和显式选模的四次调用通过；未包装 fetch 或改写 SDK 请求。该项使用模拟生成端点，不代表部署环境或真实模型兼容验收。复现方式（SDK checkout 需已有 tsx）：

```sh
JEV_TEST_AGENT_SDK=/absolute/path/to/agent-sdk go test ./internal/transport/http -run TestUnmodifiedAgentSDK -count=1 -v
```

未设置变量时此 SDK 集成测试明确跳过，常规 Go 回归仍执行。该次 #18 修复没有扩大推理参数或空 tools 数组的支持范围；推理参数的后续支持见下节。

## agent-sdk 推理参数兼容（Issue #22）

Chat Completions 和 `route.inspect` 接受以下显式控制：

- `chat_template_kwargs` 只允许必填的布尔字段 `enable_thinking`；支持 true 与 false，不接受空对象、null 或其他模板参数。
- `reasoning_effort` 接受 `none`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max`。这是入口词汇范围，不代表所有模型支持这些值。
- SDK 的 `thinking: {type: "enabled"}` 对应上述 `chat_template_kwargs`；入口不接收 SDK 内部的顶层 `thinking` 对象。

合法的 thinking / effort 参数不要求模型能力声明，单独或同时发送均原样转发，由上游判断是否支持。`auto`、显式选模和 `route.inspect` 都不依据推理能力声明拦截或过滤模型；格式、允许值及未知字段检查仍然保留。上游错误正常返回，不静默删除参数、降级或换模。

已有 `capabilities.reasoning` 数组保留读写兼容，仅作为说明性元数据；可省略，无需新增或更新配置。Jev state 的 requirements 仍包含实际请求的推理控制，计入必需部分字节预算。

当前 OpenAI 兼容上游路径通过 Bifrost 的 ExtraParams 仅转发这两个已校验字段，绕开 OpenAI 模型名称对 effort 的自动归一化；不开放任意 overrides。适配检查使用与发送相同的转换、序列化及扩展合并函数。输出限额维持既有 max_completion_tokens 语义，不换算为 thinking budget；不增加重试、fallback 或静默降级。

JSON message 与 SSE delta 保留文本 `reasoning_content`，仅含推理文本的 SSE 帧也属于有效输出，参与首包/空闲超时处理。允许 assistant 历史消息携带有界字符串 `reasoning_content`，以支持 SDK 回传 thinking 历史；签名、加密及其他 reasoning 扩展仍不支持。推理历史属于会话文本，可随其他对话内容发送给 Jev，并计入预算；不写入路由记录。

2026-10-01 本地验证：完整 Go 回归通过，未修改的本地 agent-sdk **3.5.1** 已通过 thinking、high effort、两者同时使用 × auto/显式模型 × JSON/SSE 的 12 次调用。该 SDK 仍发送 max_tokens，由 Router 既有兼容逻辑归一化；验证脚本打印实际 SDK 版本，不再假设本地 checkout 固定为 4.0.0。模拟上游检查实际发送的参数、输出预算、推理历史，并返回可断言的推理文本、正文及结束事件。另覆盖 false、各 effort 值在模型名称启发式下不被改写、无效组合零调用、推理阶段取消及路由检查一致性。

```sh
JEV_TEST_AGENT_SDK=/absolute/path/to/agent-sdk go test ./internal/transport/http -run TestUnmodifiedAgentSDKReasoning -count=1 -v
go test ./internal/provider -run Reasoning -count=1
```

2026-10-01 随后完成真实阿里云对照：`qwen3.8-flash` 与 `qwen3.8-max` 的 thinking enabled、high effort 及两者组合，在未经修改的 SDK 直连、本地 Router 显式选模和 auto 路径上均通过 JSON/SSE 验证。已核对实际控制字段、1024 输出限额、非空正文/推理文本、stop 与 SSE DONE；auto 使用唯一合格候选，未调用真实 Jev。仅承诺本次已测组合，其他 effort 值及 thinking=false 仍需对应端点验证。详细范围和脱敏证据见 [真实验收记录](acceptance/2026-10-01-reasoning.md)。

## SDK 完整 Agent examples

工具定义的 `function.description` 不再施加 4096 字符限制。SDK 的默认 Bash / Task 说明本身可能超过该值；网关完整保留说明，仍执行请求总大小（默认 16 MiB）与模型上下文容量检查；工具定义不进入 Jev 决策输入。`route.inspect` 与推理入口使用相同契约。

除 Provider 层验证外，新增原始 `examples/basic/01-simple-query.ts` 与 `examples/streaming/16-streaming.ts` 回归：SDK 加载默认工具池，经过真实 router/Bifrost，执行 Read，再将结果回传。模拟上游逐请求比较完整工具定义，JSON/SSE × 自动/显式模型四种组合均验证，不修改 SDK 示例源码：

```sh
JEV_TEST_AGENT_SDK=/path/to/agent-sdk go test ./internal/transport/http -run TestAgentSDKSimpleQuerySample -v -count=1
```

真实上游验收可运行 `TestSDKLiveSamples`，需设置 `JEV_RUN_LIVE_SAMPLES=1`、`JEV_TEST_AGENT_SDK`、`JEV_LIVE_KEY_FILE`、`JEV_LIVE_BASE_URL`、`JEV_LIVE_MODEL`。可选 `JEV_SAMPLE_LOG_DIR` 保存本地输出。此测试会产生模型费用并执行 sample 的真实工具；请在受控环境运行。工作目录使用临时 fixture；两个使用固定 `/tmp` 文件名的示例会拒绝覆盖已有文件。当前验收模型应支持 tools、thinking 和 low/medium/high effort。详见 [验收记录](acceptance/2026-10-02-sdk-samples.md)。
