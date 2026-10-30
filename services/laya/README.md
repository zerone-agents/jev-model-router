# Local Laya decision service

An optional independent CPU service selects models and reasoning effort through
the existing System One decision contract. Hosted Jev operation does not require
Python. Local decisions may select cloud generation models; this service does not
provide sensitive-session locking or offline generation.

## Deployment

The prebuilt image name is `zeroneai/jev-model-router-laya`; publication is pending.
Until a version is published, use the contributor build override below.
From `quickstart/`, configure the existing Router tokens and a separate random
`DECISION_API_KEY` in your private `.env`. Never reuse Settings or inference tokens.

```sh
docker compose -f docker-compose.yml -f docker-compose.laya.yml -f docker-compose.laya.build.yml build laya
docker compose -f docker-compose.yml -f docker-compose.laya.yml -f docker-compose.laya.build.yml run --rm laya-provision
docker compose -f docker-compose.yml -f docker-compose.laya.yml -f docker-compose.laya.build.yml up -d router laya
```

After publication, omit the build override and set `LAYA_IMAGE` to a released tag.
Provisioning is an explicit network operation; serving verifies the artifacts and
loads offline from the read-only volume. Downloads are staged and hash-checked
before an atomic `current` pointer is promoted. A failed download preserves the
previous revision. Retained revisions require disk space until manually removed.

Configure the running Router through CLI schema discovery and `decision.put`:

```json
{
  "base_url": "http://laya:8090",
  "path": "/v1/systemone",
  "model": "laya-multilingual",
  "secret_ref": "env:DECISION_API_KEY"
}
```

Use the current `expected_version` and a fresh idempotency key, read back
`decision.get`, then call `route.inspect` with a Chat-format Auto request.
Configuration writes and `status.get` do not prove model availability. Inspect
service readiness separately; verify public Chat and Messages inference and
routing records after configuration. Explicit model calls bypass the decider.

The service exposes no host port in Compose. `GET /health/live` and
`GET /health/ready` are inexpensive public health checks. `/metadata` and
`POST /v1/systemone` require the dedicated Bearer token. One model worker accepts
one request at a time, with no queue; concurrent work returns 429. Caller
cancellation does not release capacity while inference continues. The default
60-second worker deadline terminates and reloads a stuck worker. Readiness is
false during reload. Raw requests, secrets and model tracebacks are not logged.

Settings: `LAYA_MODEL_DIR`, `LAYA_MODEL_ALIAS`, `LAYA_CPU_THREADS` (default 4),
`LAYA_CONTEXT_TOKENS` (at most 8192),
`LAYA_HEAD_TOKENS` (default 512, range 1–8188), `LAYA_WORKER_TIMEOUT_SECONDS` (default 60).
Authentication accepts either `LAYA_API_KEY` or `LAYA_API_KEY_FILE`, never both.
Linux arm64 short offline inference has been verified; amd64 is not yet verified.

For offline provisioning, mount a directory containing the original manifest
artifacts and run `python -m jev_laya.provision --destination /models
--offline-source /source`. Do not pre-normalize tokenizer configuration: original
hashes are checked, and SDK normalization occurs only in private temporary storage.

## Feasibility evidence

The following probes use synthetic inputs without provider credentials or
conversation captures.

## Reproduce

Use Python 3.12.12 in an isolated environment and install
`feasibility-requirements.txt`. This records the successful macOS arm64 CPU
environment, not a cross-platform production lock. Obtain the source revision and
model artifacts listed in `model-manifest.json`; verify their original sizes and
SHA-256 values before loading. Keep the source checkout separate from the model.
The pinned SDK normalizes the tokenizer configuration in place, so use a writable
working copy of the verified artifacts for this probe.

From the repository root:

```sh
LAYA_FIXTURE_OUTPUT=/tmp/laya-requests.json go test ./internal/decision -run TestLayaDecisionFixtures -count=1
python services/laya/scripts/check_feasibility.py --requests /tmp/laya-requests.json --model-dir /path/to/model --source /path/to/laya/laya/common.py --output /tmp/laya-head-report.json
python services/laya/scripts/check_inference.py --requests /tmp/laya-requests.json --model-dir /path/to/model --output /tmp/laya-inference-report.json
```

Run inference only after the head check succeeds. The inference script uses local
artifacts with Hugging Face and Transformers offline mode. It compares an immutable
prepared-input path with the SDK on model-only, effort-only and two-question inputs.
The gate extracts unchanged upstream formatting functions for a lightweight check;
it is not the production encoding implementation.

## Observed result

On 2026-10-29, the required short fixtures passed without changing Router prompts
or raising the trained head budget. Real CPU inference matched the SDK, including
answers and input-token accounting, for all four short fixtures.

| Question | Full framed tokens | Retained tokens | Lossless |
| --- | ---: | ---: | --- |
| Six effort choices | 137 | 137 | Yes |
| Two short model descriptions | 65 | 65 | Yes |
| Five short model descriptions | 105 | 105 | Yes |
| Twenty short model descriptions | 328 | 259 | No |
| Forty short model descriptions | 638 | 259 | No |

The checkpoint default 256-token head budget excludes three framing tokens.
The service now defaults to 512 and exposes `LAYA_HEAD_TOKENS`; the table above
records the original 256-token experiment. A follow-up real-model probe retained
all 328 framed tokens for twenty candidates at both 512 and 1024, with identical
answers. Forty short candidates fit at 1024 (638 framed tokens). These results
do not guarantee that arbitrary descriptions fit or that decision quality improves.
The head shares the total context ceiling with the state; raising it does not
raise the 48-token per-option cap. Restart the service after configuration changes. The SDK also caps each
rendered option at 48 tokens and can shorten instructions. The service rejects any changed segment, not merely count retained candidate markers. These
fixtures do not establish a universal supported candidate count.

