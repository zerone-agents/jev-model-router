# Routing preference regression

[简体中文](README.zh-CN.md)

These public synthetic cases preserve the September 2026 routing experiment. `development.jsonl` has eight cases; `regression.jsonl` has twelve previously held-out cases. **The published set is now a regression set, not an independent holdout.** Create new, pre-labelled tasks before evaluating a tuned prompt.

Labels express the configured preference: routine tasks → `qwen3.8-flash`, deep correctness analysis and constrained reasoning → `qwen3.8-max`. They do not claim that the other model cannot solve a task. The model descriptions in `model-descriptions.json` assume Flash is cheaper and faster, while Max is stronger, more expensive and potentially slower. These are acceptance assumptions, not measured economics.

Configure a separate instance with those enabled public model IDs, the supplied descriptions, identical capabilities (context_limit 16000, tools and structured_output true, images false), the unchanged [balanced template](../../templates/balanced.md), and Jev `jev-1.13.0`. Discover the current management schema before configuring it. Credentials are supplied to the server through secret references; the runner only needs its settings credential. It does not configure the instance or call generation endpoints.

```sh
# Start and configure the instance separately; inject its settings token securely.
export JEV_ROUTER_URL=http://127.0.0.1:8080
mkdir -p docs/validation
# Both commands may incur Jev charges. No inference credential is needed.
go run ./evals --mode inspect --cases evals/routing/development.jsonl --output docs/validation/routing-development.jsonl --allow-paid
go run ./evals --mode inspect --cases evals/routing/regression.jsonl --repeat 2 --output docs/validation/routing-regression.jsonl --allow-paid
```

Use new output paths for each run. No automatic retries or overwrites. `repeat` identifies each attempt; repetitions use file order (the historical experiment reversed the second pass). Check outcomes as well as selection hits: successful process exit means metrics were written, not that acceptance passed. Require one successful row per case/repetition, `path=jev_choice`, both candidates, a constant `config_version`, and no error rows. The runner rejects a selected model absent from the returned candidate list. It does not discover extra configuration changes outside these snapshots; keep the instance unchanged.

The original pre-fixed gate was ≥22/24 hits, ≥10/12 per class, ≥10/12 cases with identical repeated selections, and no call/invalid-choice errors. Development scores are separate. Freeze and record dataset/model-description/template hashes, source revision, server configuration and thresholds **before** each run. The runner saves metrics only, without credentials, prompts or model answers. Do not publish raw run files.

This tool measures preference conformance only. Use separate Agent fixtures for generation quality and a separate study for actual economics. See the [dated evidence summary](../../docs/acceptance/2026-09-29.md).
