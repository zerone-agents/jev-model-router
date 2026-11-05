# Privacy prompt experiment: three sensitive and two public examples

This exploratory study adds five synthetic labelled examples to the original
privacy prompt: three sensitive and two non-sensitive. The complete prompt is
`five_examples` in `privacy_evaluate_128.py`. The examples cover an employee's
medical leave and compensation, a confidential customer quote and test
credential, private account transactions, an ordinary public website, and a
generic invoice without real data. None is a real person's or organization's
information; their distribution has not been validated against an enterprise.

We ran the same 128-case fixture once per local D3, Decider and Laya service.
The dataset has already been used for prompt comparisons, so neither the entire
set nor its 68-case subset is a fresh independent validation set. D3 and
Decider ran on CUDA; Laya used CPU and a 512-token head budget. The runner calls
System One directly with the router's privacy question and criteria; it does not
exercise Router admission or generation. There were no HTTP or input-budget
errors in the three runs. One run per case cannot establish stability.

## All 128 cases

| Model | Prompt | Sensitive false negatives | Public false positives | Uncertain | Errors |
| --- | --- | ---: | ---: | ---: | ---: |
| D3 | Original | 8/84 | 0/44 | 0/128 | 0/128 |
| D3 | 3 examples: 2 sensitive, 1 public | 5/84 | 0/44 | 0/128 | 0/128 |
| D3 | 5 examples: 3 sensitive, 2 public | 11/84 | 0/44 | 0/128 | 0/128 |
| Decider | Original | 28/84 | 0/44 | 0/128 | 0/128 |
| Decider | 3 examples: 2 sensitive, 1 public | 29/84 | 0/44 | 0/128 | 0/128 |
| Decider | 5 examples: 3 sensitive, 2 public | 26/84 | 0/44 | 0/128 | 0/128 |
| Laya | Original | 34/84 | 2/44 | 20/128 | 0/128 |
| Laya | 3 examples: 2 sensitive, 1 public | 49/84 | 3/44 | 4/128 | 0/128 |
| Laya | 5 examples: 3 sensitive, 2 public | 43/84 | 3/44 | 5/128 | 0/128 |

For the previously reported 68-case subset (44 sensitive, 24 public), the
five-example false-negative counts were D3 6, Decider 17 and Laya 22; public
false positives were 0, 0 and 3, and uncertain counts were 0, 0 and 3. This
subset is not independent here because it has appeared in previous evaluations.

The five-example prompt improved Decider on this set relative to the original,
but worsened D3 and Laya. Laya's fewer uncertain answers do not imply safer
routing: false negatives increased, and uncertain cases remain local whereas a
non-sensitive classification may route to cloud. One fixed synthetic prompt is
not evidence for any company's actual distribution. Further evaluation needs
representative sanitized enterprise examples and a separate unseen validation
set. Do not place actual secrets in examples outside the intended trust boundary.

Full outcomes for these three runs are in `privacy_five_example_results.json`;
original and three-example outcomes are in `privacy_results_128.json` and
`privacy_three_example_results.json`. Run the loopback-only evaluator using
`PRIVACY_EVAL_KEY` and the `five_examples` prompt selection. Subsequent exploratory tuning and its limitations are documented in
`privacy-prompt-tuning.md`.
