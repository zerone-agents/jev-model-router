# Final auto spot-check fixtures

English | [简体中文](README.zh-CN.md)

[cases.json](cases.json) preserves four selected public routing tasks, their generation request options and the answer checks fixed before execution. These are public regression fixtures. They do not form a fresh holdout or a general benchmark.

Submit each `request` unchanged to `/v1/chat/completions` with an inference credential. Use the published routing model descriptions and default balanced template. Do not call route.inspect first or replace auto with its selected model. No caller token limit is included; supplier limits still apply. Wait for the complete SSE stream, accumulate content, require normal completion and verify public model consistency on all frames. The extraction request adds a strict shape schema without supplying answer values.

Match the response request ID to its management routing record; check jev_choice, both candidates and constant configuration version. Compare simple outputs against `expected` exactly (trim outer whitespace for the integer answer). Complex outputs require full review against every rubric item; a material contradictory prescription fails even if the topic is otherwise covered. Do not equate keyword coverage, normal transport completion or successful JSON parsing with correct answers. Retain errors and incomplete runs rather than selectively retrying.

The existing `go run ./evals` tool does not implement this streaming/manual-review protocol. This directory provides reproducible requests and grading rules, not another executable runner. Keep responses and per-call metadata local. See the [evidence summary](../../docs/acceptance/2026-09-29.md) for historical findings.
