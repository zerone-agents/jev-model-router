# Compatibility scope and verification evidence

The protocol scope below was recorded through v0.1.19; Auto fallback is the subsequent addition described here. Dated sections record verification for the respective versions, not real-model retesting on every release.

| Client entrypoint | OpenAI-compatible upstream | Native Anthropic upstream |
| --- | --- | --- |
| Chat Completions | JSON / SSE | JSON / SSE, including the documented thinking and tool continuations |
| Anthropic Messages | JSON / SSE, converted | JSON / SSE, native preservation; see #51 below |

## Base conversion and routing

2026-09-28: Bifrost Core was pinned to `v1.10.2-0.20260924033752-a8411cd54142`, with Go 1.27.0. SQLite uses the pure-Go modernc.org/sqlite v1.39.1 driver without CGO; race tests still require a supported native build environment. Dependencies are locked in go.mod/go.sum.

`go test ./internal/compat -count=1 -v` checks the following boundaries against local simulated upstreams. It does not call paid models or establish compatibility with arbitrary remote services.

| Input/behavior | Result and contract |
| --- | --- |
| Text, image URLs/inline images | Fields preserved; no image URL fetches or SDK image downloads |
| tools, required tool_choice, parallel_tool_calls=false | Preserved on the tested OpenAI/gpt-4o-mini conversion path; other models still require per-request conversion checks |
| temperature, top_p, stop, json_object, stream_options.include_usage | Preserved in tested conversions; production entrypoints reject unknown fields |
| max_completion_tokens / max_tokens, integer 16–10000000 | Normalized to max_completion_tokens; preserved through tested JSON/SSE conversions |
| Output limit below 16 | Bifrost raises it to 16; production rejects it explicitly and fixed connection tests use 16 |
| 429, 503, SSE 503, HTTP 200 SSE error, interrupted streams | Original baseline: one selected-upstream call, no calls to other targets, observable errors. Auto fallback now permits one reselection under the conditions below. |
| Signed/encrypted reasoning extensions, client fallback or provider overrides | Outside the original input contract; later native support is documented separately below |

