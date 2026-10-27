# Management capability contracts

`management.json` and `schemas/` are the shared source for server validation and runtime discovery. `GET /admin/v1/schema`, `GET /admin/v1/schema/{capability}` and `POST /admin/v1/call/{capability}` require Settings credentials. The UI reuses these capabilities to inspect status, descriptions, prompts and routing records, and edit descriptions/prompts. Playground uses the separate Cookie-only streaming capability defined below.

## Minimum capabilities

| Goal | Result and effects |
| --- | --- |
| Discover capabilities | Return versions, descriptions, schemas, authentication, risk and availability |
| Read status | Return instance and dependency status without exposing credentials |
| List/read models and providers | Paginate and filter by permissions; return descriptions and capabilities |
| Update model/provider configuration | Validate and atomically publish a configuration version |
| Read/update routing prompts | Read the current version and save edits; provide one default balanced template |
| Inspect routing | Return selection without generation or session changes; may call the decision API |
| Test connectivity | Send a fixed minimal generation request to a specified model, including disabled models; accept no arbitrary conversation, never enable automatically, and disclose cost/data transmission |
| Query routing records | Return bounded paginated records, including a user-text summary of at most 120 characters, without full conversations or free-form decision reasons |

Initial management capabilities use trusted administrator `settings` credentials. `inference` provides model lists and generation for all enabled models in the instance. These roles separate operations, not mutually untrusted tenants.

Configuration changes replace resources and validate the complete configuration before publishing a global version. Saving does not initiate network checks. Writes carry `expected_version` and an idempotency key. Mutation, version increment and the idempotent result commit together. Successful receipts remain replayable for 24 hours across restarts. Replay lookup precedes version validation. Validation failures consume no key; corrected retries still check the version. Version and idempotency conflicts use `config_conflict` and `idempotency_conflict` respectively.

CLI `--help` works offline; `schema` discovers actual instance capabilities. Complex input supports JSON files or stdin. Results default to JSON, logs use stderr, and the CLI neither prompts interactively nor hides retries. CLI/HTTP and end-to-end tests cover these behaviors.

## Model ID constraints

`auto` is reserved for automatic routing and cannot identify an enabled or disabled generation model. Machine schemas and server validation reject creation or public-ID changes using it before any configuration/version mutation. This restriction does not apply to upstream model names. Schema, transaction and runtime tests cover the reserved name. Initial IDs are stable: put creates or fully replaces by ID, with no rename operation.

## Required contract metadata

- Stable capability ID, description, input/output JSON Schema, parameter constraints, required fields and examples.
- Authentication/scopes, effects, possible cost/data transmission, L0–L3 risk and approval responsibility.
- Synchronous/asynchronous result semantics, `operation_id`, stable error code, `retryable` and recovery guidance.
- Pagination and bounded output, write idempotency keys, expected configuration versions and conflict rules.

Risk reflects actual impact; configuration writes are not universally low risk. New data destinations, relaxed sensitive-data protections or permission changes require stricter execution boundaries than description edits. Define and disclose approval mechanisms before implementation; never fabricate approval handles.

Read-only schema/status operations do not call paid models. Route inspection is not a purely local, side-effect-free preview. Declare an asynchronous capability available only after status queries and terminal states are implemented.

## Response conventions

Management results contain `ok`, `data` or `error`, `warnings` and `meta`. Meta includes operation ID, timestamp and schema version. Initial management operations are synchronous, with no asynchronous event protocol. The CLI maps structured errors to stable nonzero exit codes documented in the contract.

These conventions apply only to management. `/v1/chat/completions` retains its JSON/SSE format, `/v1/messages` uses Anthropic JSON/SSE, and `/v1/models` retains its model-list format. Public model lists contain no provider credentials or management metadata.

