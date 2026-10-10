# 管理能力契约

机器契约在 `management.json` 和 `schemas/` 中，服务端校验与实例能力发现使用同一来源。`GET /admin/v1/schema`、`GET /admin/v1/schema/{capability}` 和 `POST /admin/v1/call/{capability}` 均要求 settings 凭证。UI 复用这些管理能力，提供状态、模型描述、提示词和路由记录的查看，以及描述/提示词编辑。

## 最小能力集合

| 业务目标 | 结果与副作用 |
| --- | --- |
| 发现能力 | 返回版本、描述、schema、认证、风险及可用状态 |
| 读取状态 | 返回实例与依赖状态，不泄露凭证 |
| 列举/读取模型与供应商 | 分页、按权限过滤，返回模型描述和能力 |
| 更新模型/供应商配置 | 校验并原子提交新配置版本 |
| 读取/更新路由提示词 | 读取当前版本，写入用户修改；默认仅一份均衡模板 |
| 检查路由选择 | 返回选择结果，不调用生成模型、不改变会话；可能调用决策 API |
| 测试模型连接 | 对指定模型发送固定最小生成请求，可测试禁用模型；不接受任意对话，不自动启用，可能产生费用与外发 |
| 查询路由记录 | 返回有界、分页的记录，包含最多 120 字的用户文本摘要，不包含完整对话或决策自由文本理由 |

首版管理能力使用可信管理员 `settings` 凭证；`inference` 仅提供模型列表与生成，能使用实例内全部启用模型。两者是操作分工，不是互不信任租户之间的隔离。

配置按资源修改，保存前校验完整配置，成功即发布全局新版本，不自动发起网络检查。写入携带 `expected_version` 与幂等键，成功修改、版本递增和幂等结果在同一事务提交；成功结果保留 24 小时并可跨重启重放。命中成功记录先于版本检查；校验失败不占用幂等键，修正重试仍检查版本。版本冲突与幂等冲突分别使用 `config_conflict`、`idempotency_conflict`。

CLI 的 `--help` 离线可用，`schema` 发现实例实际能力；复杂输入支持 JSON 文件或 stdin，结果默认 JSON、日志走 stderr，不弹交互确认，不隐藏重试。这些行为已由 CLI/HTTP 及端到端测试覆盖。

## 模型 ID 约束

`auto` 是自动路由入口的保留对外 ID，生成模型无论是否启用均不得使用。模型创建及任何涉及对外 ID 的配置变更，必须由机器 schema 声明并由服务端校验拒绝，失败不改变配置或版本。此限制不针对上游模型名称。机器 schema、服务端事务校验及运行测试覆盖保留名；首版 ID 稳定，put 按 ID 创建或完整替换，不提供 rename。

## 每项契约必须描述

- 稳定 capability ID、description、输入输出 JSON Schema；参数约束、必填项和示例。
- 所需认证与 scopes，副作用及费用/外传可能，L0–L3 风险与审批责任方。
- 同步/异步结果语义、`operation_id`、稳定错误 code、`retryable` 与修复建议。
- 列表分页与有界输出，写入幂等键、预期配置版本及冲突规则。

能力风险按实际影响分类，不能将所有“更新配置”机械地标成低风险。新增外传目的地、放宽敏感保护或改变权限，需要比修改普通模型描述更严格的执行边界。支持何种审批机制在实现该能力前确定并声明，不伪造审批句柄。

只读 schema/status 不调用付费模型；选模检查不是无副作用的纯本地预览。任何异步能力只有在状态查询和终态都实现后才声明可用。

## 响应约定

管理结果使用 `ok`、`data` 或 `error`、`warnings`、`meta`，其中 meta 包含 operation ID、timestamp 和 schema version；首版为同步管理能力，未暴露异步事件协议。CLI 将结构化错误映射到稳定非零退出码，并在契约中公布映射。

这些约定只用于管理能力。`/v1/chat/completions` 保持其 JSON/SSE 格式，`/v1/models` 保持模型列表格式。公共模型列表不携带供应商凭证与管理元数据。

