# Reproduce the frozen non-code 96-task report

Requires Python 3.9+ and its standard library. Run from the repository root:

```sh
python3 scripts/benchmarks/noncode-96/reproduce.py
python3 -m unittest discover -s scripts/benchmarks/noncode-96 -p 'test_*.py'
```

No network, credentials, model calls, or local benchmark service is required. The script verifies `snapshot.sha256`, checks complete task/arm coverage, regrades recorded final lines against frozen labels, and recomputes generation CNY and successful decision USD using Decimal arithmetic. It also reports model selections, invalid final formats, and output-limit stops. The output matches `expected.json`.

## What is tracked

`snapshot.json` contains 96 task IDs, source row indices, labels, question/prompt hashes, 384 selected successful response records, final lines, response/text hashes, token usage, decision usage, stop reasons, frozen prices, configuration hashes, and runtime provenance. It includes recovery counts but no failed-attempt charges. Full prompts, passages, reasoning traces, provider credentials, and private databases are omitted. SHA-256 provides integrity checks, not independent certification of model behavior.

The final lines permit independent reproduction of the frozen parsing and cost calculations; they do not independently verify the omitted full answers or establish that a new online run will produce identical results. No stored `passed` boolean is trusted by the reproduction script. The original responses remain in the local full archive for audit.

## Dataset attribution and pinned sources

| Dataset | Publisher | Split | Revision | License |
|---|---|---|---|---|
| [GSM8K](https://github.com/openai/grade-school-math) | OpenAI | main/test | `740312add88f781978c0658806c59bc2815b9866` | MIT |
| [MMLU-Pro](https://github.com/TIGER-AI-Lab/MMLU-Pro) | TIGER-Lab | test | `b189ec765aa7ed75c8acfea42df31fdae71f97be` | MIT in publisher dataset card |
| [BoolQ](https://github.com/google-research-datasets/boolean-questions) | Google Research | validation | `35b264d03638db9f4ce671b711558bf7ff0f80d5` | [CC BY-SA 3.0](https://creativecommons.org/licenses/by-sa/3.0/) |

The snapshot records exact official download URLs, file hashes, and original row counts. Selected source labels are retained unchanged; derived BoolQ task ID/label annotations remain under CC BY-SA 3.0. Preserve publisher attribution and applicable dataset licenses when redistributing them. These public subsets are not hidden tests or official full-benchmark scores. Full passages are not included here.

## Optional export from the full archive

```sh
python3 scripts/benchmarks/noncode-96/export_snapshot.py \
  --archive /path/to/benchmark-noncode-2026-10-16 \
  --output /path/to/new-export
```

The output directory must not exist. The exporter pins the original archive manifest hash, checks all 1,619 archived files, and deterministically creates the snapshot and its checksum. It never modifies the source archive. An export from the preserved original archive was byte-compared with the tracked snapshot before publication.

The original archive includes frozen configurations, original response files, `evaluation/`, `evaluation-recovered/`, source Parquet files, online runner/scorer scripts, and all-attempt accounting. The separate `routing-review-noncode-2026-10-16` archive retains the per-task comparison. These bulk archives are not committed and are not required for offline use of this package. Config hashes identify the preserved configurations; this compact package is not a turnkey online rerun environment.

## Accounting scope

The report adopts the successful-response snapshot after one retry of each initial service failure. Initial failures remain archived but their costs are excluded by the chosen reporting scope. Successful Jev decision fees are still included separately in USD. SR local compute is unpriced. This package does not claim all-in savings or identify the cause of the upstream HTTP 500 failures.
