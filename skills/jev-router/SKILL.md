---
name: jev-router
description: Configure and inspect Jev Model Router through its CLI. Use when managing providers, model cards, routing preferences, connection tests, or automatic and explicit model selection.
license: Apache-2.0
metadata:
  protocols:
    - cli
---

# Jev Model Router

## Discover

Run `jev-router --help` and `jev-router --version` locally. Use `jev-router schema` against the target instance to discover available capabilities, then `jev-router schema <capability>` for its input, effects, role, examples and risk. Discovery requires the settings credential, resolved from the configured env/file reference. Keep credentials out of command arguments and output.

## Configure and verify

1. Read the relevant resource and version with `call <capability> --json <file|->`; simple reads also accept `--id`, `--cursor`, `--limit`.
2. Prepare a complete replacement resource using its runtime schema. Use a server-resolved `secret_ref`, or submit write-only `api_key` when the instance schema supports managed credentials. Give models task-oriented descriptions and explicit capability declarations. External ID `auto` is reserved, including for disabled models.
3. Write with `--expected-version N --idempotency-key KEY`. On an unknown network outcome, retry the identical operation with the same key within 24 hours. A changed payload needs a new key; a version conflict needs a fresh read and a deliberate update.
4. Create models disabled, run `models.test` with only their ID, then enable through `models.put` if authorized. The fixed test checks basic connectivity with `Reply OK.` and 16 output tokens; it does not prove all declared capabilities.
5. Use `route.inspect` with a supported chat request to inspect selection. It generates no answer and changes no configuration; multiple candidates can invoke paid Jev. Read back saved resources and report the observed version and operation ID.

Machine results go to stdout; diagnostics go to stderr. A top-level successful `models.test` operation can contain `data.ok=false`; inspect both levels. Distinguish saved configuration, successful connectivity and successful generation.

## Managed provider credentials

Discover `providers.put` on the target server first: v0.1.1 does not support inline keys. The operator configures an encryption master key once; subsequent provider-key writes and replacements need no restart. Supply exactly one of `api_key` or `secret_ref` via a protected JSON file or stdin, over HTTPS (loopback HTTP permits local development and SSH tunnels). Keep key values out of command arguments and diagnostics. Responses contain an opaque managed reference; preserve it for metadata-only updates, and never copy it to a different provider or decision configuration.