参考：[AgentUse 0.2.0 协议页面](https://www.zerone.run/zh/protocol)。本目录不表示已获得协议认证。

## 请求与输出 schema

call body 为 `{"input":{...},"expected_version":1,"idempotency_key":"operation-key"}`；读操作只需 input。能力的 input_schema 验证 input，output_schema 描述成功响应的 data。写入返回 version/resource；models.test 的 data.ok 表示探测结果，顶层 ok 表示管理操作是否完成。错误和退出码在 schemas/results.json 中。

风险为声明给调用方的元数据，不是内置审批引擎。settings 是可信管理员，客户端按用户授权决定是否执行。schema/status 不产生上游调用，route.inspect/models.test 的外发与费用在能力描述中列出。

每项运行时能力包含 `call.method`、`call.path`、完整 `call.schema` 和 `call.protocol`。调用信封由 schemas/call.json 定义并用于写入边界校验；能力 input_schema 嵌入 call.schema.input。call.protocol 公布全局版本冲突恢复、成功幂等重放顺序/身份范围/24小时期限、失败不占键及通用 HTTP/CLI 错误映射。幂等期限与服务端过期计算来自同一 results.json 定义。仅查询单项 schema 也能获取完整调用与重试规则。

提示词长度统一按 JSON Schema maxLength 的 Unicode 字符数计算（上限16384），保存整个配置时复用 prompt.put 校验。Jev 的序列化字节预算独立计算：可以保存的提示词仍可能使多候选决策超限，此时明确返回 budget_exceeded。

供应商详情 `providers.get` 返回只读 endpoint 和 `api_key_masked`：密钥超过 8 个字符时仅显示前 4 个字符及 `***`，短密钥全部打码，无法解析时返回 null。完整密钥不会进入管理响应，脱敏字段不参与配置写入。

## 托管供应商凭证

`providers.put` 的 `api_key` 与 `secret_ref` 二选一。`api_key` 只写，限非空 UTF-8、16384 字节以内且不含 CR/LF/NUL；schema 的 maxLength 同时限制字符数，共享运行时校验执行更严格的字节上限。写入需要服务端部署主密钥；密钥、供应商配置、全局版本与脱敏幂等结果同事务提交。响应仅含不可变的 `managed:<revision-id>` 引用，读取保持前缀脱敏；解析失败返回 null 和 `provider_credential_unavailable` 警告。引用必须属于同一供应商，不能用于决策后端。

内联密钥写入使用主密钥派生的 HMAC 摘要识别幂等请求；SQL、回执和配置快照均不保存明文。缺失密钥或不可解密返回通用 `config_missing`，不降级为明文。更换供应商 Key 不改变已捕获请求的凭证版本。历史密文保留，删除不代表物理擦除或紧急撤销。CLI 拒绝非回环 HTTP 提交内联密钥且不跟随重定向；HTTPS 反向代理由部署方配置。

## Dashboard 会话认证

`schemas/session.json` 是认证端点、有效期、容量、Cookie 和浏览器来源要求的机器契约，随 `call.protocol.session` 发布；认证端点不是配置能力，不消耗版本或幂等键。CLI 保持 Settings Bearer；普通管理请求仅在 Authorization 缺席时接受 Cookie，有效 Cookie 不会覆盖无效 Bearer。推理 API 使用 inference 凭证。Chat 和 models 保持 Bearer；`/v1/messages` 另接受 `x-api-key`，同传时两个凭证必须都有效且属于同一身份/角色，Cookie 不授予推理权限。

登录用 Settings Bearer 与空 JSON 对象换取 HttpOnly Cookie，返回 expires_at/csrf_token；状态查询恢复 CSRF，注销只撤销请求会话。所有 Cookie POST 校验精确 Origin、JSON 与绑定该会话的 CSRF；GET 要求 X-Jev-Session: 1。响应禁止缓存。只有成功登录写 Cookie，状态、注销和错误均不清 Cookie，避免迟到响应删除另一标签页的新会话。遇到 CSRF 不匹配，显式恢复后重新决定操作，不自动重放。`session_limit` 为 HTTP 429 / CLI 6，retryable=false。

## 模型列表排序

`models.list` 默认按 ID 升序，传入 `sort: "enabled_first"` 时先列启用模型，再列禁用模型，组内按 ID 升序。排序在分页前执行；翻页时保持 sort 不变，并原样传入 next_cursor。也可传入从 0 开始的 `offset` 直接跳页，不可与非空 cursor 同时使用；响应包含模型总数 `total`。Dashboard 默认每页 20 条，可选择 10/50/100 条，采用启用优先排序。配置变更后刷新列表，重新从第一页读取。

`models.list` 支持 `query` 实时筛选：忽略大小写、去除首尾空白，按字面子串匹配模型 ID、描述、供应商 / 上游名称、位置和可见状态/能力标签。`language` 为 `en`（默认）或 `zh`，决定标签文案。筛选先于排序和分页，`total` 为匹配总数；搜索变化后从第一页读取。

## 推理控制与模型能力

`chat.json` 与 `route.inspect` 校验 `chat_template_kwargs.enable_thinking` 和 `reasoning_effort` 的格式及允许值，拒绝未知模板字段。合法推理控制直接原样转发，不要求模型声明能力，也不根据这些参数过滤候选。上游判断是否支持，失败不触发参数删除、降级或换模。

既有可选 `capabilities.reasoning` 保留读写兼容，但仅作为说明性元数据，不参与请求校验、自动选模或显式选模。无配置、空数组或与请求不同的元数据均不会阻止推理参数透传。Jev 仍接收请求的推理控制并计入输入预算。Chat JSON/SSE 保留文本 reasoning_content，并允许 assistant 历史回传；签名/加密推理扩展不在本契约内。

## 路由记录分页与保留

`records.list` 返回 `records`、`total` 和 `next_cursor`。默认每次 50 条，上限 200；支持从 0 开始的 `offset` 或既有游标，不允许非空 cursor 与非零 offset 混用。按写入顺序从新到旧，每次查询的总数和记录来自同一数据库快照；分页间新写入或清理可能改变页内容，不承诺跨请求快照。UI 默认每页 20 条，并提供 10/50/100 条和页码；清理后超出的页码回到最后有效页。

`decision_ms` 只计选模阶段（请求验证、候选过滤及需要时的决策调用），不含生成耗时；不再输出 `generation_ms`。`request_summary` 为最后一条 user 消息的文本片段，合并空白后最多 120 个 Unicode 字符，本地提取，不额外调用模型；无文本或旧记录时缺省。工具消息、图片地址和更早的消息不进入摘要。默认保留 7 天且最多 100000 条，数量超限按写入顺序丢弃最旧记录；天数和数量均可通过启动配置调整，详见 configuration.md。

`route.inspect` 的可选 `context_estimate` 返回 semantic_bytes_v1 分项和输入/输出预留总量；新增 path `context_estimate_fallback` 表示 auto 全部合格候选估算超限后的最大上下文保底，不代表精确容量足够。最大容量候选只有一个时直接选用；多个并列时仅将这些候选交给 Jev 选择一轮，path 仍为 context_estimate_fallback，保留完整 candidate_ids 和决策 usage。

显式模型的 route.inspect 不进行上下文估算，省略 context_estimate，context_exact=false 表示未计数；仍执行请求格式、启用及能力检查。

## Anthropic Messages 推理契约

`schemas/messages.json` 定义 POST /v1/messages 的输入形状；provider 还执行明确公布的内容组合及目标转换检查。`thinking.enabled` 的预算格式可识别，但当前 Chat adapter 无法执行独立预算，返回 unsupported_request；adaptive/disabled 和显式 output_config.effort 已实现。原生签名、带 is_error=true 的工具结果和不可保持顺序的混合块明确拒绝。版本固定 2023-06-01，beta 扩展拒绝。

该入口使用独立 Anthropic 错误信封，认证失败也包含顶层 type:error、error.type/message、request_id；request-id 与 X-Request-ID 相同。400/401/403/404/405/413/422/429/500/502/503/504/529 的映射和流内失败范围见 [兼容文档](../docs/compatibility.md#anthropic-messages-issue-23)。Messages 不套管理信封，也不透传上游 OpenAI 错误体。

Generation providers accept `protocol: "openai" | "anthropic"`. Omitted protocol defaults to `openai`, including replacement writes; preserve it when editing an Anthropic provider. Reads return the effective protocol. Existing stored rows need no migration or version increment. `base_url` is the API prefix (for example `https://api.anthropic.com/v1`).

## Playground 浏览器生成

`schemas/playground.json` 是专用端点、输入、流事件、认证与默认限额的单一来源，随 `call.protocol.playground` 发布，不是普通 `/call` 能力。`GET /admin/v1/playground` 返回启用状态、有效限额及当前会话/实例/UTC 日剩余额度快照；`POST /admin/v1/playground/completions` 接收 model、文本 user/assistant messages、stream:true，assistant 可携带协议需要的 reasoning_content。其余字段拒绝；输出限额由服务端注入。

两端点只接受 Dashboard Settings Cookie；GET 需要 X-Jev-Session，POST 需要 JSON、精确 Origin/Host 和绑定会话的 CSRF。任何 Authorization 替代均被拒绝，Cookie 不因此获得 /v1 推理权限。该能力是 Settings 角色的受限浏览器生成例外，Settings Bearer 及通用管理 call 不提供生成。

准入前不调用上游，成功受理后费用/次数不退款，不自动重试或隐式换模。POST 直接复用一次实际推理规划，不能先 route.inspect 再生成。SSE 按 route、delta、done/error 返回，delta 的 content/reasoning_content 分开；仅收到 finish_reason 后正常终止才发 done。EOF 无终态为中断；本端点不宣称 OpenAI SSE 兼容。HTTP 头前错误为 JSON error，流内错误为独立 error 事件；不回显供应商错误体。

429 返回 playground_rate_limited、limit_scope、retry_after_seconds、可用时 reset_at，并带 Retry-After。实例 UTC 日额度持久化，并发为单进程；配置和全部默认值见 configuration.md。只有真实执行元数据进入路由展示，不产生 Jev 自由文本理由。
