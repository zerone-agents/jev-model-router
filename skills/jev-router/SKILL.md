---
name: jev-router
description: Configure and inspect Jev Model Router through its Agent CLI. Use when managing providers, model descriptions, routing prompts, or investigating automatic and explicit model selection.
license: Apache-2.0
metadata:
  protocols:
    - cli
  status: design-stage
---

# Jev Model Router

This skill accompanies the product CLI. This repository currently contains the architecture skeleton, not an executable CLI. Do not claim that configuration or routing was performed without a working installation and a successful operation result.

## Discover the installed contract

Check whether `jev-router` is installed and read its help/version. If unavailable, report that the CLI is not installed or implemented; do not invent installation commands or substitute an unrelated executable.

Use the installed CLI's advertised capability discovery entrypoint to obtain schemas, examples, required scopes, risk, and result semantics. The proposed discovery command is `jev-router schema`; execute it only when supported by the installed version. Treat runtime schemas as the source of truth for command names and parameters.

## Choose the operation

- To configure a provider or model, read the existing resource and configuration version, apply only the requested change, validate it, and use the advertised update capability. Use credential references; keep secret values out of model descriptions and output.
- To change selection preferences, edit the routing prompt. The default is one balanced template; do not invent preset modes or task-to-model rules.
- To investigate a selection, inspect eligible models, their descriptions and capabilities, then use route inspection if authorized for its declared cost and data destination. Inspection does not generate an answer, but may call a paid remote decision API.
- To select a model explicitly, use an ID visible to the authenticated caller. An explicit choice still obeys capability and privacy constraints and does not authorize fallback to another model.

Jev mode has no privacy routing guarantee. Privacy routing is optional with local Laya; a local-only session must not be switched to a cloud model to work around a failure. Changing a model description or routing prompt does not override those constraints.

## Mutate and verify

Follow the operation's declared authorization and approval boundary. For writes, provide the expected configuration version and an idempotency key when the contract requires them. Reuse the same key for a retry of the same intended operation; an altered payload is a new operation.

Read structured status and error fields. For a conflict, refresh state before preparing another change. For pending approval, follow the declared approval path; do not treat it as completion. Retry only when the result says it is retryable and the operation's effects are understood.

After a successful change, read back the relevant resource/version and report the operation ID and observed result. Distinguish saved configuration, a routing decision, and successful generation; none implies the others.
