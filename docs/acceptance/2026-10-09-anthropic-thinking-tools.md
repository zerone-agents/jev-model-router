# 2026-10-09 Anthropic upstream thinking/tool acceptance

## Scope

Local Router from `fix/anthropic-thinking-tools`, based on `db3463b` (v0.1.14), exposes OpenAI Chat Completions and calls BigModel's real Anthropic endpoint. Production Router configuration and deployment were not changed.

Agent SDK v3.5.1 (`e533243`) was used without source or example modifications. The SDK received the local Router `/v1` base URL with API type `openai-completions`. Router upstream was `https://open.bigmodel.cn/api/anthropic/v1`, mapping external `bigmodel-glm-5.3` to `glm-5.3` and retaining `glm-5.3-flash`.

## Findings and changes

- Removed the blanket requirement to disable thinking for tool requests. Default, enabled, and supported effort controls can reach native upstreams.
- Replay assistant `reasoning_content` as native thinking before text/tool blocks, preserving text and tool associations. Bifrost's final Chat builder strips unsigned thinking; the adapter now sends its checked native body through Bifrost's public passthrough transport and reuses Bifrost response converters.
- Native stream validation still precedes conversion. Completion waits for `message_stop` and clean EOF, including gzip checksum verification. HTTP/SSE errors remain safely mapped; cancellation and size limits remain enforced.
- BigModel JSON tool responses include nonempty signatures. Replaying the same captured synthetic tool history with and without its signature returned HTTP 200 and the correct answer on both models (four comparisons). Real SDK examples subsequently verified their own multi-turn histories without signatures.
- A request-local compatibility profile permits omission only for the official HTTPS BigModel endpoint (default port/443) and upstream models `glm-5.3` / `glm-5.3-flash`. Visible thinking is retained; native signature fidelity is not claimed. Other hosts, proxies, paths and models retain the signed-tool restriction. Encrypted thinking remains unsupported. No signatures are fabricated.

## Unmodified SDK examples

Every run exited successfully without timeout. Success was checked in example output and tool results, not only process exit status. Multi-turn examples produced all expected nonzero-usage results; effort examples produced all four responses.

| Example | glm-5.3-flash | bigmodel-glm-5.3 |
| --- | --- | --- |
| `basic/01-simple-query.ts` | PASS (10.97 s) | PASS (19.82 s) |
| `basic/03-multi-turn.ts` | PASS (29.27 s) | PASS (51.75 s) |
| `tools/07-custom-tools.ts` | PASS (8.38 s) | PASS (17.25 s) |
| `streaming/16-streaming.ts` | PASS (8.08 s) | PASS (10.91 s) |
| `streaming/17-streaming-with-tools.ts` | PASS (212.0 s) | PASS (99.52 s) |
| `advanced/32-reasoning-effort.ts` | PASS (33.24 s) | PASS (43.96 s) |

The streaming tool example completed both queries on each model. Flash sent a 18,520-character Write argument and continued through additional Bash calls; GLM-5.3 sent a 12,789-character Write argument. These establish argument delivery, tool execution and continuation, not a general quality assessment of generated code.

The effort example exercises low, medium, high and default requests. Successful responses establish request compatibility; they do not prove the upstream honors each effort value with distinct reasoning behavior.

## Regression checks

- `go test ./...`, `go vet ./...`, `go build ./...`: PASS.
- `go test -race ./internal/provider ./internal/transport/http`: PASS.
- OpenAI SDK and Agent SDK production-HTTP tests: 24 JSON/SSE tool roundtrips covering disabled, default, enabled and high-effort thinking; PASS against a local native upstream.
- Provider regressions cover actual sent thinking history, stream IDs, signature profile isolation, safe HTTP/SSE error statuses, malformed/truncated streams, trailing errors, gzip corruption, cancellation and usage handling.

SDK example working directories were isolated temporary fixtures. Existing fixed-name `/tmp` example files were backed up/restored; temporary credential files were removed. Raw model responses and credentials are not committed. Repeating real examples requires explicit test credentials and incurs upstream usage.