The native Jev protocol follows the [API reference](https://docs.typesafe.ai/api): state/questions, at most 255 choices, and selection with probabilities/confidence rather than free-text explanations. Fixtures are synthetic examples of documented shapes, not actual API results. Input limits follow the [Models reference](https://docs.typesafe.ai/models) and must be checked against the configured model before deployment. Without an exact tokenizer, estimates include headroom but cannot guarantee upstream acceptance.

Generation parameters must satisfy the published schema, model capability declarations and actual adapter conversion checks. SDK conversion tests do not prove model capabilities. Local tests cover cancellation, first-event/idle timeouts and bounded buffering. Full slow-reader socket stress across SDK paths remains separate work, as does paid model-quality evaluation.

Jev defaults to 255 candidates and 32,000 serialized UTF-8 bytes including questions and options, configurable with decision_max_bytes / JEV_ROUTER_DECISION_MAX_BYTES. This conservative byte estimate references the official single-question 32k-token limit; it is neither an official byte recommendation nor exact tokenization. See [startup configuration](configuration.md). All candidates, preferences, system/developer instructions and the latest user query are retained. Older turns are added as complete groups from newest to oldest, marking omissions. Jev receives no tool definitions; tool history retains call IDs, names and result associations, without arguments, outputs or attached execution text. Images become image_present without addresses or inline data. Oversized text segments in instructions or the latest query retain their beginning and end, omit the middle and mark content_truncated. Failure occurs only when mandatory metadata and minimal text framing still exceed the budget. The generation request remains complete.

Generation capacity estimation uses `semantic_bytes_v1`: full history text, including tool results and reasoning/refusal, plus tool names/descriptions is estimated as ceil(total UTF-8 bytes / 4). Tool arguments use decoded string bytes; tool schemas, response_format and tool_choice use compact JSON bytes, accounted separately. Framing reserves 4 per message, 8 per tool call/definition and 8192 per image. Image URL/base64 and non-prompt fields such as model/stream/temperature do not count as text. Input and output are separate; output is reserved once. Unspecified OpenAI output reserves 4096 without injecting that limit upstream; native Anthropic sends and reserves 65536. `route.inspect.context_estimate` reports the method, text/structured bytes, image/framing, input, output reserve and total. `context_exact=false` indicates a heuristic, not a multilingual or visual-model upper bound.

When every otherwise eligible Auto candidate exceeds a non-exact capacity estimate, all models tied for the largest ContextLimit become overflow candidates, with path `context_estimate_fallback`. A sole candidate skips model selection but may still need an automatic effort decision; Jev selects among ties, preserving candidates and decision usage. Normal capacity matches take precedence; exact overflow never uses this path. Capability, enablement and provider checks remain mandatory. Explicit selection skips local capacity estimation and delegates limits to the upstream without switching models. Jev failures remain failures, with no arbitrary substitute. Generation inputs and output limits remain unchanged. This capacity policy is distinct from Auto fallback after an eligible generation failure: Auto can now reselect once before semantic output, excluding the failed model. SDK retries remain disabled; explicit requests never switch models.

Plain `response_format.type=text` does not require structured-output capability; JSON modes do. Jev's required input also includes output format/JSON Schema, tool_choice, parallel_tool_calls and max_completion_tokens, all counted in its decision budget.

SSE usage reflects upstream reporting: absent values are omitted and explicit zeros retained. The adapter checks raw SDK frames in memory only to detect usage presence; public responses and records contain no raw SDK metadata. An empty database or no enabled model gives Auto config_missing/503; enabled models that cannot satisfy the request give no_candidates/422.

Historical assistant tool calls treat content:null and absent content as equivalent. Adapter-added tool_calls.index does not change semantic comparison and is omitted from non-streaming tool responses. Streaming deltas retain index; IDs, names, argument strings, ordering and results remain intact. End-to-end Bifrost/HTTP tests cover complete tool rounds.

## Real-endpoint smoke checks

Official Jev `jev-1.13.0` and Alibaba Cloud `qwen3.8-flash` / `qwen3.8-max` were used to verify multi-candidate selection, generation and record retrieval. Both generation models passed JSON, SSE, tool_choice=auto and tool-history continuation without top-level tools. Provider base_url includes the full compatibility prefix; the adapter appends only `/chat/completions`, with strict JSON/SSE regression tests.

The tested Alibaba thinking combination rejected tool_choice=required; the gateway returned failure without changing parameters. finish_reason=length can indicate output truncation: HTTP success is not task-quality success. These smoke checks do not establish all model/field combinations or stable performance. Real images, streaming tool deltas and provider cancellation/timeouts require separate verification.

## agent-sdk output-limit compatibility (Issue #18)

Chat Completions and route.inspect accept either max_tokens or max_completion_tokens, never both, even if equal or one is null. Capacity estimation, Jev input and generation adaptation use max_completion_tokens without clamping or dropping values, for both Auto and explicit selection.

Both entrypoints share validation and diagnostics. Conflicting fields return HTTP 400 / invalid_request explaining mutual exclusion. Out-of-range, null, string, boolean, array, object and non-integer values use the same code and identify the field and integer range 16–10000000. Other unsupported fields remain unsupported_request. Schema errors identify top-level fields where possible without echoing values, conversations or raw schema errors; unusually long or malformed unknown field names receive generic messages.

`go test ./internal/transport/http -run TestSDKOutputLimitThroughRouter -v` exercises production HTTP/routing/Bifrost with a local fake upstream: both fields, both selection modes, JSON/SSE, limit preservation and no outbound calls for invalid requests. Routing tests also cover capacity budgets, bounds and conflicts.

2026-09-30: unmodified local agent-sdk 4.0.0 OpenAIProvider.createMessage/createMessageStream passed four Auto/explicit × JSON/SSE requests through the updated local Router. No fetch wrapper or request rewrite was used. The simulated generation endpoint establishes protocol behavior, not deployment or real-model acceptance. Reproduce with tsx installed in the SDK checkout:

```sh
JEV_TEST_AGENT_SDK=/absolute/path/to/agent-sdk go test ./internal/transport/http -run TestUnmodifiedAgentSDK -count=1 -v
```

Without the variable, this SDK integration explicitly skips while ordinary Go regressions still run. Issue #18 did not broaden reasoning controls or empty-tools support; later reasoning support follows below.

## agent-sdk reasoning controls (Issue #22)

Chat Completions and route.inspect accept:

- chat_template_kwargs with the required boolean enable_thinking only; true/false are supported, while empty objects, null and other template fields are rejected.
- reasoning_effort: none, minimal, low, medium, high, xhigh, max. This is entrypoint vocabulary, not a claim that every model supports every value.
- SDK thinking: {type: "enabled"} maps to chat_template_kwargs; the SDK-internal top-level thinking object is not accepted.

With an explicit model, valid OpenAI-compatible thinking/effort controls require no matching capability declaration and are forwarded unchanged, individually or together, for upstream validation. Shape, allowed-value and unknown-field checks remain. Explicit requests fail without silently stripping parameters, degrading effort or switching models.

Auto derives effort levels from capabilities.reasoning. Manual effort, disabled thinking and fixed budgets are rejected before decision or generation; enabled/adaptive thinking without a fixed budget remains subject to adapter support. Chat route.inspect follows the same rules. See [Automatic effort](#automatic-effort-58) for upgrade behavior; reasoning declarations are no longer descriptive-only metadata.

The OpenAI path forwards only these two validated extensions through Bifrost ExtraParams, bypassing model-name effort normalization without exposing arbitrary overrides. Validation uses the same conversion, serialization and extension merge as actual sending. Output limits keep max_completion_tokens semantics rather than becoming a thinking budget. No SDK retries or silent downgrade are introduced; bounded Auto reselection is an independent router policy.

JSON messages and SSE deltas preserve textual reasoning_content. Reasoning-only frames are valid output and participate in first-event/idle handling. Bounded reasoning_content strings in assistant history support SDK replay; signatures, encryption and other reasoning extensions remain outside this conversion contract. Reasoning history is conversation text, may reach Jev and counts toward budgets, but is not persisted in routing records.

2026-10-01 local verification: full Go regressions passed. Unmodified local agent-sdk **3.5.1** passed 12 thinking/high-effort/combined × Auto/explicit × JSON/SSE requests. It still sends max_tokens, normalized by existing Router logic. Verification prints the actual SDK version rather than assuming 4.0.0. Fake upstreams assert controls, output limits and reasoning history and return checkable reasoning, text and terminal events. Tests also cover false, effort preservation despite model-name heuristics, invalid combinations without calls, cancellation during reasoning and inspection consistency.

```sh
JEV_TEST_AGENT_SDK=/absolute/path/to/agent-sdk go test ./internal/transport/http -run TestUnmodifiedAgentSDKReasoning -count=1 -v
go test ./internal/provider -run Reasoning -count=1
```

Later on 2026-10-01, real Alibaba qwen3.8-flash/qwen3.8-max passed thinking-enabled, high effort and their combination over JSON/SSE via unmodified direct SDK, local explicit Router and Auto. Checks covered actual controls, 1024 output limit, nonempty text/reasoning, stop and SSE DONE. Auto had one eligible candidate and did not call real Jev. Other effort values and thinking=false require endpoint-specific checks. Per-run details remain local and are not a public benchmark.

## Complete SDK Agent examples

The former 4096-character function.description limit is removed: default SDK Bash/Task descriptions can exceed it. Descriptions remain intact while total body size (default 16 MiB) and model context checks apply. Tool definitions do not reach Jev. route.inspect shares the inference contract.

Original examples/basic/01-simple-query.ts and examples/streaming/16-streaming.ts load the default tool pool, call the production Router/Bifrost, execute Read and return its result. Fake upstreams compare complete tool definitions per request. JSON/SSE × Auto/explicit are covered without modifying SDK examples:

```sh
JEV_TEST_AGENT_SDK=/path/to/agent-sdk go test ./internal/transport/http -run TestAgentSDKSimpleQuerySample -v -count=1
```

Real-upstream TestSDKLiveSamples requires JEV_RUN_LIVE_SAMPLES=1, JEV_TEST_AGENT_SDK, JEV_LIVE_KEY_FILE, JEV_LIVE_BASE_URL and JEV_LIVE_MODEL. Optional JEV_SAMPLE_LOG_DIR retains local output. It incurs model fees and runs actual sample tools; use a controlled environment. Working directories are temporary fixtures, and two examples using fixed /tmp filenames refuse to overwrite existing files. The acceptance model should support tools, thinking and low/medium/high effort. These checks establish protocol/tool roundtrips, not task quality or all SDK examples.

## Generation upstream errors

Validated OpenAI-compatible errors retain error.message/type/code/param and HTTP 400–599 for JSON or SSE connection failures. After SSE headers, errors use in-stream error objects; missing valid status defaults to 502. error.diagnostic.upstream_error contains sanitized JSON or plain-text bodies, including HTML text, excluding Bifrost internal diagnostics, original requests and full response headers. Redaction and bounds are in contracts/README.md. Network, timeout, cancellation and SDK-internal failures use stable Router errors. Records and management retain generic classification rather than provider error bodies.

Top-level provider error fields require a validated structured error object with nonempty string message and string/null type/code/param. Plain text, HTML, malformed bodies and connection failures use safeError; sanitized details remain response-only. Bifrost itself discards raw HTTP 200 in-stream errors; the newer SDK-reader capture boundary described below retains these for validation. IsBifrostError=false alone is not proof of provenance.

## Anthropic Messages (Issue #23)

This section describes /v1/messages conversion to OpenAI-compatible Chat Completions; native Anthropic targets are documented under #51. The pinned Bifrost Anthropic→Responses→Chat conversions are supplemented only for observed gaps: message grouping, parallel controls, explicit reasoning parameters, usage and terminal validation. Messages retains its own error envelope instead of copying Chat's top-level provider errors.

| Capability | Scope and evidence |
| --- | --- |
| Text, top-level system, Auto/explicit, JSON/SSE | Local production Router/Bifrost and fake upstreams; official Anthropic SDK 0.52.0 passed all four modes |
| Images | HTTPS/base64 png/jpeg/webp/gif conversion; fixture field checks and SDK inline-image continuation; no downloads; real vision models unverified for this conversion |
| Ordinary tools, tool_choice, disable_parallel_tool_use | Candidate and final-wire checks; official SDK result continuation, SSE argument deltas and ID/input matching; is_error=true rejected |
| temperature/top_p, output limit | temperature/top_p 0–1; required max_tokens 16–10000000, valid JSON integer representations normalized without clamping |
| JSON Schema | output_config.format maps to response_format, requiring structured_output. A generic JSON object uses {"type":"object"}; no nonstandard json_object field |
| thinking/effort | adaptive→enable_thinking=true, disabled→false; explicit low/medium/high/xhigh/max preserved without model-name rewrites or defaults. SDK unsigned history and JSON/SSE outputs tested; real models unverified for this conversion |
| Manual thinking budget | enabled+budget_tokens shape recognized, but this adapter would lose the independent budget; OpenAI candidates reject it before generation |
| Native signed/encrypted history | Rejected. Empty-payload Responses item markers may roundtrip as associations, not Claude signatures |
| usage | Missing JSON fields omitted. SSE starts with usage={} and final message_delta supplies reported counts, including real zeros. Cache read/write counts remain separate and are subtracted from ordinary input. SDK 0.52.0 accumulates updates; this differs from strictly numeric native initial usage. Other strict clients unverified |
| stop_sequences | Forwarded unchanged; ambiguous stop without matched metadata fails rather than guessing end_turn/stop_sequence. Fake JSON/SSE matching stop:"END" tested; contradictory terminal metadata fails; real provider metadata unverified |
| refusal | Pinned converter cannot fully represent it; JSON/SSE returns safe upstream_error rather than empty successful content |
| Failures, cancellation, timeouts | Pre-stream JSON, in-stream error, missing terminal, first-event/idle timeout and cancellation tested. Failures emit no message_stop. Only Auto may reselect once before semantic output; explicit requests never retry. |

Require anthropic-version: 2023-06-01, JSON Content-Type and inference x-api-key/Bearer. If both auth headers are present, both must be valid and identify the same principal/role. Settings and Cookies do not authorize inference. Nonempty beta, cache_control, documents, server tools, native redacted_thinking and unrepresentable mixed assistant ordering are rejected on this conversion path. Ordinary assistant blocks follow thinking→text→tool_use. tool_result precedes extra user text/images and matches prior calls. Thinking-only history uses an empty-text structural carrier. Consecutive assistant messages also enforce cross-message ordering, rejecting thinking after text/tools. This is not full Messages compatibility.

Errors use {"type":"error","error":{"type":"…","message":"safe diagnostic"},"request_id":"…"}. request-id/X-Request-ID headers match body request_id; SSE uses event:error without a successful terminal event. Mappings: invalid credentials 401/authentication_error; wrong role 403/permission_error; validation/unsupported 400/invalid_request_error; method 405; body size 413/request_too_large; unavailable explicit model 404/not_found_error; no candidates/decision budget 422/invalid_request_error; configuration/storage/fallback exhaustion 503/api_error; generation/conversion/decision 502/api_error; timeout 504/timeout_error; cancellation on a writable connection 499/api_error. Upstream 429/529 map to rate_limit_error/overloaded_error, trusted request failures and 5xx retain published classifications, and upstream 401/402/403 map to 502/api_error. Raw upstream identities do not replace Router request IDs; sanitized details use optional diagnostics.

Reproduce with an existing Node project containing @anthropic-ai/sdk; the test prints its version:

```sh
JEV_TEST_ANTHROPIC_SDK=/absolute/path/to/node-project go test ./internal/transport/http -run TestUnmodifiedAnthropicSDK -count=1 -v
go test ./internal/compat -run Anthropic -count=1 -v
go test ./internal/provider ./internal/transport/http -run 'PreparedMessages|Messages' -count=1
```

SDK tests do not rewrite fetch or requests. With maxRetries=0, four complete rounds make eight fake generation calls; an additional interrupted stream checks SDK error parsing. Authentication and invalid-input failures make no calls. See examples/anthropic-messages.mjs. Client SDK retries are distinct from Router reselection.

Messages→OpenAI differs in thinking budgets, signed history, initial usage, stop metadata and effort vocabulary. These fake-upstream SDK results do not establish every OpenAI semantic or every online model. Native BigModel checks below are not evidence for this conversion direction.

Before Bifrost accumulates Messages SSE state, serialized decoded frames are bounded to 16 MiB and 65536 frames; each raw capture is at most 16 MiB. Text, thinking, tool arguments/names and IDs all count. Overflow fails and cancels upstream without truncating into success. Each tool argument is also limited to 1 MiB. These bounds cover accumulated adapter/converter data, not transient allocation while parsing one huge raw SDK frame.

Routing summaries come from the original last Messages user message, not the merged Chat projection; tool-result/image-only messages yield an empty summary. JSON/SSE reject duplicate tool IDs. Parallel calls within one frame enter the converter individually; terminal conversion follows all tool fragments, preserving IDs, indexes and arguments.

After text/tools close an SSE thinking block, resumed nonempty thinking causes an in-stream error without message_stop: the converter cannot reopen that block and must not silently discard fragments. Empty thinking deltas do not trigger this failure.

## OpenAI clients with native Anthropic upstream (#50)

BigModel server-executed `server_tool_use` blocks and their associated `tool_result` blocks are validated and consumed by the adapter in JSON and SSE responses. They are not emitted as client `tool_calls`, and their raw results are not added to assistant text. Subsequent text, reasoning, ordinary client tool calls, and usage remain available. Orphan, duplicate, or missing server results fail validation; unknown block types and `pause_turn` remain unsupported.

Set provider protocol to `anthropic`. JSON and SSE use the same checked Bifrost conversion for messages, leading system/developer instructions, HTTPS/data images, ordinary function tools and tool results. Leading developer text maps to the same instruction level as system, preserving order. Mid-conversation instruction insertion, unsupported image detail and unrepresentable OpenAI fields are rejected before selection. JSON Schema requires the final native schema to preserve constraints; json_object is rejected. Sampling fields are rejected if the SDK would drop them, including model-name dependent rewrites. Tool-history JSON numbers retain their original precision, including integers beyond float64 exact range. Numeric compatibility checks compare exact values; schema constraints rounded by SDK conversion are rejected before sending. The prepared native body is sent through Bifrost’s public passthrough transport, preventing its Chat serializer from stripping unsigned thinking history. Responses still use Bifrost converters; SSE framing and terminal validation run before conversion. No SDK retries occur. Auto may reselect once before semantic output; explicit requests never switch models.

Thinking true maps to adaptive; false and effort none normalize to disabled. Tools can use default or enabled thinking on initial and continuation requests. Low/medium/high/max effort is preserved; minimal/xhigh and conflicting controls are rejected. Visible thinking is preserved in reasoning_content, including unsigned thinking alongside tools. Assistant reasoning_content is replayed as an unsigned native thinking block. By default, signed thinking with tools and encrypted thinking remain unsupported and fail explicitly; signatures are never fabricated. The verified BigModel profile is a narrow exception: HTTPS `open.bigmodel.cn/api/anthropic/v1` (default port or 443), with upstream model `glm-5.3` or `glm-5.3-flash`, accepts thinking/tool replay without response signatures. For these targets only, the adapter preserves visible thinking text and omits the optional signature. It does not claim native signature fidelity; proxies, other paths/models, and encrypted thinking are not covered. Standalone signed thinking retains its visible text only and does not provide a signed-history roundtrip.

The default native max_tokens and routing reserve are 65536, while OpenAI retains its existing behavior. Native prompt usage includes ordinary input plus reported cache reads and writes; cumulative output usage is not added twice. Missing usage is not fabricated. Successful SSE completion requires a validated message_stop followed by response-body EOF. Malformed/unknown events, bad tool indexes or JSON, and events after message_stop fail without a successful finish_reason/[DONE]. Stream reads are bounded to 16 MiB total and 1 MiB per frame.

Native errors retain HTTP 400–599 status and stable local summaries; sanitized upstream bodies are available in diagnostic.upstream_error for JSON and SSE. Parsing/network failures use 502, timeouts 504, cancellation 499. Raw provider messages and IDs do not override Router errors or request IDs. Recognized quota/content/authentication/rate-limit codes may refine the safe diagnostic classification; unknown codes are omitted from diagnostics. Chat and Messages errors share these optional diagnostics. OpenAI SSE error frames are captured at the SDK reader boundary because the SDK otherwise discards their raw bodies.

Local production HTTP/Bifrost tests and unmodified OpenAI/Agent SDK tool roundtrips cover JSON/SSE. Reproduce with `JEV_TEST_OPENAI_SDK=<Node project with openai> JEV_TEST_AGENT_SDK=<agent-sdk checkout with tsx> go test ./internal/transport/http -run 'TestChatAnthropicUpstreamHTTP|TestNativeUpstreamSDKs' -v`. These use a simulated native upstream. BigModel real-upstream checks used Agent SDK v3.5.1 through a local Router, covering simple queries, multi-turn history, custom tools, streaming, streaming tools and effort controls on glm-5.3 and glm-5.3-flash. All 12 example runs completed; this is a bounded compatibility check, not a benchmark or validation of all Anthropic-compatible deployments. Successful effort requests do not prove distinct internal reasoning behavior. Per-run logs remain local. Native Messages client support is documented separately below.

Native Anthropic JSON Schema does not support OpenAI `json_schema.description`; nonempty descriptions fail candidate checks with `unsupported_request` instead of being silently lost. Format names are not sent. Schemas use native constrained output; `strict` is not sent as a native field.

## Native Messages clients with native Anthropic upstream (#51)

`POST /v1/messages` supports `protocol=anthropic` and `protocol=openai` targets for explicit and auto selection. Native prepared JSON is immutable; candidate checks and execution share its body builder. Only the request model becomes upstream_name and response model becomes the external ID. Native requests/responses never traverse Chat conversion. Unsupported fields exclude only the incompatible candidate; internal errors abort planning. There is no SDK retry, image download or signature rewriting. Auto may reselect once before semantic output.

| Native field | Contract |
| --- | --- |
| Text/system/images/tools | Original block order and raw tool numbers preserved; HTTPS/base64 images are forwarded, not downloaded. Tool results preserve text/image content and is_error, and require unique matching IDs. |
| Thinking | disabled/adaptive/enabled preserved. Enabled budget is an integer ≥1024 and below max_tokens. Signed thinking and redacted_thinking.data remain opaque. Strict native targets require nonempty history/response signatures; only the existing exact BigModel HTTPS endpoint and glm-5.3/glm-5.3-flash profile permits unsigned thinking. |
| Cache | ephemeral on tools, system text and message text/image blocks; ttl omitted/5m/1h, at most four breakpoints; tools→system→messages order requires 1h before 5m. No top-level cache or thinking/redacted cache. |
| Controls | max_tokens is required (16–10000000); temperature/top_p/top_k, stop_sequences, metadata.user_id, tool_choice, parallel control and output_config effort/JSON schema are preserved. Model support remains upstream-owned. |
| Usage | Preserve native counters, missing fields versus zero, nested cache details and bounded optional extensions. Never add cache counts into input_tokens or sum cumulative message_delta counts. Known counters must be nonnegative integers. |
| Stops | end_turn/max_tokens/stop_sequence/tool_use/refusal; stop_sequence must match a requested sequence. Unknown/pause_turn and unsupported content types fail. |

Native streams preserve event JSON and order (including resumed thinking and signature fragments); only message_start.message.model changes. Bounds are 16 MiB each for compressed/decompressed bytes, 1 MiB per frame and 65536 frames. A valid message_stop is held until clean body EOF; malformed tails, gzip truncation, missing terminal events and errors never produce successful termination. Error bodies and upstream request IDs are not forwarded; HTTP and SSE share the safe error classification above.

At max_tokens, only the last tool block may contain an unfinished but completable JSON object prefix, including empty argument deltas. Earlier tools must be complete; invalid syntax, duplicate keys/IDs and complete non-objects still fail. JSON responses must be syntactically complete and tool input must be an object, but missing business-required properties are preserved. Router never repairs or executes arguments. Clients must inspect stop_reason before using a tool input.

Decision projection excludes thinking/signatures/redacted, tool arguments/results, cache and image data/addresses. The independent `native_messages_bytes_v1` estimate counts full content plus framing/image reserves and max_tokens once; it is heuristic. Explicit selection skips estimation. Auto retains the existing largest-context overflow fallback and has no model affinity: signature authenticity and cross-provider replay acceptance remain upstream responsibilities. Use an explicit stable model when replay compatibility matters.

Beta headers, server tools, documents/audio/video, citations, unknown input fields, duplicate JSON keys and invalid UTF-8 are rejected. Native support does not widen the separate OpenAI Chat conversion contract.

Reproduce production HTTP and unmodified official SDK acceptance:

```sh
JEV_TEST_ANTHROPIC_SDK=/absolute/path/to/node-project go test ./internal/transport/http -run TestNativeMessages -count=1 -v
```

SDK 0.52.0 passes auto/explicit × JSON/SSE signed thinking, redacted history, images, cache and is_error tool replay with maxRetries=0. Low-level streaming preserves max_tokens and incomplete partial_json. Its high-level finalMessage aggregates the tested incomplete input to `{}`; missing usage causes its high-level helper to throw, while JSON/low-level events preserve absence. These SDK behaviors do not alter Router output. Mock signatures establish preservation only; real Claude signature authenticity remains unverified.

Real native Messages acceptance used a temporary local Router and official Anthropic SDK 0.52.0 against glm-5.3/glm-5.3-flash: two 14-call runs completed JSON/SSE text and tools/continuation, plus Flash image analysis. The replay run reused complete original assistant blocks and verified returned tool-result text. Flash identified the synthetic red square, blue circle and green triangle in both modes. GLM-5.3 text emitted signed thinking; these low-effort tool turns and Flash responses did not emit thinking. Cache counters were zero; cache hits and real Claude signature authenticity are not established. No production configuration was changed.

## Automatic effort (#58)

Model `capabilities.reasoning` supplies effort levels for Auto parameter adaptation.
There is no additional opt-in configuration. One Jev call can contain independent model and effort
questions; task effort is not conditioned on the model answer. Select the highest
allowed level at or below the target, falling to the lowest allowed level when
necessary. Models without usable effort declarations omit injection. Auto rejects manual
effort, disabled thinking and fixed budgets with HTTP 400; use an explicit model
to retain caller controls. See the [management contract](../contracts/README.md#automatic-task-effort)
for configuration, singleton behavior and upgrade rules.

Chat and native Messages JSON/SSE paths apply effort to full request copies.
Chat-only `route.inspect` cannot represent native Messages thinking budgets or
signed history; native acceptance uses full HTTP requests and upstream captures.
Local mocked end-to-end tests cover both input protocols against OpenAI and
Anthropic upstreams. They do not establish real Jev selection quality or upstream
model support; live multi-question validation requires usable decision credentials.


## Local Laya System One service

The pinned `convaiinnovations/laya-multilingual` service supports choice-only
model/effort decisions, with one or two questions and no Cartesian product.
The parser ceiling of 64 options is not a fit guarantee. The 256-token head
budget and SDK per-option cap are independent of the 8192 context ceiling.
Actual Router six-effort input fits unchanged; synthetic 20/40-model probes exceed
the head budget and are rejected rather than truncated.

Short real-weight inference has been verified on macOS arm64 and native Linux
arm64, including a network-disabled container with read-only weights. This does
not verify amd64, full-length latency, routing quality, or sensitive-session
protection. Agent integration exercises CLI configuration/readback, inspection,
Chat and native Messages generation, local-decider fallback, and explicit-path
availability while the decider fails. Generation in those tests is controlled,
not a paid-provider quality benchmark.

See the [service guide](../services/laya/README.md) for deployment and current
publication status. Preserve the existing hosted decision backend when the local
service is not explicitly configured; there is no implicit backend switching.
