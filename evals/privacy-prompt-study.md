# Privacy prompt study

This synthetic study compares the existing privacy prompt and alternatives on
three locally hosted decision models. It does not establish safety for real data.
The reproducible fixture and runner are in `evals/privacy_prompt_study.py`;
all names, credentials, addresses and identifiers in it are fictional.

The fixed dataset has 60 cases: 40 labelled sensitive and 20 public, in English
and Chinese. A 36-case tuning split (24 sensitive, 12 public) was used to compare
seven prompt variants. A separate 24-case split (16 sensitive, 8 public) was
held back until a prompt was selected for each model. The splits were authored
at the same time and are small, so this is not an independent external benchmark.
They cover credentials, personal and financial information, medical and business
content, system and developer messages, tool calls and results, and history.

The experiment used the actual local System One services on the user's GPU
server. D3 nano and Decider 2B ran in CUDA containers; Laya ran as a temporary CPU
service using cached weights and a 512-token head budget. The router's privacy
question and criteria were reproduced by the script, but the study does not
exercise router admission, all protocol validation or live generation providers.
An `uncertain` answer or service error is reported separately; it is not a false
negative, because the router restricts it to local generation. An erroneous
`non_sensitive` answer on a sensitive case is a false negative and can permit
cloud routing. No real personal data was submitted.

## Held-out results

The original prompt was selected for D3, `risk` for Decider and `preview` for
Laya, based on their tuning results. The table compares each selection with the
original prompt on the held-out split. Numbers are individual cases, not a claim
of general accuracy.

| Model | Prompt | Sensitive false negatives | Public false positives | Uncertain or service error |
| --- | --- | ---: | ---: | ---: |
| D3 nano | Original | 0/16 | 0/8 | 0/24 |
| Decider 2B | Original | 3/16 | 0/8 | 0/24 |
| Decider 2B | Risk | 3/16 | 0/8 | 0/24 |
| Laya | Original | 7/16 | 0/8 | 3/24 |
| Laya | Preview | 6/16 | 0/8 | 2/24 |

D3 had 3/24 false negatives on the tuning split under the original prompt, so
its held-out zero must not be presented as an overall guarantee. The Decider
candidate changed which inputs were missed without reducing the count. Laya's
candidate improved this small held-out set by one case but still missed 6/16.
Seven prompt variants could not establish a dependable improvement across all
three models. Use of the same model for selection and privacy, editable prompts,
and administrator-declared locality do not make classification an authorization
or data-loss-prevention guarantee.

## Reproduction

Set `PRIVACY_EVAL_KEY` to the local service's dedicated test credential and, if
needed, `PRIVACY_EVAL_MODEL` to its configured alias. For example, run
`python3 evals/privacy_prompt_study.py d3 19203 test original`. The program
writes individual outcome labels, HTTP status and aggregate counts as JSON; it
never prints the supplied service credential. The numeric examples are synthetic.
The two splits are fixed in source; once a prompt is adjusted using the held-out
cases, that split is no longer independent for the new selection.
