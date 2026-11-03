# Jev Model Router: seven routing strategies and local decision latency

November 3, 2026. On this shared answer bank, Decider 2B matched fixed Max on both samples at about 26% lower generation cost. Laya matched fixed Max on code and fixed Flash on non-code. Cloud Jev passed the most coding tasks (91). These results describe the selected answers and configurations on these tasks.

## Quality and generation cost

| Strategy | Code /128 | Max / Flash | CNY | Non-code /96 | Max / Flash | CNY |
|---|---:|---:|---:|---:|---:|---:|
| Jev Model Router (cloud Jev) | 91 | 85 / 43 | 7.5988 | 86 | 13 / 83 | 0.5026 |
| Semantic Router | 85 | 85 / 43 | 6.5021 | 85 | 19 / 77 | 0.3110 |
| Jev Model Router (Laya) | 88 | 101 / 27 | 6.1811 | 84 | 15 / 81 | 0.2764 |
| Jev Model Router (Decider 2B) | 88 | 68 / 60 | 6.4732 | 88 | 47 / 49 | 0.7316 |
| Jev Model Router (D3 nano) | 87 | 119 / 9 | 8.3495 | 85 | 13 / 83 | 0.3746 |
| Fixed Max | 88 | 128 / 0 | 8.7390 | 88 | 96 / 0 | 0.9874 |
| Fixed Flash | 85 | 0 / 128 | 0.9988 | 84 | 0 / 96 | 0.1811 |

Each task has two recorded answers, one from Qwen3.8-Max and one from Qwen3.8-Flash. Each policy gets the answer, grade and token usage for its chosen model. Costs use frozen API-equivalent token prices; they exclude decision fees, failed attempts and local compute. All rows cover every task. Fixed Flash is the cheapest strategy in both samples.

The three local backends made real decisions on an RTX 4090 with the same compact candidate descriptions and candidate order (Flash first). The original long descriptions exceeded Laya's 48-token per-option limit; all 224 of those requests failed. The table therefore uses the separate compact condition for **all three** local models. Cloud Jev and Semantic Router use their historical frozen choices, not fresh decisions under the compact configuration. This controls answer variability, not differences in router configuration.

Code uses the selected [128-task LiveCodeBench snapshot](livecodebench-128.md). Non-code uses the two fixed-model answers from the [96-task online evaluation](noncode-96.md), regraded with its frozen final-line rules. Its historical routing choices are replayed against those same answers, so these scores can differ from the independently generated online results.

## Decision latency (median / P95, milliseconds)

| Decision backend | RTX 4090 | Four CPU cores | Online API (historical) |
|---|---:|---:|---:|
| Jev Model Router (Laya) | 31.0 / 72.8 | 225.4 / 862.5 | — |
| Jev Model Router (Decider 2B) | 29.6 / 62.7 | 2,172.1 / 5,961.7 | — |
| Jev Model Router (D3 nano) | 28.3 / 48.5 | 2,248.9 / 5,883.1 | — |
| Cloud Jev | — | — | 750.0 / 4,013.0 |

The six local measurements use the same eight tasks: two each from coding, math, knowledge and reading. Each backend processed one excluded warmup followed by three serial passes (24 successful timed requests). Loopback HTTP timing includes inference and returning the choice, excluding downstream generation. CPU runs on the same Intel Core Ultra 7 270K Plus host used four pinned cores, four intra-op threads and no CUDA. The GPU and CPU services used device-specific kernels. P95 is the nearest-rank percentile (the second slowest local observation); these small samples measure warm latency, not concurrent throughput.

**The online API figure is historical and uses a different task batch.** It is the router-recorded decision time for 96 successful non-code cloud decisions from October 16, with benchmark concurrency three. The November API probe returned insufficient credit, so it supplied no successful latency observation. Differences in inputs, concurrency, network and timing boundaries preclude a matched cloud/local speed ratio. Semantic Router latency was not measured here; fixed policies skip model selection.

## Reproduce offline

From the repository root, run:

```sh
python3 scripts/benchmarks/local-routing/reproduce.py
```

The [compact evidence](../../scripts/benchmarks/local-routing/evidence.json) contains the per-task local choices, the six timing sequences (index zero is warmup; subsequent fields are task ID, HTTP status and milliseconds), the historical cloud observations and hashes of the original source files. The [script](../../scripts/benchmarks/local-routing/reproduce.py) verifies the evidence and both existing answer-bank checksums, joins choices by task ID, and prints both tables. It does not contact a provider, execute candidate code or remeasure latency.
