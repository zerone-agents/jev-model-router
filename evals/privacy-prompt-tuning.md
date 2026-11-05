# Privacy prompt tuning: four example-based variants

Four candidate prompts containing three to five synthetic examples were run
against the existing 128-case dataset on D3, Decider and Laya. The dataset was
already used in earlier prompt experiments; no part is a fresh independent
validation set for this selection. Each model/prompt pair was tested once per
case via its local System One service. D3 and Decider ran on CUDA, Laya on CPU
with a 512-token head budget. No run returned an HTTP error. These measurements
are not Router end-to-end testing, do not prove repeatability and do not provide
a safety guarantee.

Every row contains 84 sensitive and 44 public cases. A false negative is a
sensitive case classified non-sensitive and could therefore be sent to cloud.
A false positive is a public case classified sensitive. An uncertain result is
reported separately; the Router confines it to local generation. The baseline
numbers come from `privacy_results_128.json`; the four new runs per model are
preserved in `privacy_prompt_tuning_results.json`. Prompt definitions are in
`privacy_evaluate_128.py`.

| Prompt | Cases | D3 FN / FP / uncertain | Decider FN / FP / uncertain | Laya FN / FP / uncertain | Total FN |
| --- | ---: | --- | --- | --- | ---: |
| Original | 0 | 8 / 0 / 0 | 28 / 0 / 0 | 34 / 2 / 20 | 70 |
| Earlier three-example candidate | 3 | **5 / 0 / 0** | 29 / 0 / 0 | 49 / 3 / 4 | 83 |
| Round 1 | 3 | 8 / 0 / 0 | 30 / 0 / 0 | 37 / 3 / 4 | 75 |
| Round 2 | 4 | 10 / 0 / 0 | 31 / 0 / 0 | 23 / 11 / 8 | 64 |
| Round 3 | 4 | 9 / 0 / 0 | 30 / 0 / 0 | 38 / 5 / 3 | 77 |
| Round 4 | 5 | 9 / 0 / 0 | 29 / 0 / 0 | 35 / 9 / 4 | 73 |

The earlier `three_examples` candidate is selected provisionally because D3 nano
has the lowest observed false-negative count among tested model/prompt pairs:
5/84, with no public false positives or uncertain responses. D3's best observed
result, rather than an equally weighted average across models, is the selection
criterion. This supersedes the earlier aggregate selection of Round 2. The
selected prompt contains two sensitive examples and one non-sensitive example.

On this same reused set, Decider and Laya classified 29/84 and 49/84 sensitive
cases as non-sensitive under the selected prompt. They are not recommended for
this privacy-classification use based on this experiment. This is a benchmark
recommendation, not an enforced backend restriction: the configured decision
service remains administrator-selected. Results are from one run per pair on a
small synthetic dataset already used for tuning and do not establish a global
optimum or safety. Enterprise data distributions can differ. Use a genuinely
unseen, representative dataset before relying on the selected prompt; no model
provides a data-loss-prevention guarantee.

A second D3 run of the selected prompt on the same 128 cases produced the same
per-case outcomes (5/84 false negatives and 0/44 false positives); this is a
repeatability check on the same data, not independent validation. Results are
in `privacy_d3_selected_repeat.json`.

Existing persisted prompts are not overwritten by this change. Users can edit
the prompt through the Settings API and CLI.
