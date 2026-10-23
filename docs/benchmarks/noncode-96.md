# Non-code Online Routing Evaluation: 96 Tasks

Date: October 16, 2026

This frozen exploratory evaluation compares fixed Qwen3.8-Max, fixed Qwen3.8-Flash, Jev, and Semantic Router on the same 96 non-code tasks. Both routers make live decisions and generate answers independently. The reported snapshot includes one recovery attempt for each initial service failure.

Jev passed 86/96 tasks with 43.71% lower successful-response generation cost than fixed Max, which passed 88/96. Jev and Semantic Router achieved the same score; Semantic Router used less generation cost. This is a small-sample observed tradeoff, not evidence of stable superiority or an official full-benchmark result.

## Results

| Strategy | GSM8K /32 | MMLU-Pro /32 | BoolQ /32 | Total /96 | Pass rate | Successful generation cost (CNY) |
|---|---:|---:|---:|---:|---:|---:|
| Fixed Max | 31 | 27 | 30 | 88 | 91.67% | 0.9873840 |
| Fixed Flash | 30 | 24 | 30 | 84 | 87.50% | 0.1811183 |
| Jev | 31 | 25 | 30 | 86 | 89.58% | 0.5558036 |
| Semantic Router | 31 | 25 | 30 | 86 | 89.58% | 0.3079090 |

Jev selected Flash for 83 tasks and Max for 13. Semantic Router selected Flash for 77 tasks and Max for 19. Their selections differed on 24 tasks. Fewer Max calls did not produce a lower token bill: Jev's nine unique Max selections were knowledge questions with much longer outputs than SR's fifteen unique Max selections.

## Recovery and accounting boundary

The initial 384 requests produced 367 HTTP 200 responses and 17 Jev routing-stage HTTP 500 failures. DefAPI reported failure to forward to its upstream API. Each failed request was retried once with unchanged prompts, models, configuration, and grading; all 17 retries succeeded. No incorrect or truncated answer was retried. There were 401 benchmark attempts in total. Intermediate-provider throttling is a possible explanation, but the recorded error does not establish that cause.

The score table uses the 384 selected successful responses. **Failed-attempt costs are excluded from this report's cost comparison**, as are setup and synthetic-control calls. Successful Jev decisions add **USD 0.004963224**, reported separately without currency conversion. Semantic Router's local compute and other serving overhead are unpriced. The generation-cost table is not an all-in deployment bill or a Coding Plan invoice.

All selected successful responses have reported generation usage. Flash had six output-limit stops with invalid final-answer format; SR had four. These responses remain incorrect under the frozen grading protocol. Jev and fixed Max had no such stops.

## Frozen protocol

- **Tasks:** 32 randomly selected GSM8K test questions; 32 MMLU-Pro test questions sampled across twelve categories excluding math and computer science; and 32 BoolQ public validation questions, balanced to 16 true and 16 false. Selection seed: `2026101601`. Exact overlap with the previously used MMLU-Pro validation questions was excluded. This is an English, single-turn, tool-free pilot.
- **Routing:** generic Jev model descriptions and the existing SR general balance recipe mapped to the same two-model pool. Both were frozen before formal calls. Synthetic controls checked operation; there was no holdout-driven configuration tuning. These are configured policies, not a comparison of two untouched vendor defaults.
- **Generation:** temperature 0, thinking disabled, 4,096 output tokens, non-streaming, concurrency 3. The initial interleaved schedule used seed `2026101602`. All four arms received identical task messages; SR bridge forwarding was checked.
- **Grading:** the last nonempty line must match `FINAL: <number>`, `FINAL: <A-J>`, or `FINAL: true|false`. Numeric answers use exact Decimal equality; option letters and Boolean answers use exact equality. Missing or invalid format counts incorrect. No model judge or post-hoc relabeling is used. This custom zero-shot protocol differs from official leaderboard protocols.
- **Runtime:** Jev configuration version 13. The local benchmark router binary includes the decision transport-timeout fix; its SHA-256 is `dc94ee5a6e4fe8bfc6046c7d06d7520c6155d3a27575b7d2aad17c522d537dc6`. A repository commit alone does not identify that binary. The pinned SR image digest is `sha256:efc356587252c28b8d972d5d27b381df0626ccdc53597f3fccff7ae454041178`. Frozen configuration hashes are in the evidence snapshot.

## Frozen token prices

These are the archived October 13, 2026 API-equivalent prices, not newly verified current quotes.

| Model | Input / million tokens (CNY) | Output / million tokens (CNY) | Cache read / million tokens (CNY) |
|---|---:|---:|---:|
| Qwen3.8-Max | 12.0 | 36.0 | 1.5 |
| Qwen3.8-Flash | 0.8 | 2.7 | 0.1 |

Jev decision pricing is USD 0.042 per million input tokens and USD 0 per million output tokens. Historical source references and the complete recorded usage are retained in the snapshot. Successful generation and decision fees are recomputed separately by the reproduction script.

## Interpretation limits

Each arm generated its own answer, including when routers chose the same model. Identical model choices therefore do not remove generation variability, even at temperature zero. The 86–86 tie does not establish equivalent expected quality, and the cost comparison does not establish stable production savings.

These are established public datasets, not evidence of training-unseen evaluation. GSM8K and BoolQ provide relatively easy-task coverage, and each 32-question category has coarse score increments. Per-task review also identified wording or option ambiguities; original keys and scores remain unchanged. The results do not establish multilingual, multi-turn Agent, or tool-use performance.

## Evidence and reproduction

Run from the repository root:

```sh
python3 scripts/benchmarks/noncode-96/reproduce.py
python3 -m unittest discover -s scripts/benchmarks/noncode-96 -p 'test_*.py'
```

The [reproduction guide](../../scripts/benchmarks/noncode-96/README.md) describes the tracked compact evidence, pinned dataset sources, and optional full-archive export. The offline script regrades recorded final lines and recomputes token costs; it does not call models or regenerate answers. Full original configurations, initial results, recovery results, and per-task review remain preserved separately in local archives. No original artifact is overwritten by this publication.
