# Local Decider and D3 System One services

This work-in-progress shared server supports one independently configured Decider
2B or D3 worker. It exposes authenticated `POST /v1/systemone`, readiness,
liveness and authenticated metadata. Only choice questions are supported.
The worker holds its slot until inference completes and rejects concurrent work.

Set `DECISION_BACKEND` to `decider` or `d3`, configure the pinned manifest, `DECISION_MODEL_DIR` to a verified directory of regular files,
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
mount it read-only and use `python -m decision_common.provision --destination
/models --offline-source /source`.

After readiness is healthy, discover `decision.put` through the Router CLI.
Configure `base_url` as `http://SERVICE:8090`, `path` as `/v1/systemone`,
`model` as `SERVICE` and `secret_ref` as `env:DECISION_API_KEY`. Read the
current version, write with a fresh idempotency key, read back the configuration,
and exercise `route.inspect` and inference. The service shares no GPU or network
namespace with Router, so both can be deployed independently.

## D3 model variants

The D3 image contains code and pinned manifests, not model weights. Set
`D3_VARIANT` in `quickstart/.env` to `edge`, `lite`, `nano` (default), `mini`,
`flash`, or `d3`. Run the `d3-provision` command above before starting the
service; it downloads and verifies the selected snapshot into the persistent
volume. To switch variants, change `D3_VARIANT`, run `d3-provision` again,
then recreate `d3` with `docker compose -f docker-compose.yml -f
docker-compose.d3.yml up -d --force-recreate d3` from `quickstart/` (also
include the GPU override when needed). Verified snapshots are reused when switching back.
The serving process does not download models; an unprovisioned or mismatched
variant is unavailable until successful provisioning. A failed provision keeps
the previous snapshot, but the new variant cannot start until it is provisioned.

GPU memory, system memory and disk requirements vary substantially. The `d3`
variant may need more resources than a single GPU; no automatic quantization or
offloading is configured. Validate the selected size on your own hardware.
Privacy classification is probabilistic and must be evaluated for your data.
The pinned artifacts occupy approximately 1.2 GB (`edge`), 1.7 GB (`lite`),
4.5 GB (`nano`), 9.1 GB (`mini`), 16.8 GB (`flash`) and 52.2 GB (`d3`) on
disk, excluding temporary downloads and caches. Runtime memory requirements
are different and depend on hardware and configuration.

For offline provisioning, mount a verified snapshot as `/source` and use
`python -m decision_common.provision --destination /models --offline-source /source`
with the same `D3_VARIANT` as the service.

## Publishing

Stable tags `decider-vMAJOR.MINOR.PATCH` and `d3-vMAJOR.MINOR.PATCH` trigger
separate image workflows. Configure `DOCKERHUB_DECIDER_IMAGE` and
`DOCKERHUB_D3_IMAGE`; optional Huawei mirrors use `REGISTRY_DECIDER_IMAGE` and
`REGISTRY_D3_IMAGE` under the existing `REGISTRY_HOST`. Existing registry login
secrets are reused. D3 and Decider validation is performed locally; release
tags build and publish native amd64 and arm64 images with Docker-format
manifest lists. GPU support requires NVIDIA Container Toolkit on the target host.
