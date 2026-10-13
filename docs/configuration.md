# Startup configuration

The management API persists runtime business configuration in SQLite. Environment variables and JSON configuration files control startup only. Precedence is `JEV_ROUTER_<UPPERCASE_FIELD>` environment variable > JSON configuration > default.

| JSON field | Default |
| --- | --- |
| listen | 127.0.0.1:8080 |
| database | .data/router.sqlite |
| settings_token_ref | env:JEV_ROUTER_SETTINGS_TOKEN |
| inference_token_ref | env:JEV_ROUTER_INFERENCE_TOKEN |
| encryption_key_ref | Empty (not required when using external credentials only) |
| decision_timeout | 10s |
| decision_max_bytes | 32000 |
| first_event_timeout | 60s |
| idle_timeout | 60s |
| record_timeout | 100ms |
| max_body_bytes | 16777216 |
| stream_buffer | 8 |
| retention_days | 7 |
| record_max_count | 100000 |

These initial defaults passed local boundary tests; they are not latency promises for real models. Durations accept Go duration strings. Numeric limits must be positive.

Both authentication tokens must exist and differ. Credential references support `env:NAME` or `file:/absolute/path`; trailing file newlines are removed. Settings identifies trusted administrators who can change generation destinations and must not be given to untrusted users. These two authentication tokens are not stored in SQL. Provider keys use external references or the encrypted managed mode below; status and errors never echo plaintext.

Ordinary disabling affects new requests only. Emergency stopping requires stopping the instance or revoking upstream keys. The default listener is local; operators must provide an appropriate TLS boundary when exposing it to a network.


CLI `--url` overrides `JEV_ROUTER_URL`; otherwise the startup listen address is used. Configuration files provide startup settings and CLI credential references only. The management API stores providers/models/decision/prompt. Jev base_url is a root address (for example `https://api.typesafe.ai`); generation provider base_url is an API prefix (for example `https://api.openai.com/v1`).

The official Jev configuration uses root address `https://api.typesafe.ai` and pinned model `jev-1.13.0`. The adapter appends `/v1/systemone`, so do not repeat `/v1` in decision base_url. The following `decision.put` example uses a key injected into the server environment:

```json
{"base_url":"https://api.typesafe.ai","model":"jev-1.13.0","secret_ref":"env:TYPESAFE_API_KEY"}
```