Use the existing version/idempotency workflow for rotation too. A successful write returns no plaintext key. Read back masked status and explicitly run `models.test`; saving does not test upstream connectivity. Retained encrypted revisions keep in-flight requests on their captured credential. Deletion is not emergency revocation. Master-key changes require a separate migration, not this workflow; see [deployment and recovery](../../docs/configuration.md#managed-provider-credentials).

## Routing boundaries

Edit `prompt` to adjust preferences; the default is one balanced template. `auto` evaluates each request independently. Explicit selection uses an enabled external model ID and still obeys compatibility checks; the upstream decides context capacity. Failures do not trigger fallback or retries.

Jev mode offers no sensitive-data routing guarantee. Conversation text may go to Jev. Images, tool definitions, tool arguments and outputs are excluded; tool IDs, names and result associations remain. Local Laya, privacy session locking, ArbiterOS are future capabilities, not available in this version. Disabling a model affects new requests; it does not revoke an in-flight snapshot.


## Reasoning controls

Discover the current `models.put` and `route.inspect` schemas first. Chat requests support `chat_template_kwargs: {"enable_thinking": true|false}` and `reasoning_effort`; template kwargs cannot contain arbitrary provider overrides. SDK `thinking: {type: "enabled"}` becomes `chat_template_kwargs.enable_thinking=true` on the wire.

No `capabilities.reasoning` declaration is required. For OpenAI-compatible upstreams, valid thinking and effort controls are forwarded unchanged, alone or together; the upstream decides whether they are supported. Existing reasoning metadata remains readable/writable for compatibility but does not gate requests or filter candidates.

Validate upstream behavior against the actual endpoint/model in JSON and SSE. Neither `auto` nor explicit selection requires reasoning metadata. Inspect uses the same checks as generation, and Jev receives the reasoning requirements within its input budget. No field dropping, reasoning downgrade, retry or fallback is used. Output limits are preserved independently of these controls. Responses preserve text `reasoning_content`, including thinking-only SSE events; OpenAI-compatible upstream history can replay this text. Native signed-tool responses have only the narrow BigModel exception described below; encrypted reasoning remains unsupported.

## Anthropic Messages

Configure an official Anthropic SDK with the Router root base URL, inference API key, and `model: "auto"` or an enabled external model ID. `POST /v1/messages` requires `anthropic-version: 2023-06-01` and JSON, and accepts inference `x-api-key` or Bearer. Both headers together must authenticate the same identity and role; settings credentials and dashboard cookies do not authorize inference. Use `max_tokens` within 16–10000000.

Text/system, HTTPS or base64 images, ordinary tools and tool result continuation, JSON/SSE, sampling, structured JSON Schema output, `thinking: {type: "adaptive"|"disabled"}` and explicit `output_config.effort` are implemented for the published combinations. The current generator still uses an OpenAI-compatible upstream. `thinking.enabled.budget_tokens`, native signed/redacted thinking, cache control, server tools and beta features are rejected; do not silently change a caller's request to make it fit. Bifrost's replay marker is bookkeeping, not a Claude integrity signature.

SSE starts with unknown usage as `{}` and updates reported counts at the end; missing counts are never fabricated. This differs from strict Anthropic initial-usage requirements and has been tested with official SDK 0.52.0. A request with stop sequences fails if the upstream stop reason cannot identify whether/which sequence matched. SSE also has a 16 MiB decoded-frame total and 65536-frame limit; exceeding either fails explicitly without successful truncation. Errors use Anthropic shapes with Router `request-id`; midstream errors never emit a successful message_stop. Router does not retry; official SDK clients may retry by default, so set maxRetries/max_retries to 0 when proving a single upstream call. See [compatibility and limitations](../../docs/compatibility.md#anthropic-messages-issue-23) and [example](../../examples/anthropic-messages.mjs). This local SDK evidence does not establish compatibility with every deployment or model.

## Human dashboard

When the user wants the management page, run `jev-router dashboard` against the running instance; use `--url` for an HTTP(S) root address or `--no-open` to return its URL without network access or a browser. It does not start the server. `serve` provides `/dashboard/` with embedded assets.

The user enters the settings credential in the browser. Never add it to a URL or browser-opening command. The page exchanges it once for a 7-day HttpOnly session; refresh and same-credential restart restore login. Logout revokes the request session. CLI continues to use Bearer credentials. Remote browser use requires HTTPS and a configured public `dashboard_origin`; consult [deployment and recovery](../../docs/configuration.md#dashboard-sessions). Runtime `call.protocol.session` describes the authentication endpoints; never silently retry a failed write or a stale-session logout. The UI reads status, models, prompt and records, and edits descriptions and the prompt. Continue to use runtime schema and CLI for other management operations.

### Decision budget

Startup `decision_max_bytes` (or `JEV_ROUTER_DECISION_MAX_BYTES`, env takes precedence) configures the serialized Jev request budget; restart to apply. Default: 32000 bytes, a conservative local heuristic informed by Jev's documented 32k-token state-plus-longest-question limit, not an official byte recommendation or exact tokenizer count. Larger values require upstream validation. Oversized system/developer and latest-query text segments are independently middle-truncated, preserving both ends with [...truncated...] and content_truncated. A shared text cap is fitted against serialized bytes; short segments stay intact. Only metadata plus minimum head/tail context overflow still fails; generation inputs are never truncated. This applies to multi-candidate auto and route.inspect.

Jev never receives tool definitions. Tool history contains only paired execution records (call IDs, tool names and result references), without arguments, outputs or accompanying execution-message text. Recent complete query groups fit within the budget; older groups may be omitted. The generation upstream receives the full original request.

## Routing records

Discover `records.list` for paginated routing records. Use `offset` and `limit` for numbered pages, or continue with `next_cursor`; do not combine a nonempty cursor with a nonzero offset. `total` is the currently retained count; new writes and cleanup can change pages between calls. `decision_ms` measures selection only, not generation. `request_summary` retains at most 120 Unicode characters of text from the last user message, without an extra model call; missing summaries are normal for older records or messages without text. Records default to seven days and 100000 entries, discarding the oldest first. Configure `retention_days` / `JEV_ROUTER_RETENTION_DAYS` and `record_max_count` / `JEV_ROUTER_RECORD_MAX_COUNT`, then restart. Writes remain best effort, with degradation reported by `status.get`.

### Generation context estimate

Inspect exposes optional `context_estimate` with semantic_bytes_v1 component accounting, separate input and output reserve, and total. This is heuristic (`context_exact=false`), independent of Jev decision bytes. For OpenAI upstreams, unspecified output reserves 4096 locally without injecting a limit; native Anthropic targets send and reserve 65536. If every otherwise eligible auto candidate exceeds capacity only by inexact estimation, `context_estimate_fallback` retains all candidates with the largest context window: one is selected directly, while multiple ties go through one Jev decision; the path remains context_estimate_fallback. Capability checks remain enforced; explicit models skip local context estimation/admission entirely and never switch; capacity is delegated to the upstream. Explicit inspect omits context_estimate and leaves context_exact=false (not counted). Generation requests remain complete and upstream errors do not trigger retries.

## Native Anthropic upstreams

Discover and preserve `Provider.protocol` on replacements: `openai` is the default; `anthropic` selects native Messages upstream for OpenAI Chat clients. Use an API-prefix base_url such as `https://api.anthropic.com/v1`; the adapter adds `/messages`. Read the derived endpoint with providers.get, and test connectivity before enabling a model. Omitted max_tokens/max_completion_tokens becomes 65536 on native targets, including routing capacity reservation.

Native tools support default or enabled thinking on initial and continuation requests, with assistant reasoning_content replayed as unsigned thinking. True means adaptive; false or reasoning_effort=none means disabled. Low/medium/high/max effort remains exact; minimal/xhigh and conflicting controls are rejected. Upstream models may reject disabled thinking; do not force it for tools. Signed thinking with tools is rejected except for the verified official HTTPS open.bigmodel.cn/api/anthropic/v1 endpoint (default port/443), upstream glm-5.3 or glm-5.3-flash: visible thinking is retained and the optional signature omitted. Other hosts, proxies and models do not inherit this exception; encrypted thinking remains unsupported. Inspect and generation reject fields the native adapter cannot preserve before selecting that candidate. Native `/v1/messages` clients targeting native Anthropic upstream are **planned**, tracked by #51; do not configure or report that direction as available. Local mocked SDK tests do not establish real upstream compatibility. The two BigModel models passed 12 original Agent SDK examples through a local Router; see the [compatibility and validation scope](../../docs/compatibility.md).

### Browser Playground

Runtime discovery exposes `call.protocol.playground` for a browser-only, rate-limited text generation experience. Open the dashboard and use its Settings Cookie session; this is not a capability callable through `jev-router call`, and Settings Bearer does not grant generation access. Existing CLI/SDK inference remains unchanged.

Playground makes real decision/generation calls and can incur charges. Discover the effective limits through its authenticated status endpoint. Do not bypass exhausted Playground quotas by switching credentials, sessions, or endpoints. Do not automatically retry a generated request after cancellation, a 429, or a partial stream.

