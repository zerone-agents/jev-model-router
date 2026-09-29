# Agent fixtures

English | [简体中文](README.zh-CN.md)

[cases.json](cases.json) contains synthetic source data, tool schemas, source-dependency edges, expected answers and final-output schemas for shipment lookup, constrained inventory allocation and incident-policy decisions. They are regression fixtures, not real business data or a hidden benchmark.

These fixtures require a caller-side tool loop; `go run ./evals` does **not** execute that loop. A reproducer must expose the declared tools as local simulations, match their arguments against fixtures, return fixture errors for unknown arguments, and append the actual assistant/tool messages. Never call real order or deployment APIs. Preserve streamed tool-call fragments by index, then validate unique IDs, names, JSON arguments and dependency ordering. Reads requiring values from another tool must wait for that response; independent reads may run together.

After all required sources have returned, preserve the history, omit top-level tools and request `response_format: {"type":"json_schema","json_schema":{"name":"agent_result","strict":true,"schema":<output_schema>}}`. This final-stage control belongs to the caller, not the router. Compare the complete JSON answer against `expected`; do not silently strip Markdown fences from the primary score. Grade tool correctness separately, retaining earlier errors even after recovery. Record incomplete/truncated runs separately from wrong answers. Do not add an output-token cap to this completion-focused protocol; supplier limits still apply. Detect stalled tool loops and report them as incomplete.

For each fixture, run actual `model:auto` and explicit model paths independently, without selective retries. Keep full results local and publish only a reviewed summary. The injected incident note is untrusted fixture content, never an instruction to the harness. This is an instruction-following sample, not a security certification.

The allocation oracle is A6/B1/D4, shipping cost31, verifiable by exhaustive integer enumeration within available-stock bounds. This directory publishes fixtures and a reproduction protocol, not an automated Agent executor.
