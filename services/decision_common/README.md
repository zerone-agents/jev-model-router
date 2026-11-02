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
file before loading. The standalone provisioning command, separate images and Compose overrides are
included. Verify architecture-specific image and GPU runtime support before
production deployment.

## Compose deployment

The two services have independent images and volumes. Until image publication,
run the commands below from `quickstart/` using the appropriate service name
(`decider` or `d3`). Prepare `.env` with distinct Router credentials and a random
`DECISION_API_KEY` as described in the main quickstart.

```sh
# Substitute decider or d3 for SERVICE in each command.
docker compose -f docker-compose.yml -f docker-compose.SERVICE.yml -f docker-compose.SERVICE.build.yml build SERVICE
docker compose -f docker-compose.yml -f docker-compose.SERVICE.yml -f docker-compose.SERVICE.build.yml run --rm SERVICE-provision
docker compose -f docker-compose.yml -f docker-compose.SERVICE.yml -f docker-compose.SERVICE.build.yml up -d router SERVICE
```

On an NVIDIA host with the NVIDIA Container Toolkit installed, also include
`-f docker-compose.SERVICE.gpu.yml` in the `up` command. CPU is the default.
The provision command requires network access and verifies every pinned artifact;
serving loads the verified volume offline. To prepopulate from a verified copy,
mount it read-only and use `python -m decision_common.provision --manifest
/app/model-manifest.json --destination /models --offline-source /source`.

After readiness is healthy, discover `decision.put` through the Router CLI.
Configure `base_url` as `http://SERVICE:8090`, `path` as `/v1/systemone`,
`model` as `SERVICE` and `secret_ref` as `env:DECISION_API_KEY`. Read the
current version, write with a fresh idempotency key, read back the configuration,
and exercise `route.inspect` and inference. The service shares no GPU or network
namespace with Router, so both can be deployed independently.

## Publishing

Stable tags `decider-vMAJOR.MINOR.PATCH` and `d3-vMAJOR.MINOR.PATCH` trigger
separate image workflows. Configure `DOCKERHUB_DECIDER_IMAGE` and
`DOCKERHUB_D3_IMAGE`; optional Huawei mirrors use `REGISTRY_DECIDER_IMAGE` and
`REGISTRY_D3_IMAGE` under the existing `REGISTRY_HOST`. Existing registry login
secrets are reused. Pull requests validate and build without publishing. Releases
require native amd64 and arm64 validation and publish Docker-format manifest
lists. GPU support requires NVIDIA Container Toolkit on the target host.
