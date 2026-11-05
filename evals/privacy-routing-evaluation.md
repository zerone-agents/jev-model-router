# Privacy classification evaluation

This is a small, synthetic development evaluation, not a privacy guarantee or a
representative production benchmark. The reproducible fixture is
`internal/decision/privacy_quality_test.go`. It uses 20 examples: 14 labelled
sensitive and six labelled public, in Chinese and English. The sensitive cases
include credentials, personal and medical information, confidential business
content, system and developer messages, tool calls and results, and secrets in
longer history. No real credentials or personal records are used.

On 2026-11-05, the fixture used the default privacy prompt and the Router's
System One classifier adapter against two separately deployed CUDA services on
an NVIDIA RTX 4090 reporting 49,140 MiB of memory. The service images were local
validation builds `jev-decider:gpu-final` and `jev-d3:gpu-final`. The repository
manifests pin Decider revision `7614a6c7ea5a047aa7d7bcfb1a0015469381c3e3` and
D3 revision `462efc366229ea268cf4eed3738b3fc4415e4817`; these results do not
independently attest the container's provisioned model revision.

| Service | Sensitive false negatives | Public false positives | Uncertain or errors | Adapter preflight rejects |
| --- | ---: | ---: | ---: | ---: |
| Decider 2B | 5/14 | 0/6 | 0/20 | 0/20 |
| D3 nano | 0/14 | 0/6 | 0/20 | 0/20 |

Decider missed the synthetic medical case, a secret in tool arguments, and all
three longer-history cases. An incorrectly `non_sensitive` result permits the
normal cloud route: operators should not rely on this Decider configuration to
protect sensitive content. D3's result on this small fixture does not establish
that it is safe from false negatives. Input acceptance is not evidence that a
model attended to every part of its input; actual decision quality and
complete-input coverage are distinct properties.

The measured test loop took approximately 1.6 seconds per model for 20
sequential calls on this deployment; this is not a latency benchmark. Separate
Router HTTP tests with real decision services and stub generation confirmed that
these examples can select a local target and reject explicit cloud selection.
They did not validate a real generation provider. Broader and independently
curated privacy evaluation is necessary before trusting any model with real
sensitive data.
