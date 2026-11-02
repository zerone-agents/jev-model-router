# Local Decider and D3 System One services

This work-in-progress shared server supports one independently configured Decider
2B or D3 nano worker. It exposes authenticated `POST /v1/systemone`, readiness,
liveness and authenticated metadata. Only choice questions are supported.
The worker holds its slot until inference completes and rejects concurrent work.

Set `DECISION_BACKEND` to `decider` or `d3`, `DECISION_MANIFEST` to its pinned
manifest, `DECISION_MODEL_DIR` to a verified directory of regular files,
`DECISION_MODEL_ALIAS` to the Router model ID, and `DECISION_API_KEY_FILE` to a
private file containing the dedicated bearer token. `DECISION_DEVICE` accepts
`cpu` or `cuda` and defaults to `cpu`. `DECISION_CONTEXT_TOKENS` defaults to
8192. CUDA and the pinned model libraries must be installed separately.

Run one service process using `uvicorn decision_common.server:application
--factory --host 127.0.0.1 --port 8090` with `services/decision_common` on
`PYTHONPATH`. Use network isolation or a trusted proxy for remote access.

The model manifests are tied to fixed revisions. Serving verifies every listed
file before loading. The current provisioning module is a library and image,
standalone provisioning entry point, dependency lock and Compose integration are
not yet delivered. Runtime dependencies and CPU architectures still need explicit
acceptance before deployment claims.
