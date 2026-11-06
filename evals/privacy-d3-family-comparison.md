# D3 family privacy classification comparison

The same 128 synthetic requests and `three_examples` privacy prompt were sent serially to each local System One service on an NVIDIA RTX 4090 reporting 49,140 MiB. Each set contains 84 sensitive and 44 public cases. No Jev service was used.

| Model | Sensitive false negatives | Public false positives | Uncertain | Errors | Total seconds |
| --- | ---: | ---: | ---: | ---: | ---: |
| D3 nano | 5/84 | 0/44 | 0 | 0 | 12.209 |
| D3 mini | 3/84 | 0/44 | 0 | 0 | 13.553 |
| D3 flash | 2/84 | 0/44 | 0 | 0 | 13.925 |

D3 flash achieved the fewest false negatives in this run, followed by mini and nano; flash also requires the most resources of these three sizes. Nano remains the default to reduce deployment resource requirements. A false negative may permit confidential content to be sent to a cloud model. Flash still missed two sensitive cases; these results do not establish production privacy protection.

The entire dataset had previously been used during prompt tuning. It is not an independent holdout; its synthetic cases cannot represent every enterprise distribution. The elapsed time is one end-to-end serial run per model, not a latency benchmark or statistically reliable speed comparison. Nano and mini were resident during their runs; flash was loaded afterward, and all three were resident during the flash run. Other existing workloads were present; the three runs were serial. Total observed GPU memory after loading flash was 35,598 MiB, including existing workloads.

The pinned snapshots were nano `462efc366229ea268cf4eed3738b3fc4415e4817`, mini `df99bf4ca4418bc6908ee68e9b7479af1f3b8800`, and flash `67e03e1bc157961c582ee7c6a35d9b15fada3dec`. The mini and flash weight shards and other files listed in each downloaded model package manifest passed SHA-256 verification against that manifest. Model source files were additionally hashed into the isolated test service manifest from the downloaded snapshots; these checks establish consistency with the downloaded manifest and snapshots, not independent authenticity verification. The services used a test-only adapter allowing each D3 model ID; production D3 service currently pins nano.

Per-case outcomes and timings are in `privacy-d3-family-results.json`. Synthetic request inputs and evaluation harness are in `privacy_cases_128.json` and `privacy_evaluate_128.py`.

## Repository deployment verification

The updated pinned-manifest provisioning and serving paths were exercised on the GPU server using offline copies of the pinned nano, mini and flash snapshots. Each service became healthy and ran the same 128 cases with no request errors: nano 5, mini 3 and flash 2 sensitive false negatives, and zero public false positives. Mini continued to answer after the active pointer switched to flash; switching back to the verified mini snapshot succeeded. These checks do not change the study limitations above.
