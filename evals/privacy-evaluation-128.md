# Privacy classification evaluation: 128 synthetic cases

This evaluation uses three local decision services, not hosted Jev. It does not
establish privacy or security guarantees. The dataset, runner and per-case
outcomes are in `privacy_cases_128.json`, `privacy_evaluate_128.py` and
`privacy_results_128.json`, respectively. All content and identifiers are
synthetic, not real credentials or personal data.

## Method

The frozen set contains 128 cases: 84 sensitive and 44 public. The previous 60
cases were already inspected during prompt exploration. A further 68 cases
(44 sensitive and 24 public) were authored and held back until the two prompts
were fixed: the earlier baseline (`original`) and the previously tested local
preview variant (`preview`). The new cases are a held-out split relative to
those prompt selections, but they share an author and synthetic style; they are
not an independent external benchmark. English and Chinese cases include
credentials, private medical, financial, identity, legal, HR and business data;
system/developer messages, earlier history, tool arguments/results, long text
and prompt injection are also represented.

D3 nano and Decider 2B used CUDA containers on the same server; Laya ran as a
temporary CPU service with its 512-token head budget and cached model revision
`1720e3e3357cfe1e281542e223f8273b0890ca34`. D3 and Decider repository manifests
pin revisions `462efc366229ea268cf4eed3738b3fc4415e4817` and
`7614a6c7ea5a047aa7d7bcfb1a0015469381c3e3`; the deployment was not independently
attested against those manifests for this run. The script sends the same privacy
question and criteria used by the router directly to each System One endpoint.
It does not exercise the Router's input validation, protocol or routing paths.
Each case was run once per model and prompt; no confidence intervals or latency
claims follow from these measurements.

A sensitive case answered `non_sensitive` is a false negative and could permit a
cloud request. A public case answered `sensitive` is a false positive. The
`uncertain` category is reported separately; in the implemented router it
restricts generation to local. HTTP errors and invalid responses are counted
separately and also lead to uncertain routing. No HTTP errors occurred in these
six runs.

## Results across all 128 cases

| Model | Prompt | Sensitive false negatives | Public false positives | Uncertain | Errors |
| --- | --- | ---: | ---: | ---: | ---: |
| D3 nano | Original | 8/84 | 0/44 | 0/128 | 0/128 |
| D3 nano | Preview | 10/84 | 0/44 | 0/128 | 0/128 |
| Decider 2B | Original | 28/84 | 0/44 | 0/128 | 0/128 |
| Decider 2B | Preview | 32/84 | 0/44 | 0/128 | 0/128 |
| Laya | Original | 34/84 | 2/44 | 20/128 | 0/128 |
| Laya | Preview | 33/84 | 3/44 | 15/128 | 0/128 |

## New held-out 68 cases

| Model | Prompt | Sensitive false negatives | Public false positives | Uncertain | Errors |
| --- | --- | ---: | ---: | ---: | ---: |
| D3 nano | Original | 5/44 | 0/24 | 0/68 | 0/68 |
| D3 nano | Preview | 5/44 | 0/24 | 0/68 | 0/68 |
| Decider 2B | Original | 17/44 | 0/24 | 0/68 | 0/68 |
| Decider 2B | Preview | 20/44 | 0/24 | 0/68 | 0/68 |
| Laya | Original | 18/44 | 2/24 | 10/68 | 0/68 |
| Laya | Preview | 20/44 | 3/24 | 7/68 | 0/68 |

D3's original prompt missed new cases involving medical, HR, security and
unmarked personal information. The preview variant missed the same number of
held-out cases but changed which examples failed. Decider and Laya missed
examples in system/history and tool content as well as ordinary messages. The
preview prompt does not consistently improve these models, and Laya's lower
uncertain count should not be interpreted as better accuracy: more sensitive
cases were classified non-sensitive on the new split.

This synthetic, single-run evaluation cannot prove production performance. In
particular, even D3 produced false negatives. Do not rely solely on these models
for data-loss prevention or as a guarantee that sensitive content will not reach
cloud services.

## Reproduce

Set `PRIVACY_EVAL_KEY` to the local test service credential and, optionally,
`PRIVACY_EVAL_MODEL` to its configured model alias. Example:

```sh
python3 evals/privacy_evaluate_128.py d3 19203 original
```

The endpoint is constrained to loopback; use a local service or SSH tunnel. The
runner emits case IDs, labels, outcomes, HTTP status and aggregate counts as
JSON, without emitting the credential. Running new prompt variants against the
held-out cases uses those cases for tuning, so future unbiased validation needs
another unseen dataset.
