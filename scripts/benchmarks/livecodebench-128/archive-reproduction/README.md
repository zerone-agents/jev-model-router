# Best observed results on the frozen 128 tasks

This bundle records the best whole replicate for each policy, with its matching cost, and retains all three replicate scores and means. Peaks are selected independently after observing results. They are not a paired comparison, a per-task oracle, or evidence of expected performance. Actual online router results are shown separately from offline frozen-route replay.

## Offline reproduction (tested)

Requires Python 3.9 or newer and the two original archives adjacent to this bundle. No network, credentials, model calls, Docker, or third-party Python packages are needed. The output directory must not exist.

```sh
python3 reproduce.py --data-root .. --output /private/tmp/jev-best-reproduced
```

Run from this bundle directory, or supply absolute paths. The script verifies every file listed in both original archive manifests against SHA-256, checks the pinned manifest hashes in `source-pins.json`, and runs the original analysis against a new copy of the raw responses and saved evaluator results. It recomputes the token costs, answer bank, bootstrap statistics, policy scores, and best-run report. It requires exact equality with the archived three-replicate summary. Source archives are never modified. Generated candidate programs and serialized tests are never executed by this offline path.

Outputs include `report.zh-CN.md`, `results.json`, `analysis.log`, and a `recomputed/` evidence directory. The report and results in this bundle were produced by this command. The original archive dependencies are:

- `benchmark-shared-3x-2026-10-15`: raw responses, scores, prompts, route choices, frozen settings, prices, and analysis.
- `benchmark-expanded-128-2026-10-15`: original online four-arm evidence, frozen task data, and calibration provenance.

Keep both archives when relocating this bundle. It is not a standalone copy of the large dataset.

## Original generation and scoring scripts

`original-scripts/` contains unchanged copies of all Python scripts from the shared-bank experiment. Their hashes can be checked against that archive's manifest. These are provenance snapshots, not a portable one-command online runner: several contain absolute local paths, and live verification depends on the original service and external private credential files. No credentials are included in this bundle.

For a new live experiment, work in a new directory and review/relocate paths in `prepare.py`, `run.py`, `score_all.py`, `verify.py`, `finish.py`, and `archive.py`. Recreate the frozen service model aliases and generation settings from the source archive, provision credentials outside the archive, and preserve all existing attempts. `prepare.py` establishes the schedule and source snapshots; `run.py` performs explicit-model calls with failure-only retries; `score_all.py` invokes `score_shard.py` in isolated Docker containers; `verify.py` checks the protocol; `summarize.py` and `report.py` produce the original analysis. Do not blindly run `finish.py` or `archive.py` against historical paths.

For rescoring, use the original selected dataset at `benchmark-expanded-128-2026-10-15/evaluation/selected.jsonl`, the archived responses, and the pinned evaluator image and resource policy documented in the report and source manifest. Candidate code and serialized benchmark tests must run only inside the configured network-disabled, non-root, read-only-root containers. The offline command above reuses recorded grades; it does not independently re-execute tests. New provider calls can differ even with identical parameters, so only offline replay is expected to reproduce the recorded numbers exactly.

## Accounting

Prices are the frozen 2026-10-13 API-equivalent prices, not current quotes or Coding Plan invoices. Selected-response generation costs exclude routing overhead and unknown-usage failed attempts. The two failed shared-bank attempts affect SR replicate 3 (and the corresponding fixed baselines); unknown costs are never represented as zero. Original configuration and result archives remain unchanged.
