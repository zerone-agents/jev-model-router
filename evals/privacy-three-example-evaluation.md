# Privacy prompt experiment: two sensitive examples and one public example

This exploratory test compares the previously reported original privacy prompt
with two synthetic three-example variants. Each variant contains exactly two
examples labelled `sensitive` and one labelled `non_sensitive`; their full text
is in `privacy_evaluate_128.py` under `three_examples` and `three_bilingual`.
Neither the examples nor the evaluation cases contain actual enterprise data.
The examples approximate common enterprise themes, but their distribution has
not been validated against any specific enterprise.

The same frozen 128 cases were run once against each local D3, Decider and Laya
service with each new prompt. The original results come from
`privacy_results_128.json`; the six new per-case result sets are in
`privacy_three_example_results.json`. D3 and Decider used the server's CUDA
containers; Laya used an isolated CPU service with a 512-token head budget. No
HTTP error occurred, so these results do not show a prompt-budget rejection.
The study directly calls the System One services using the router's privacy
question/criteria; it is not an end-to-end router or production security test.

This dataset was already evaluated in earlier prompt studies and is reused here.
The new 68 cases are not a fresh, untouched validation set for this experiment.
One run per case is too small to establish stability or generalization.

| Model | Prompt | Sensitive false negatives | Public false positives | Uncertain | Errors |
| --- | --- | ---: | ---: | ---: | ---: |
| D3 | Original | 8/84 | 0/44 | 0/128 | 0/128 |
| D3 | Three English examples | 5/84 | 0/44 | 0/128 | 0/128 |
| D3 | Three bilingual examples | 7/84 | 0/44 | 0/128 | 0/128 |
| Decider | Original | 28/84 | 0/44 | 0/128 | 0/128 |
| Decider | Three English examples | 29/84 | 0/44 | 0/128 | 0/128 |
| Decider | Three bilingual examples | 29/84 | 0/44 | 0/128 | 0/128 |
| Laya | Original | 34/84 | 2/44 | 20/128 | 0/128 |
| Laya | Three English examples | 49/84 | 3/44 | 4/128 | 0/128 |
| Laya | Three bilingual examples | 28/84 | 8/44 | 5/128 | 0/128 |

The English variant reduces D3's false negatives in this set but increases Laya's
substantially. The bilingual variant reduces Laya's false negatives while
increasing its public false positives; both variants slightly increase Decider's
false negatives. Fewer uncertain answers do not by themselves imply safer
routing: an uncertain result remains local, whereas a false negative may permit
cloud routing. No variant is established as the best shared default. Enterprise-
specific samples and new, representative, unseen validation data are needed;
never use a real company secret in examples exposed outside its trust boundary.
