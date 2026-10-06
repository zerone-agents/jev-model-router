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

Jev 决策默认限制为 255 个候选、32,000 UTF-8 序列化字节（包含问题和选项），可通过 decision_max_bytes / JEV_ROUTER_DECISION_MAX_BYTES 修改。默认值参照官方单问题 32k token 上限采用保守字节估算；官方未提供字节推荐值，这不是精确 token 计数，详见 [启动配置](configuration.md#jev-决策输入预算)。保留全部候选、偏好、system/developer 指令和最新 user query；较旧回合按完整组从近到远加入，溢出时标记省略。Jev 不接收工具定义，工具执行仅保留调用 ID、工具名称和结果关联记录，不保留参数、输出及执行消息附带文本。图片结构替换为 image_present，不保留 URL 或内联数据。system/developer 与最新 query 的各文本段超限时分别保留首尾、从中间截断，并标记 content_truncated；仅不可裁剪元数据与最小首尾结构仍超限时失败。生成请求保持完整。

生成容量预检使用 `semantic_bytes_v1`：完整历史文本（含工具结果、reasoning/refusal）、工具名称和描述按 ceil(UTF-8 字节总数/4) 估算；工具参数按解码后的字符串字节数、工具 schema/response_format/tool_choice 按紧凑 JSON 字节数单独核算；每消息预留 4、每工具调用/定义预留 8、每张图片预留 8192。图片 URL/base64 不计作文本；model、stream、temperature 等非提示字段不计入。输入与输出分开，输出限额只加一次，未指定时仍预留 4096，但不会向上游注入该限额。`route.inspect.context_estimate` 返回方法、文本/结构化字节数、图片/framing、输入、输出预留及总估算；`context_exact=false` 表明它不是精确计数，也不保证多语言或视觉模型上界。

`auto` 仅当所有通过非容量约束的模型都因非精确估算超限而被排除时，保留 ContextLimit 最大的全部模型作为保底候选，路径标记为 `context_estimate_fallback`；只有一个候选时直接执行，多个并列时仅将这些候选交给 Jev 选择一轮，候选列表和决策 usage 保留。若任一模型正常通过容量检查，沿用原流程；精确超限不进入此保底。能力、启用状态、供应商配置检查不能被绕过；显式模型跳过本地上下文预算估算与拦截，由上游判断容量，不换模。并列候选的 Jev 决策失败时明确报错，不任意选用模型；不重试上游；完整生成输入和输出限额保持不变，实际超限由上游处理。

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

## 生成上游错误

生成供应商返回的 OpenAI 兼容错误保留 `error.message/type/code/param`，普通 JSON 与 SSE 建连失败保留上游 400–599 状态码。SSE 已发送响应头后无法更改 HTTP 状态，错误通过流内 `error` 对象返回；没有有效错误状态时使用 502。此处透传的是供应商结构化错误字段，不包含 Bifrost 内部诊断、原始请求或完整响应头；不承诺逐字节转发任意 HTML/非标准响应。网络异常、超时、取消及 SDK 内部错误仍使用 router 的稳定错误。内部记录和管理接口保留 `upstream_error` 分类及通用信息，不写入供应商错误内容。

仅在原始响应能验证为结构化 error 对象（非空字符串 message，type/code/param 为字符串或 null）时透传。纯文本、HTML、格式错误和连接故障使用 safeError。当前 Bifrost 不保留 HTTP 200 流内错误的原始帧，因此该路径无法验证来源，也返回通用错误；不依据 IsBifrostError=false 推断来源。

## Anthropic Messages (Issue #23)

2026-10-06：新增客户端侧 `POST /v1/messages`，当前仍调用已配置的 OpenAI-compatible Chat Completions 生成上游。使用锁定 Bifrost 的 Anthropic→Responses→Chat 和返回转换；只对实测转换缺口补充消息分组合并、并行控制、显式推理参数及 usage/终态保护。已有 Chat Completions 的供应商原始错误透传规则不适用于此新入口。

| 能力 | 当前范围及证据 |
| --- | --- |
| 文本、顶层 system、auto/显式、JSON/SSE | 本地生产 Router/Bifrost 与模拟上游通过；官方 Anthropic SDK 0.52.0 四种模式通过 |
| 图片 | HTTPS URL/base64 png/jpeg/webp/gif 转换；本地 fixture 检查字段保留，官方 SDK 续写回合含内联图片；不下载图片；真实视觉模型未验证 |
| 普通 tools、tool_choice、disable_parallel_tool_use | 候选能力过滤和最终 wire 检查；官方 SDK 完整工具结果续写、SSE 参数增量和 ID/输入配对通过；is_error=true 明确拒绝 |
| temperature/top_p、输出限额 | Messages temperature 为 0–1、top_p 为 0–1；max_tokens 必填，16–10000000，合法 JSON 整数形式归一化，不钳制 |
| JSON Schema | output_config.format 转为现有 response_format；需要模型 structured_output 能力。无 schema 的 JSON object 可用 `{"type":"object"}` schema 表达；不新增非标准 json_object 字段 |
| thinking/effort | adaptive→enable_thinking=true；disabled→false；显式 output_config.effort 的 low/medium/high/xhigh/max 原样转发，不由模型名改写，不注入默认 effort。官方 SDK unsigned thinking 历史、JSON/SSE 输出通过；真实模型未验证 |
| 手动 thinking 预算 | enabled+budget_tokens 的形状可识别，但当前上游适配会丢掉独立预算，入口在任何网络调用前明确拒绝，不能称为预算支持 |
| 原生签名/加密历史 | 明确拒绝。Bifrost 返回的空 payload Responses item 标记可回传，只用于关联，不宣称是 Claude 签名 |
| usage | JSON 缺失则省略；SSE 初始 usage={}，最终 message_delta 补充真实报告的 input/output counts；真实零值保留；报告的缓存读写计数分别输出，普通 input_tokens 扣除这些计数，不把缓存输入算作普通输入。官方 SDK 0.52.0 能累积更新，这是与严格原生 Anthropic 初始数值 usage 的兼容差异；其他严格客户端未验证 |
| stop_sequences | 请求原样转发；上游仅返回模糊 stop 且未给出匹配序列时明确失败，不猜测 end_turn/stop_sequence。已用模拟上游验证带 `stop:"END"` 的 JSON/SSE 匹配元数据；矛盾终态明确失败，真实供应商元数据仍未验证 |
| 失败、取消、超时 | 本地测试通过流前 JSON、流内 error、无终态断流、首包/空闲超时、客户端取消；失败不发 message_stop，不换模；单请求不重试 |

必须使用 `anthropic-version: 2023-06-01`、JSON Content-Type 和 inference x-api-key/Bearer；同时提供认证头时必须均有效且身份/角色一致。settings 和 Cookie 不授权推理。非空 beta、cache_control、文档、服务端工具、原生 redacted_thinking、不可保持顺序的 assistant 混合块明确拒绝。普通 assistant 块顺序为 thinking→text→tool_use；tool_result 在 user 额外文本/图片之前，结果必须与此前调用配对。thinking-only assistant 历史通过空文本结构载体保留；同角色的连续 assistant 消息也执行跨消息块顺序检查，不接受文本/工具之后的 thinking。不承诺全量 Messages。

所有错误使用 `{"type":"error","error":{"type":"…","message":"安全诊断"},"request_id":"…"}`。响应头 request-id/X-Request-ID 与 body request_id 一致；流内为 event:error，不发送成功终态。凭证无效 401/authentication_error，角色不符 403/permission_error，校验或不支持 400/invalid_request_error，方法 405，体积超限 413/request_too_large，显式模型不可用 404/not_found_error，无候选/决策预算 422/invalid_request_error，配置/存储故障 503/api_error，生成/转换/决策故障 502/api_error，超时 504/timeout_error，仍可写连接上的取消 499/api_error。上游 429/529 分别保留并映射 rate_limit_error/overloaded_error；可信的请求相关失败与 5xx 按公布分类保留状态；上游 401/402/403 映射 502/api_error。上游错误内容、诊断及其请求 ID 不直接透传。

复现未经修改的官方 SDK 集成（Node 项目需已有 @anthropic-ai/sdk，打印实际版本）：

```sh
JEV_TEST_ANTHROPIC_SDK=/absolute/path/to/node-project go test ./internal/transport/http -run TestUnmodifiedAnthropicSDK -count=1 -v
go test ./internal/compat -run Anthropic -count=1 -v
go test ./internal/provider ./internal/transport/http -run 'PreparedMessages|Messages' -count=1
```

SDK 测试不改写 fetch/请求；单次调用验证设置 maxRetries=0，四个完整回合共 8 次模拟生成请求；另有 1 次模拟断流验证 SDK 流内错误解析，认证失败和非法请求不外发。示例见 examples/anthropic-messages.mjs。官方 SDK 默认重试是客户端行为，不能误认为 Router 换模或重试。

发布条件仍未全部完成：独立 thinking 预算、原生签名兼容、严格 initial usage、停止元数据、OpenAI 独有 effort 档位在标准 Anthropic 字段中的表达仍有差距；部署环境和真实模型验证未运行。不得据本地部分通过结论关闭 #23 或宣称已经覆盖当前 OpenAI 入口的全部语义。

Messages SSE 在 Bifrost 累计状态之前执行总预算：序列化后的已解码帧累计最多 16 MiB、最多 65536 帧，原始捕获单次最多 16 MiB。包含文本、thinking、工具参数/名称、ID 等元数据；超限明确失败并取消上游，不截断成成功输出。每个工具参数另限 1 MiB。该预算限制 adapter/converter 的累计数据，不宣称约束 SDK 解析一个巨大原始帧之前的瞬时内存分配。