See the [official TypeSafe API documentation](https://docs.typesafe.ai/api) and [model versions](https://docs.typesafe.ai/models). OpenAI-compatible generation providers retain their own API prefixes, independently of decision-backend configuration.

Example startup JSON:

```json
{"listen":"127.0.0.1:8080","database":".data/router.sqlite","decision_timeout":"10s","first_event_timeout":"60s","idle_timeout":"60s"}
```

first_event_timeout covers stream establishment through the first valid SSE event, or the complete response for non-streaming calls and fixed connectivity tests. Afterwards, idle_timeout bounds event waits and downstream writes without a total duration limit for a healthy stream. Generation clients retain at most 16 provider-address/key snapshots. When full, additional targets are rejected explicitly instead of replacing in-flight configuration.

Each recording attempt waits at most record_timeout. `retention_days` (1–3650, default 7) limits age; `record_max_count` (1–10000000, default 100000) limits count. Their environment variables are `JEV_ROUTER_RETENTION_DAYS` and `JEV_ROUTER_RECORD_MAX_COUNT`, effective after restart. Each write transaction enforces the count limit by evicting the oldest entries in insertion order. Startup cleanup after lowering the limit deletes at most 1000 entries per batch. Expiry cleanup runs in batches at startup and hourly, so expired entries may remain until the next pass. Freed SQLite pages are reusable, but cleanup neither shrinks the file automatically nor guarantees a disk-byte limit. Background cleanup or record-write failures set status.get records_degraded and a warning until restart. Records are not transactional audits and are not lossless. The database is for one instance process; do not share one file across processes.


## Management dashboard

`serve` provides the same-origin `/dashboard/` UI without a Node runtime. `dashboard [--url URL] [--config file] [--no-open]` uses the same address precedence as the CLI; addresses derived from wildcard listeners map to local loopback. URLs must be HTTP(S) roots without credentials, queries or fragments.

Normal mode first performs an unauthenticated HTML reachability check, with a maximum five-second timeout and no redirects, then opens the system browser. Failure returns a JSON error and effective URL for manual opening. `--no-open` only returns the URL as JSON, without probing or starting a service. This command requires no Settings credential.

The browser separately accepts Settings credentials and exchanges them for a fixed seven-day HttpOnly session, recoverable after refresh or restart with the same credential. Explicit logout revokes its request session server-side. Authentication material enters neither URLs nor Web Storage. The UI accesses same-origin APIs only. Remote use requires HTTPS and a configured public Origin; local loopback HTTP is the exception. The UI does not manage accounts, keys or startup parameters. See Dashboard sessions below for lifecycle and recovery rules.

## Docker Compose

See [Quickstart](../quickstart/README.md) for the recommended deployment. The image listens on `0.0.0.0:8080` and stores SQLite at `/data/router.sqlite`. Compose publishes host `127.0.0.1:8080` by default and persists data in a named volume. The image runs as UID/GID 10001; custom bind mounts must allow that user to write the database directory.

Use the container CLI through `docker compose exec -T router jev-router …`, without installing Go on the host. Pass JSON through `--json -` and stdin; host paths do not automatically exist in the container. Compose `.env` supplies interpolation values only. Environment variables referenced by new credentials must also be explicitly injected into the service. File references require mounted files and container-absolute paths.

## Managed provider credentials

`encryption_key_ref` / `JEV_ROUTER_ENCRYPTION_KEY_REF` is an optional startup `env:` or absolute `file:` reference to a hex-encoded 32-byte AES-256-GCM master key. Configure it on the server only. Missing/empty environment material keeps legacy external-reference deployments working but rejects managed writes. Nonempty malformed keys, unreadable key files, unsupported references, or existing managed records without a matching key fail startup. All retained revisions are verified at startup, including records from deleted providers.

Quickstart uses `env:JEV_ROUTER_ENCRYPTION_KEY`; generate the value once with `openssl rand -hex 32`. Back up it separately from SQLite and restore the same key with the database. Master-key rotation/re-encryption is not implemented; replacing this value makes existing records unreadable. Upstream-key replacement through `providers.put` and `decision.put` is supported without restart.

`providers.put` and `decision.put` accept exactly one of `api_key` and `secret_ref`. Inline keys are nonempty UTF-8, at most 16384 bytes, with no CR/LF/NUL. SQLite stores versioned authenticated ciphertext bound to its provider or decision owner and immutable revision. Reads return references and masked status only; no reveal endpoint. `managed:` references belong either to one provider or to the decision backend and cannot be exchanged between these owners. Decision reads return only the reference, never the key. Retaining such a reference preserves the key on metadata updates.

Key revisions are retained so captured request snapshots and successful idempotent receipts remain valid. Deleting a provider does not erase historical ciphertext or revoke upstream keys; revoke at the supplier for emergency response. Credential history garbage collection is deferred. Encryption protects a database-only leak, not a compromised running server with access to the master key. Keep TLS termination and proxy request-body logging configured accordingly. Neither requests nor keys belong in logs.

## Dashboard sessions

The dashboard exchanges the Settings credential for a host-only HttpOnly, SameSite=Strict cookie scoped to `/admin/`. Sessions expire exactly 7 days after login, without renewal. Refresh/reopen and server restarts with the same Settings credential and origin preserve valid sessions. CLI continues to use Bearer authentication. No browser authentication material is written to Web Storage.

Set `JEV_ROUTER_DASHBOARD_ORIGIN=https://router.example.com` (or JSON `dashboard_origin`) for remote access. Use an origin only, with optional port. The environment overrides the configuration file. Without it only loopback HTTP browser sessions are allowed; remote CLI remains available. Remote origins require HTTPS; explicit HTTP is allowed only for localhost/literal loopback IPs.

A TLS reverse proxy must preserve the original Host. Keep the HTTP container backend private. Secure cookies follow the configured public HTTPS origin even over private HTTP; X-Forwarded-* does not grant trust. Browser requests require exact same-origin checks, with a session-bound CSRF header for every POST. No credentialed CORS is provided.

Logout revokes only its request session. Its now-useless cookie may remain until expiry or the next login: logout and error responses never write/clear cookies, so a delayed response cannot erase another tab's newer login. A stale tab's CSRF cannot revoke a newer session; reconnect explicitly without automatic write/logout retry. Network or storage failure does not confirm revocation; retain the session and retry logout. If login has an unknown outcome, check session status before another authentication action.

At most 128 active sessions are allowed. Expiry/logout frees capacity; replacement login can replace its own session at capacity. `session_limit` requires an existing logout or expiry, not automatic retries. Changing Settings or the origin policy on restart invalidates all sessions, including when an old Settings value is later restored. Inference-only rotation leaves sessions intact. When restoring a database backup, use a new Settings credential to invalidate backed-up sessions. Provider encryption keys are unrelated to session authentication.

<!-- Preserve existing inbound links after translating this heading. -->
<a id="jev-&#20915;&#31574;&#36755;&#20837;&#39044;&#31639;"></a>

## Jev decision input budget

`decision_max_bytes` limits the serialized Jev request byte budget; its environment variable is `JEV_ROUTER_DECISION_MAX_BYTES`. For example, use JSON `{"decision_max_bytes":64000}` or `JEV_ROUTER_DECISION_MAX_BYTES=64000`. Environment values take precedence and require restart. The accepted integer range is 1–268435456. Increasing the local budget does not change upstream limits.

The [official Jev model documentation](https://docs.typesafe.ai/models), checked on 2026-10-03, specifies 64k tokens for total request context and 32k for state plus the longest question. This project sends one model-selection question and therefore uses 32k as the reference. The official documentation provides no recommended byte budget. The default 32,000 **bytes** is a conservative local one-byte-per-token heuristic, not an exact conversion of the token limit or a guarantee of upstream acceptance. Configure a larger byte budget for better utilization only based on actual language, model and upstream validation.

The budget includes candidates, preferences, system instructions, latest query, question and serialization overhead. Jev receives no tool definitions. Tool history retains call IDs, names and result associations, excluding arguments, outputs and incidental execution-message text. Older history is omitted in complete groups. Each system/developer and latest-query text segment retains its beginning and end, truncates the middle with [...truncated...], and sets content_truncated. A common text limit is adjusted using actual serialized bytes; short segments remain complete, without splitting Chinese characters or emoji. Generation requests are never truncated. If candidates, requirements and minimum beginning/end structure still exceed the budget, return budget_exceeded. Multi-candidate auto and route.inspect use the same budget; explicit and sole-candidate paths do not call Jev.

Generation providers accept `protocol: "openai" | "anthropic"`. Omitted protocol defaults to `openai`, including replacement writes; preserve it when editing an Anthropic provider. Reads return the effective protocol. Existing stored rows need no migration or version increment. `base_url` is the API prefix (for example `https://api.anthropic.com/v1`).

## Native Anthropic generation upstream

Use a complete `providers.put` input such as:

```json
{"id":"claude","protocol":"anthropic","base_url":"https://api.anthropic.com/v1","secret_ref":"env:ANTHROPIC_API_KEY"}
```

The adapter appends `/messages`; OpenAI providers append `/chat/completions`. No hostname or version-path guessing occurs. `providers.get` returns the effective protocol and derived `endpoint`. Omission of protocol on replacement resets it to openai. Managed `api_key` writes support the same protocol field.

Native requests without an output limit send **65536** tokens and reserve the same amount in automatic routing. Explicit limits are preserved, never clamped. Connectivity probes still use 16. Tool requests, including continuation, support default or enabled thinking and replay plain `reasoning_content`. `chat_template_kwargs:{"enable_thinking":true}` maps to adaptive; false or `reasoning_effort:"none"` requests disabled thinking only when the upstream supports it. Signed thinking with tools is rejected by default. The verified exception is BigModel’s official HTTPS `/api/anthropic/v1` endpoint with upstream `glm-5.3` or `glm-5.3-flash`: these accept replay without signatures, so visible thinking is retained while the optional signature is omitted. Proxies and other models do not inherit this exception. Encrypted thinking remains unsupported. Test model support explicitly; a local conversion check does not guarantee the upstream model accepts its parameters.


For the verified BigModel profile, use this provider input (the environment reference must resolve on the Router server):

```json
{"id":"bigmodel","protocol":"anthropic","base_url":"https://open.bigmodel.cn/api/anthropic/v1","secret_ref":"env:BIGMODEL_API_KEY"}
```

Create models through `models.put` with `provider_id: "bigmodel"` and `upstream_name: "glm-5.3"` or `"glm-5.3-flash"`. Choose your own public IDs, descriptions and verified capabilities using the instance schema. Create disabled, run `models.test`, then enable; the 16-token connection test does not validate thinking, tools or vision. In Compose, add `BIGMODEL_API_KEY` to the service environment as well as `.env`, or use managed `api_key` instead.

The provider API prefix includes `/v1`; the resulting endpoint is `https://open.bigmodel.cn/api/anthropic/v1/messages`. An Anthropic SDK may itself append `/v1/messages` to a root URL, but Router provider configuration appends only `/messages`. OpenAI clients still connect to the Router's `/v1` URL. The Router `/v1/messages` entry can select native Anthropic providers; it requires max_tokens and preserves native content. See the [native Messages contract](compatibility.md#native-messages-clients-with-native-anthropic-upstream-51).

## Playground

After Dashboard login, the dedicated Cookie-only Playground supports text generation without a separate inference credential. It uses real routing/generation services, transmits content to providers and may incur charges. Existing CLI/SDK inference permissions and limits remain unchanged.

Startup configuration uses the following fields, with environment variables formed by `JEV_ROUTER_` plus the uppercase field name:

| Field | Default | Range |
| --- | --- | --- |
| `playground_enabled` | `true` | `true` / `false` |
| `playground_session_rpm` | 6 | 1–1000000 |
| `playground_instance_rpm` | 20 | 1–1000000 |
| `playground_session_concurrency` | 1 | 1–128 |
| `playground_instance_concurrency` | 3 | 1–128 |
| `playground_daily_requests` | 200 | 1–1000000 |
| `playground_input_bytes` | 32768 | 1–16777216, no greater than the body limit |
| `playground_body_bytes` | 262144 | 1–16777216 |
| `playground_max_messages` | 100 | 1–1000 |
| `playground_output_tokens` | 4096 | 16–10000000 |
| `playground_timeout` | `120s` | `1s`–`10m` |

RPM counts admitted requests in any rolling 60-second window; daily quotas reset at midnight UTC. Session/instance windows and daily quotas commit atomically in SQLite. Refreshing, logging in again or restarting does not reset instance quotas. Admitted failures, cancellations and timeouts still count; authentication and input-validation failures do not. Database admission failure prevents upstream calls.

Concurrency limits apply to a single instance process; replicas have no shared concurrency-coordination guarantee. Cancellation propagates upstream but cannot reverse incurred costs. Limits bound call volume and output, not exact spending. Input bytes include complete text and protocol-carried reasoning history. Over-limit input is rejected, never automatically truncated.

The page keeps conversations in memory only; refresh or navigation loses them. Failed, stopped or truncated turns are not automatically added to subsequent history. Thinking text is not displayed or stored persistently in the browser; protocol-required reasoning history remains in page memory. Streamdown renders content without executing raw HTML or loading images; existing CSP remains unchanged.


### Third-party Jev-compatible endpoints

`decision.put` accepts optional `path`, appended to `base_url` after removing its trailing slash. Omission uses `/v1/systemone` for backward compatibility. Supply a slash-prefixed path of alphanumeric, hyphen or underscore segments; URLs, query strings and fragments are not accepted in this field. A replacement that omits `path` resets it to the default.

For a third-party Jev-compatible service, use its documented base URL, request path and model ID. The following values are illustrative placeholders:

```json
{
  "base_url": "https://decision.example.com",
  "path": "/api/v1/decisions",
  "model": "provider-model-id",
  "api_key": "YOUR_API_KEY"
}
```

Prefer managed `api_key` with a configured encryption master key; alternatively use `secret_ref` with a server-side env/file reference. Supported services must use Bearer authentication and native `state` / `questions` requests with Choice answers. Configurable endpoints do not imply support for arbitrary decision protocols. Configuration writes do not test connectivity or change the deployed server environment.

## Generation upstream health

Health filtering applies to `auto` only. Explicit model requests still reach an
isolated target and return its sanitized upstream error; a complete success
restores automatic eligibility. Unknown targets are eligible. Health does not
change `enabled`, and `/v1/models` continues to list all enabled models.

Settings operators can read `models.list` / `models.get`, use `route.inspect`
to see health exclusions, and run `models.test` to check an isolated or disabled
model. Tests send fixed text and may incur provider charges. They update health,
not configuration versions. `status.get` reports `health_degraded` warnings when
health storage cannot be used reliably. Missing health is not an unknown result.

Startup JSON keys and corresponding `JEV_ROUTER_` uppercase environment variables:

| JSON key | Default | Meaning |
| --- | --- | --- |
| `health_failure_threshold` | `3` | Counted temporary failures before isolation |
| `health_failure_window` | `"2m"` | Rolling failure window |
| `health_transient_delay` | `"30s"` | First temporary-failure probe delay |
| `health_limit_delay` | `"1m"` | Rate-limit delay without valid Retry-After |
| `health_permanent_delay` | `"5m"` | Credentials/quota/model-error first delay |
| `health_max_backoff` | `"30m"` | Local exponential backoff cap |
| `health_concurrency` | `2` | Shared background/manual probe concurrency |
| `health_interval` | `"5s"` | Minimum interval between background probe starts |
| `health_timeout` | `"1m"` | Total probe timeout |
| `health_max_tokens` | `1024` | Probe output limit, including provider reasoning |

A valid upstream Retry-After is a lower bound even when longer than the local
backoff cap. Waiting does not restore health; a complete successful call does.
Disabled models are not automatically probed. Configuration errors and content
rejections are distinguished; generic 403/404/451 do not alone isolate a model.

The instance creates a separate 32-byte health fingerprint key in SQLite,
independent of the optional managed-credential encryption key. Back up and restore
the complete database. Restart preserves target identities and isolation. A
missing/corrupt initialized key never triggers automatic regeneration: automatic
routing fails, while explicit generation can still be attempted. Fingerprint-key
rotation is not supported. Treat the database and backups as private files.


## Auto fallback

| Config field | Environment variable | Default | Values |
| --- | --- | --- | --- |
| auto_fallback_enabled | JEV_ROUTER_AUTO_FALLBACK_ENABLED | true | true / false |
| auto_fallback_max_retries | JEV_ROUTER_AUTO_FALLBACK_MAX_RETRIES | 1 | 0 or 1 |
| auto_fallback_timeout | JEV_ROUTER_AUTO_FALLBACK_TIMEOUT | 180s | 1s–30m |

Disabling fallback or setting retries to zero preserves single-attempt behavior. The total budget applies to enabled Auto fallback across both decisions and generations, including streaming. Earlier caller or Playground limits win. Stage limits still apply independently. Budget exhaustion is returned as timeout but does not penalize target health. Explicit requests retain their existing limits. One accepted Playground request consumes one quota unit and may make two paid generation calls.
