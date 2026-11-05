# Privacy one-shot prompt experiment

This exploratory study adds one synthetic, labelled sensitive example to the
privacy instructions. It reuses `privacy_cases_128.json`, the runner
`privacy_evaluate_128.py` and the local D3, Decider and Laya System One services.
The examples and test data are fictional and do not contain real credentials or
personal information. All prompt variants use exactly one example:

- `oneshot_employee`: a private employee treatment record and compensation.
- `oneshot_business`: a confidential customer contract price and test credential.
- `oneshot_mixed`: a private employee record with medical information, a customer
  quote and a test account password.

These are synthetic approximations, not samples from an actual enterprise or
proof that their distribution matches any enterprise. Their complete wording is
in the runner's `PROMPTS` mapping. This direct decision-service experiment does
not exercise Router end-to-end behavior.

## Previously used 60 cases

The first 60 cases were used to compare all three one-shot variants. The original
prompt baseline comes from `privacy_results_128.json`. Each row has 40 sensitive
and 20 public cases; all entries below had zero HTTP errors.

| Model | Original false negatives | Employee | Business | Mixed |
| --- | ---: | ---: | ---: | ---: |
| D3 nano | 3/40 | 3/40 | 3/40 | 3/40 |
| Decider 2B | 11/40 | 12/40 | 11/40 | 9/40 |
| Laya | 16/40 | 23/40 | 18/40 | 22/40 |

False positives were zero for all these runs except Laya mixed, which had one;
Laya reported four uncertain with employee, four with business and two with
mixed. Original Laya had ten uncertain on this split.

## Repeat comparison on the other 68 cases

Business (best aggregate on the 60 cases) and mixed (best Decider result on the
60 cases) were selected before running the other 68 cases. **These 68 cases had
already been evaluated and their baseline results reported in the prior study, so
this is not a fresh independent validation set.** Each row has 44 sensitive and
24 public cases; all runs had zero HTTP errors.

| Model | Prompt | Sensitive false negatives | Public false positives | Uncertain |
| --- | --- | ---: | ---: | ---: |
| D3 nano | Original | 5/44 | 0/24 | 0/68 |
| D3 nano | Business | 5/44 | 0/24 | 0/68 |
| D3 nano | Mixed | 6/44 | 0/24 | 0/68 |
| Decider 2B | Original | 17/44 | 0/24 | 0/68 |
| Decider 2B | Business | 20/44 | 0/24 | 0/68 |
| Decider 2B | Mixed | 19/44 | 0/24 | 0/68 |
| Laya | Original | 18/44 | 2/24 | 10/68 |
| Laya | Business | 23/44 | 2/24 | 4/68 |
| Laya | Mixed | 26/44 | 2/24 | 2/68 |

Lower uncertain counts are not automatically better: Laya's one-shot variants
also misclassified more sensitive cases as non-sensitive. The mixed example
improved Decider on the first split but worsened it on the second. These single
runs do not support replacing the default prompt or claiming that one-shot cannot
help with a representative enterprise-specific dataset. That hypothesis needs a
separate set of sanitized, representative examples and a truly unseen validation
set. Never include real enterprise secrets in a prompt sent to an untrusted or
cloud service; classify and evaluate within the intended trust boundary.

The machine-readable per-case output for all 15 runs is in
`privacy_oneshot_results.json`; baseline per-case results remain in
`privacy_results_128.json`. Run the same loopback-only evaluator with
`PRIVACY_EVAL_SPLIT=previously_seen` or `new_holdout` and the respective prompt
name. Once examples or prompts are tuned against these cases, use new unseen
cases to measure generalization.