The encoder configuration declares an 8192-token ceiling. Short inference used
that ceiling, but full-length execution and long-context accuracy are not verified.
The measured run used four CPU threads: model loading took about 3.24 seconds,
short prepared calls took 0.10–0.36 seconds, and process peak RSS was about 1.91 GiB.
These are one macOS arm64 run, not Linux capacity or latency guarantees.

The examples verify compatibility, not decision quality. Native Linux arm64
inference, strict rejection, HTTP authentication and worker lifecycle tests passed.
Compose provisioning, offline cached restart, missing-weight readiness, and Router
Agent integration also passed. Generation responses in the integration test were
controlled fixtures; these checks do not establish generation-model quality.

## Image publication configuration

The Laya workflow validates pull requests without registry writes. Version tags
publish only after validation. Configure repository variable `DOCKERHUB_LAYA_IMAGE`
with the separate Docker Hub image name; it reuses `DOCKERHUB_USERNAME` and
`DOCKERHUB_TOKEN`. An optional Huawei mirror uses `REGISTRY_LAYA_IMAGE`, the existing
`REGISTRY_HOST`, `DOCKER_REGISTRY_USER` and `DOCKER_REGISTRY_PASSWORD`. Docker media
types are used with provenance and SBOM attachments disabled for mirror compatibility.
Only Linux arm64 is currently included; add amd64 after native runtime acceptance.

## Bounded routing-quality observation

A small synthetic bilingual probe used the unchanged Router model/effort
instructions, two task-specialized candidates, and both candidate orders.
English and Chinese translation requests selected the fast model. An English
proof request selected the reasoning model; its Chinese counterpart selected
the fast model. All eight requests selected `minimal` effort. Reversing candidate
order did not change these outcomes.

This is a diagnostic sample, not an accuracy estimate or a calibrated effort
benchmark. It demonstrates a concrete multilingual/effort limitation of the pinned
checkpoint under this setup. Evaluate your own tasks before relying on its
choices; successful HTTP integration does not establish routing quality.

## Recovering corrupt model storage

Verification failures leave readiness false and preserve existing files. Cached
revision directories are not overwritten automatically. If a revision is corrupt,
stop the Laya service, back up the model volume, and move only the affected
revision directory out of `revisions/` to a quarantine directory. Then rerun the
explicit provisioning command and restart Laya after verification succeeds.
Keep the quarantine until recovery is confirmed. Do not remove Router data or
credentials; explicit model calls and management remain available during recovery.

In the native arm64 acceptance environment, a 3026-position request completed in
about 20 seconds. A near-ceiling request exceeded the default 60-second worker
deadline and returned 503; the worker reloaded and accepted a subsequent short
request. The 8192-token encoding ceiling is not a latency guarantee. Size CPU and
deadline settings for measured workloads rather than assuming every fitting input
finishes within the default deadline.

For the eight short bilingual/order probes in the Linux arm64 container, median
HTTP latency was 0.451 seconds and nearest-rank p95 was 1.082 seconds. With only
eight observations, these are descriptive timings, not a service-level objective.

Reproduce the short bilingual/order probe with Go-generated fixtures and an
explicit local endpoint:

```sh
# Supply LAYA_API_KEY through your private environment, not command arguments.
python services/laya/scripts/evaluate_choices.py --requests /tmp/laya-requests.json --url http://127.0.0.1:8090 --output /tmp/laya-quality.json
```

The script preserves Router question instructions, substitutes synthetic tasks
and candidate descriptions, and records raw answers and request latency. Expected
model labels are diagnostic annotations rather than automatic pass/fail gates.


## Approved head-budget change

On 2026-10-30 the user explicitly approved the 512-token service default and
configurable head budget after the real twenty-candidate capacity experiment.
This supersedes the original design restriction to the checkpoint default of 256;
it does not change weights or establish routing quality.

Acceptance separates capacity from quality: the unchanged Router twenty-candidate
fixture must preserve all instructions/options at 512, match real SDK inference,
and fail at 256. Configuration defaults/overrides, invalid ranges, runtime
propagation and reported metadata must agree; context and per-option rejection
remain in force. The configured upper bound is not a validated quality range.

The capacity fixture has no selection-quality oracle. The bilingual/order and
effort observations above remain limitations, not successful quality acceptance.
Representative quality evaluation is still required before recommending this
backend for production routing; SDK equivalence cannot substitute for it.


## Native Mac GPU

`LAYA_DEVICE` defaults to `cpu`; set it to `mps` to use an available Apple Metal
GPU in a native macOS Python deployment. Only `cpu` and `mps` are accepted.
Restart after changing it. Explicit MPS selection never silently falls back to
CPU: unavailable devices or initialization failures leave readiness false.
Authenticated metadata reports the configured device; readiness confirms worker
initialization, including a real inference smoke check.

Use the same verified artifacts, dedicated token and single-worker server:

```sh
# Set LAYA_API_KEY privately and LAYA_MODEL_DIR to verified original artifacts.
LAYA_DEVICE=mps uv run --project services/laya --frozen uvicorn jev_laya.server:application --factory --host 127.0.0.1 --port 8090
```

The Linux Docker Compose deployment remains CPU-only; this native macOS option
does not expose Metal inside Docker. `LAYA_CPU_THREADS` still controls CPU work.
On one Apple M4, eager inference with head 512, one warmup and three measured
runs yielded medians of 0.86 seconds on MPS versus 2.26 seconds on four CPU
threads for twenty candidates plus effort. With longer input (2648/2839 positions
for the two questions), medians were 1.69 versus 5.76 seconds. Choices matched
in these samples. These are latency observations, not quality or concurrency
certification; model loading and first-call warmup are additional costs.
