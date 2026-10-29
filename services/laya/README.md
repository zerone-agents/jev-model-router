# Laya feasibility probe

This directory currently contains a feasibility probe, not a deployable service.
The Go Router remains unchanged. All inputs are synthetic; no provider credentials
or conversation captures are required.

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

The 256-token head budget excludes three framing tokens. The SDK also caps each
rendered option at 48 tokens and can shorten instructions. A future service must
reject any changed segment, not merely count retained candidate markers. These
fixtures do not establish a universal supported candidate count.

The encoder configuration declares an 8192-token ceiling. Short inference used
that ceiling, but full-length execution and long-context accuracy are not verified.
The measured run used four CPU threads: model loading took about 3.24 seconds,
short prepared calls took 0.10–0.36 seconds, and process peak RSS was about 1.91 GiB.
These are one macOS arm64 run, not Linux capacity or latency guarantees.

The examples verify compatibility, not decision quality. Linux native acceptance,
strict rejection tests, HTTP/authentication, overload/cancellation, provisioning,
and Compose deployment remain subsequent implementation work.
