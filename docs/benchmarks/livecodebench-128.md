# Jev Routing Evaluation on 128 LiveCodeBench Tasks

Date: October 16, 2026

This report presents a selected evaluation snapshot using a shared answer bank and frozen routing choices. On this snapshot, Jev passed 91 of 128 tasks, compared with 88 for fixed Max, while reducing selected-response generation cost by 13.05%. These are descriptive results for this task set and snapshot, not a statistical claim of general superiority.

## Results

All four strategies use the same tasks, model pool, generation settings, and evaluator. When two strategies choose the same model for a task, they receive exactly the same generated answer.

| Strategy | Tasks passed | Pass rate | Selected-response generation cost (CNY) |
|---|---:|---:|---:|
| Fixed Qwen3.8-Max | 88 / 128 | 68.75% | 8.7390000 |
| Fixed Qwen3.8-Flash | 85 / 128 | 66.41% | 0.9988045 |
| Jev frozen-route replay | 91 / 128 | 71.09% | 7.5988475 |
| Semantic Router frozen-route replay | 85 / 128 | 66.41% | 6.5020520 |

Relative to fixed Max, Jev passed three additional tasks and used CNY 1.1401525 less in selected-response generation cost. Relative to Semantic Router, Jev passed six additional tasks and used CNY 1.0967955 more. Fixed Flash had the lowest generation cost in this snapshot.

Both routing policies selected Max for 85 tasks and Flash for 43 tasks. Their selections differed on 54 tasks. Equal model counts therefore do not imply equal outcomes or equal token costs.

## Evaluation protocol

The dataset consists of 128 frozen LiveCodeBench tasks: 64 Medium and 64 Hard, with 65 AtCoder tasks and 63 LeetCode tasks. Tasks were selected after development-set calibration. They were previously untested in this project when selected; this does not establish that they were absent from model training data.

The model pool is Qwen3.8-Max and Qwen3.8-Flash. Generation uses temperature 0, disabled thinking, an 8,192-token output limit, non-streaming responses, and concurrency 3. Answers that fail tests or reach the output limit remain in the denominator.

Jev model descriptions and Semantic Router configuration were calibrated on a separate development set and frozen before evaluation. These are configured policies, not vendor-default configurations. The evaluation replays their frozen per-task model choices against the shared answer bank; it does not make new routing decisions or measure routing-decision repeatability or end-to-end latency.

Scoring uses official code extraction and executable tests. The evaluator commit is `28fef95ea8c9f7a547c8329f2cd3d32b92c1fa24`. The Docker image ID is `sha256:085ee4d71dd0144099a7c077c867f7d260ae183b605f23c82287f880c8739c66`.

Candidate code executes inside isolated containers with networking disabled, a non-root user, a read-only root filesystem, a 4 GiB memory limit, and a six-second per-test timeout. The evaluator permits an additional 2 GiB virtual-allocation budget above its process baseline. This is not a 2 GiB total resident-memory limit.

## Cost accounting

Costs use frozen October 13, 2026 API-equivalent list prices, not Coding Plan subscription charges or actual provider invoices.

| Model | Input / million tokens (CNY) | Output / million tokens (CNY) | Cache read / million tokens (CNY) |
|---|---:|---:|---:|
| Qwen3.8-Max | 12.0 | 36.0 | 1.5 |
| Qwen3.8-Flash | 0.8 | 2.7 | 0.1 |

Price references: [Qwen3.8-Max documentation](https://help.aliyun.com/zh/model-studio/qwen3-8-max) and [Qwen3.8-Flash documentation](https://help.aliyun.com/en/model-studio/qwen3-8-flash). These are the archived pricing basis, not a newly verified current-price quotation.

Each cost is calculated from the recorded token usage of the answers selected by that strategy. All selected responses in this snapshot have reported usage. Routing decision fees and local Semantic Router compute are excluded. Replay costs are hypothetical selections from a shared bank, not four additive provider bills. Cache behavior reflects the shared generation schedule rather than separate deployments of each strategy.

## Interpretation

This snapshot shows a quality-cost tradeoff: Jev achieved the highest pass count, Semantic Router used less generation cost than Jev, and fixed Flash was cheapest. The result supports this specific observed comparison. It does not establish a stable ranking, production savings after routing overhead, or performance across other workloads.

The preserved local evidence includes frozen task IDs and configurations, prompts, raw model responses, token usage, per-task grades, and an offline reproduction script. That script recomputes results from recorded responses and grades; it does not regenerate model answers or independently rerun tests. The large evidence archive is maintained separately from this Git-tracked report.

## Reproduce this snapshot

Run `python3 scripts/benchmarks/livecodebench-128/reproduce.py` from the repository root. The [reproduction guide](../../scripts/benchmarks/livecodebench-128/README.md) explains the tracked evidence, offline calculation, and full-archive dependencies.
