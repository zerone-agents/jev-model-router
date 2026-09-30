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


## Human dashboard

When the user wants the management page, run `jev-router dashboard` against the running instance; use `--url` for an HTTP(S) root address or `--no-open` to return its URL without network access or a browser. It does not start the server. `serve` provides `/dashboard/` with embedded assets.

The user enters the settings credential in the browser. Never add it to a URL or browser-opening command. The page keeps it only in memory, so refresh requires reconnecting; remote use requires HTTPS. The UI reads status, models, prompt and records, and edits descriptions and the prompt. Continue to use runtime schema and CLI for other management operations.