Reference: [AgentUse 0.2.0 protocol](https://www.zerone.run/zh/protocol). This directory does not claim protocol certification.

## Request and output schemas

The call body is `{"input":{...},"expected_version":1,"idempotency_key":"operation-key"}`; reads require only input. A capability's input_schema validates input, while output_schema describes successful response data. Writes return version/resource. For models.test, data.ok reports the probe outcome; top-level ok reports management-operation completion. schemas/results.json defines errors and exit codes.

Risk is metadata for callers, not a built-in approval engine. Settings users are trusted administrators; clients act on user authorization. schema/status makes no upstream calls. route.inspect/models.test descriptions disclose cost and external transmission.

Each runtime capability includes `call.method`, `call.path`, complete `call.schema` and `call.protocol`. schemas/call.json defines the write envelope and validates the boundary; capability input_schema is embedded in call.schema.input. call.protocol documents global-version recovery, successful replay ordering/identity scope/24-hour expiry, failure without key consumption, and shared HTTP/CLI error mappings. Idempotency expiry and server calculations share results.json. Querying one capability schema is sufficient to learn complete call and retry rules.

Prompt lengths consistently use JSON Schema maxLength Unicode character counts, capped at 16384. Full-configuration saves reuse prompt.put validation. Jev serialized-byte budgets are independent: a valid saved prompt may still exceed the multi-candidate decision budget and return budget_exceeded.

`providers.get` returns read-only endpoint and `api_key_masked`. Keys longer than eight characters expose only the first four plus `***`; shorter keys are fully masked, and unresolved keys return null. Full keys never enter management responses. Masked fields are not configuration inputs.

## Managed credentials

`providers.put` and `decision.put` accept exactly one of `api_key` and `secret_ref`. api_key is write-only, nonempty UTF-8, at most 16384 bytes and contains no CR/LF/NUL. Schema maxLength also limits characters; shared runtime validation enforces the stricter byte limit. Writes require a server master key. Credentials, resource configuration, global version and sanitized idempotent results commit together. Responses contain immutable `managed:<revision-id>` references. Provider details show masked prefixes; resolution failures return null and a `provider_credential_unavailable` warning. Provider references must belong to that provider; decision credentials bind to an independent decision-backend identity. References cannot cross these owners. Decision reads return only references, never plaintext.

Inline-key writes use a master-key-derived HMAC digest for idempotency. SQL, receipts and configuration snapshots store no plaintext. Missing or undecryptable keys return generic `config_missing`, without plaintext fallback. Provider rotation does not change credential revisions captured by in-flight requests. Historical ciphertext remains; deletion is neither physical erasure nor emergency revocation. The CLI rejects inline keys over non-loopback HTTP and does not follow redirects. Operators configure HTTPS reverse proxies.

## Dashboard session authentication

`schemas/session.json` defines authentication endpoints, lifetime, capacity, cookies and browser-origin requirements, published under `call.protocol.session`. Authentication endpoints are not configuration capabilities and consume neither versions nor idempotency keys. The CLI retains Settings Bearer authentication. Ordinary management accepts Cookie only when Authorization is absent; a valid cookie cannot override an invalid Bearer. Inference APIs require inference credentials. Chat and models retain Bearer; `/v1/messages` also accepts `x-api-key`, with both credentials required to be valid and share an identity/role if supplied together. Cookies grant no inference permission on these /v1 endpoints; Playground is the explicit exception below.

Login exchanges Settings Bearer plus an empty JSON object for an HttpOnly cookie and expires_at/csrf_token. Status restores CSRF; logout revokes only its request session. Cookie POST requests validate exact Origin, JSON and session-bound CSRF; GET requires X-Jev-Session: 1. Responses disable caching. Only successful login writes cookies. Status, logout and errors never clear them, preventing late responses from deleting another tab's new session. CSRF mismatches require explicit recovery and a new operation decision, never automatic replay. `session_limit` maps to HTTP 429 / CLI 6 with retryable=false.

## Model-list ordering

`models.list` defaults to ascending ID. `sort: "enabled_first"` places enabled models first, then disabled models, each group sorted by ID. Sorting precedes pagination. Keep sort unchanged and pass next_cursor unchanged across pages. A zero-based `offset` also permits direct navigation, but cannot be combined with a nonempty cursor. Responses include `total`. Dashboard defaults to 20 rows, offers 10/50/100 and uses enabled-first sorting. Configuration changes refresh the list from page one.

`models.list` supports live `query` filtering: case-insensitive, trimmed literal substring matching against model ID, description, provider/upstream names, location and visible status/capability labels. `language` is `en` by default or `zh`, selecting label wording. Filtering precedes sorting/pagination; total counts matches. Query changes restart at page one.

## Reasoning controls and model capabilities

`chat.json` and `route.inspect` validate formats and values of `chat_template_kwargs.enable_thinking` and `reasoning_effort`, rejecting unknown template fields. Valid controls pass unchanged to OpenAI-compatible upstreams without requiring declared capabilities. Native Anthropic maps thinking/effort through normalization rules and rejects combinations that cannot be preserved before candidate selection; see the [compatibility matrix](../docs/compatibility.md#openai-clients-with-native-anthropic-upstream-50). The upstream decides model support. Failures trigger neither parameter removal, downgrades nor model switching.

Optional `capabilities.reasoning` remains readable/writable for compatibility but is descriptive metadata only. It does not affect request validation or auto/explicit selection. Missing configuration, empty arrays or metadata differing from the request do not block control forwarding. Jev still receives reasoning requirements and counts them in its input budget. Chat JSON/SSE preserves textual reasoning_content and permits assistant-history replay; signed/encrypted reasoning extensions are outside this contract.

## Routing-record pagination and retention

`records.list` returns records, total and next_cursor. The default limit is 50, maximum 200. It supports zero-based offset or the existing cursor; nonempty cursor and nonzero offset cannot be mixed. Results follow reverse insertion order. Each query's count and rows share a database snapshot, but new writes/cleanup between pages may change contents; cross-request snapshots are not promised. UI defaults to 20 rows, offers 10/50/100 and page numbers, and returns to the last valid page after cleanup removes later pages.

`decision_ms` covers selection only: validation, filtering and any decision call. It excludes generation time; generation_ms is no longer emitted. `request_summary` extracts text from the last user message locally, collapses whitespace and limits it to 120 Unicode characters without another model call. Missing text and older records omit it. Tool messages, image addresses and earlier messages are excluded. Default retention is seven days and 100000 records, evicting the oldest by insertion order. Startup configuration controls both; see configuration.md.

Optional `route.inspect.context_estimate` returns semantic_bytes_v1 components and total input/output reserve. The context_estimate_fallback path means all eligible auto candidates exceeded estimated capacity and the largest-context fallback was used; it does not establish exact capacity. One largest candidate skips model selection but may still require an effort decision; ties send only those candidates to Jev once. The path remains context_estimate_fallback, retaining complete candidate_ids and decision usage.

Explicit-model route.inspect skips context estimation, omits context_estimate and sets context_exact=false to mean uncounted. It still validates request format, enabled state and capabilities.

## Anthropic Messages inference contract

`schemas/messages.json` defines POST /v1/messages input; provider performs shared validation and target checks. Native Anthropic targets retain thinking budgets, signatures/redacted content, tool-result is_error, cache, sampling and metadata. OpenAI candidates reject combinations that cannot be preserved. max_tokens is required, from 16 to 10000000; enabled budget_tokens must be at least 1024 and below max_tokens. Native bodies change only model, while complete requests are estimated separately; decision projections omit sensitive native content. The version is fixed to 2023-06-01 and beta is rejected. See the [native Messages contract](../docs/compatibility.md#native-messages-clients-with-native-anthropic-upstream-51).

This entry uses a separate Anthropic error envelope, including top-level type:error, error.type/message and request_id on authentication failures. request-id equals X-Request-ID. See [compatibility documentation](../docs/compatibility.md#anthropic-messages-issue-23) for 400/401/403/404/405/413/422/429/500/502/503/504/529 mappings and streaming failures. Messages does not use the management envelope; sanitized upstream details appear in error.diagnostic.upstream_error.

Generation providers accept `protocol: "openai" | "anthropic"`. Omitted protocol defaults to `openai`, including replacement writes; preserve it when editing an Anthropic provider. Reads return the effective protocol. Existing stored rows need no migration or version increment. `base_url` is the API prefix (for example `https://api.anthropic.com/v1`).

## Playground browser generation

`schemas/playground.json` is the single source for dedicated endpoints, input, stream events, authentication and default limits. It is published under call.protocol.playground, not an ordinary /call capability. GET /admin/v1/playground returns enabled state, effective limits and remaining session/instance/UTC-day quotas. POST /admin/v1/playground/completions accepts model, text user/assistant messages and stream:true; assistant messages may include protocol-required reasoning_content. Other fields are rejected; the server injects output limits.

Both endpoints accept only Dashboard Settings cookies. GET requires X-Jev-Session; POST requires JSON, exact Origin/Host and session-bound CSRF. Authorization cannot substitute for the cookie, and cookies gain no /v1 inference permissions. This is a restricted browser-generation exception for Settings; Settings Bearer and general management calls do not provide generation.

No upstream call occurs before admission. Accepted calls receive no cost/count refunds. Auto may reselect once under the fallback policy; both attempts share one admission lease and quota charge. POST uses the shared inference lifecycle instead of calling route.inspect before generation. Successful selection emits route immediately without waiting for the first upstream event. Later generation failures are SSE errors. Event order is route, delta, done/error; delta separates content and reasoning_content. Only normal termination after finish_reason emits done. EOF without a terminal event is interruption. This endpoint does not claim OpenAI SSE compatibility. Pre-header errors are JSON; in-stream failures are separate error events. Shared sanitization may expose provider error bodies as upstream_error details.

429 responses include playground_rate_limited, limit_scope, retry_after_seconds, reset_at when available, and Retry-After. Instance UTC daily quotas persist; concurrency is single-process. configuration.md lists all defaults. Routing displays contain only actual execution metadata, never generated Jev explanations.

Playground JSON/SSE errors provide stage (request/routing/generation), Router request_id and available upstream_status/allowlisted upstream_code. Summaries retain stable classifications. Optional upstream_error contains body (sanitized JSON or plain text), truncated and an available upstream request_id distinct from the Router ID. Public Chat/Messages JSON/SSE exposes the same details through error.diagnostic.upstream_error. Unknown provider codes may remain in details without becoming Router classifications.

Jev failures retain the same details, use routing stage in Playground, and emit neither a successful route nor generation calls. Nested JSON strings and JSON fragments with explanatory prefixes/suffixes are decoded and recursively sanitized. Malformed structures are omitted from their start to avoid exposing request echoes with unknown boundaries. Authentication fields, API keys, passwords, cookies, structured request echoes and the current provider credential are masked. URL scheme matching is case-insensitive; userinfo, queries and fragments are removed. Details enter neither routing records nor browser storage. Internal SDK diagnostics and raw requests are not sources of error bodies. Inputs above 64 KiB omit the body and mark truncated; sanitized output above 16 KiB returns a truncated text fragment. If the complete input cannot be inspected safely, no partial original text is shown. HTML is displayed only as text. Missing upstream bodies and network/parsing errors retain generic errors. Read timeouts remain timeout.

## Decision endpoint

`decision.put` supports optional `path`, appended to `base_url`; omitted means `/v1/systemone`. For third-party Jev-compatible services, configure `base_url`, `path` and `model` according to their API documentation. This is a full replacement: omitting a previously configured path restores the default. Authentication remains Bearer via managed `api_key` or external `secret_ref`; request and Choice response schemas remain native Jev. Paths allow slash-separated alphanumeric, hyphen and underscore segments only, without query or fragment.

## Generation health

`models.list` items and `models.get.resource` include optional read-only `health`:
state (`unknown`, `healthy`, `isolated`), nullable success/failure/next-probe times,
and safe failure classification. Missing health plus `health_degraded` warns of
unavailable observation/storage, not an unknown healthy candidate. Runtime schemas
expand the shared `schemas/health.json`; configuration write schemas remain unchanged.

`models.test` permits disabled/isolated targets, sends a fixed paid request and
updates health without changing enabled/configuration version. Top-level `ok`
means the operation completed; `data.ok` is the probe result. `data.persisted=false`
means health storage is degraded. `health_probe_in_progress` (409/CLI 5) is retryable;
`health_unavailable` (503/CLI 6) prevents unsafe automatic admission.

`route.inspect` returns `health_exclusions` for isolated auto candidates, including
in `no_candidates` error metadata. Exclusions are limited to 200 entries;
`health_exclusions_truncated=true` means further exclusions were omitted. Explicit inspection does not generate or recover.
The public Chat error can include these safe exclusions, never credential fingerprints.
Model lists remain enabled-model lists; explicitly selecting an isolated model is
allowed. The entire generation lifecycle shares one immutable credential snapshot
with its health identity and error redaction, across all supported entrypoints.


## Auto fallback

Enabled by default, Auto can make one additional selection and generation attempt after an eligible upstream failure, before any answer, reasoning, refusal or tool delta is forwarded. Failed model IDs are excluded from this request regardless of global isolation thresholds. A single remaining candidate skips model selection but may still require an effort decision; multiple remaining candidates return to Jev with a typed failure summary. Original input, configuration version and request ID are preserved. Explicit selection, decision failures, content/request errors, cancellation and downstream writes never trigger fallback.

Both attempts share a 180-second default total budget. Its internal cancellation cause is health-neutral; genuine independent generation-stage timeouts still count toward health. Cancellation is best effort and both attempts may incur charges. status.get exposes auto_fallback {enabled,max_retries,timeout_seconds}. records.list exposes optional attempts containing only safe execution metadata; old records remain readable.

Final errors retain their original protocol status and add diagnostic.attempts (at most two entries; combined diagnostics at most 32 KiB). fallback_exhausted means no eligible backup (HTTP 503 / CLI 6). Successful compatible responses contain only the final model and its reported usage. Detailed upstream bodies are sanitized and response-only, never persisted. Playground emits retry before replanning and route after each successful plan; one final done/error terminates the request. Inspect and health probes never execute fallback.

Response writes use the earlier of the request deadline and the stream idle deadline. Client cancellation interrupts blocked writes. Terminal timeout errors are best effort on writable connections and do not receive a fresh idle interval after the total budget expires. Playground uses the same write boundary, including when the Auto deadline is earlier than its own limit.

## Decision usage in routing records

`records.list` exposes optional `decision_usage` on each record and attempt.
`input_tokens` and `output_tokens` sum known decision usage, including failed
decisions and Auto reselection. `complete=false` means one or more invoked
decisions lack valid usage; the counts are only a lower bound, not a final cost.
`invocations` counts decision adapter calls, including local failures before
transmission, and is not a billable request count. Explicit and sole-candidate paths that do not require automatic effort
selection report zero invocations and complete zero usage. Historical records omit
this object: treat their decision cost as unknown, never zero.

These counts exclude generation usage. Use the actual inference request ID to
join records with generation results; a separate `route.inspect` call incurs
its own decision and cannot stand in for execution usage. Records remain best
effort; missing records or `records_degraded` prevent complete reconciliation.

## Automatic task effort

Models may declare `auto_effort: {"allowed": ["low", "high"]}` through
`models.put` and configuration snapshots. Arrays must be nonempty and unique.
Omission disables automatic injection; retain this field on replacement writes.
The automatic order is minimal, low, medium, high, xhigh, max. Anthropic providers
use the common adapter subset low/medium/high/max. Actual upstream model support
is declared by the operator, not inferred from a model name or connection test.
Informational `capabilities.reasoning` does not enable this feature.

Auto asks independent model and task-effort Choice questions in one Jev call.
The effort question judges the task, not the selected model or its allowed
levels. Resolve to the highest allowed level no greater than the answer; when
none exists, use the lowest allowed level. An unconfigured selected model gets
no injected parameter. Requested answers must be valid even if an unconfigured
model is chosen. A sole model with multiple levels asks effort only; a sole
model with one level applies it directly, and an unconfigured sole model bypasses
Jev. Both questions share existing request budgets and one invocation count.

Explicit model selection, caller effort, disabled thinking and fixed thinking
budgets suppress automatic injection. Explicit effort is never downgraded.
Enabled/adaptive thinking may coexist when the adapter preserves the combination;
automatic effort does not add a thinking switch. Each fallback attempt starts
from the original request and applies its own decision to a separate copy.

`route.inspect` remains Chat-format-only and returns optional `effective_effort`,
the final outgoing parameter, not measured computational effort. For Chat targeting
Anthropic, explicit none becomes thinking.disabled and therefore omits
effective_effort; the original caller control is still forwarded through conversion. Native Messages
fields are rejected, not converted into equivalent inspection inputs. Native
Messages is verified through full request execution and upstream captures.
No effort fields are added to routing records, public inference response metadata
or Playground events. Dashboard adds no effort editor or display; existing edits
preserve the hidden model configuration. Configuration belongs to CLI/Agents.
