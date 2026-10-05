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

Edit `prompt` to adjust preferences; the default is one balanced template. `auto` evaluates each request independently. Explicit selection uses an enabled external model ID and still obeys compatibility and context limits. Failures do not trigger fallback or retries.

Jev mode offers no sensitive-data routing guarantee. Image fields are excluded from the decision call, but conversation text and tool content may go to Jev. Local Laya, privacy session locking, ArbiterOS are future capabilities, not available in this version. Disabling a model affects new requests; it does not revoke an in-flight snapshot.


## Reasoning controls

Discover the current `models.put` and `route.inspect` schemas first. Chat requests support `chat_template_kwargs: {"enable_thinking": true|false}` and `reasoning_effort`; template kwargs cannot contain arbitrary provider overrides. SDK `thinking: {type: "enabled"}` becomes `chat_template_kwargs.enable_thinking=true` on the wire.

No `capabilities.reasoning` declaration is required. Valid thinking and effort controls are forwarded unchanged, alone or together; the upstream decides whether they are supported. Existing reasoning metadata remains readable/writable for compatibility but does not gate requests or filter candidates.

Validate upstream behavior against the actual endpoint/model in JSON and SSE. Neither `auto` nor explicit selection requires reasoning metadata. Inspect uses the same checks as generation, and Jev receives the reasoning requirements within its input budget. No field dropping, reasoning downgrade, retry or fallback is used. Output limits are preserved independently of these controls. Responses preserve text `reasoning_content`, including thinking-only SSE events; assistant history can replay this text. Signed/encrypted reasoning and the Anthropic Messages API remain outside this capability.

## Human dashboard

When the user wants the management page, run `jev-router dashboard` against the running instance; use `--url` for an HTTP(S) root address or `--no-open` to return its URL without network access or a browser. It does not start the server. `serve` provides `/dashboard/` with embedded assets.

The user enters the settings credential in the browser. Never add it to a URL or browser-opening command. The page exchanges it once for a 7-day HttpOnly session; refresh and same-credential restart restore login. Logout revokes the request session. CLI continues to use Bearer credentials. Remote browser use requires HTTPS and a configured public `dashboard_origin`; consult [deployment and recovery](../../docs/configuration.md#dashboard-sessions). Runtime `call.protocol.session` describes the authentication endpoints; never silently retry a failed write or a stale-session logout. The UI reads status, models, prompt and records, and edits descriptions and the prompt. Continue to use runtime schema and CLI for other management operations.

### Decision budget

Startup `decision_max_bytes` (or `JEV_ROUTER_DECISION_MAX_BYTES`, env takes precedence) configures the serialized Jev request budget; restart to apply. Default: 32000 bytes, a conservative local heuristic informed by Jev's documented 32k-token state-plus-longest-question limit, not an official byte recommendation or exact tokenizer count. Larger values require upstream validation. Oversized system/developer and latest-query text segments are independently middle-truncated, preserving both ends with [...truncated...] and content_truncated. A shared text cap is fitted against serialized bytes; short segments stay intact. Only metadata plus minimum head/tail context overflow still fails; generation inputs are never truncated. This applies to multi-candidate auto and route.inspect.

Jev never receives tool definitions. Tool history contains only paired execution records (call IDs, tool names and result references), without arguments, outputs or accompanying execution-message text. Recent complete query groups fit within the budget; older groups may be omitted. The generation upstream receives the full original request.

## Routing records

Discover `records.list` for paginated routing records. Use `offset` and `limit` for numbered pages, or continue with `next_cursor`; do not combine a nonempty cursor with a nonzero offset. `total` is the currently retained count; new writes and cleanup can change pages between calls. `decision_ms` measures selection only, not generation. `request_summary` retains at most 120 Unicode characters of text from the last user message, without an extra model call; missing summaries are normal for older records or messages without text. Records default to seven days and 100000 entries, discarding the oldest first. Configure `retention_days` / `JEV_ROUTER_RETENTION_DAYS` and `record_max_count` / `JEV_ROUTER_RECORD_MAX_COUNT`, then restart. Writes remain best effort, with degradation reported by `status.get`.

### Generation context estimate

Inspect exposes optional `context_estimate` with semantic_bytes_v1 component accounting, separate input and output reserve, and total. This is heuristic (`context_exact=false`), independent of Jev decision bytes. Unspecified output reserves 4096 locally without injecting a limit upstream. If every otherwise eligible auto candidate exceeds capacity only by inexact estimation, `context_estimate_fallback` selects the largest context window (ties by model ID). Capability checks remain enforced; explicit models never switch. Generation requests remain complete and upstream errors do not trigger retries.
