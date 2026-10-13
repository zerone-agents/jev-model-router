# Quick Start

English | [Chinese](README.zh-CN.md)

Run Jev Model Router with Docker Compose. One container serves the API and management UI; a named volume persists SQLite. Requires Docker with Compose and Git.

## Run (recommended)

The default stack pulls `zeroneai/jev-model-router:latest` from Docker Hub. No Go, Node, or source build is needed. To pin the current documented release, set `ROUTER_IMAGE=zeroneai/jev-model-router:0.1.19` in `.env`.

```sh
git clone https://github.com/zerone-agents/jev-model-router.git
cd jev-model-router/quickstart
cp .env.example .env
```

Edit `.env`: generate **three separate** values with `openssl rand -hex 32`. Set `JEV_ROUTER_SETTINGS_TOKEN`, `JEV_ROUTER_INFERENCE_TOKEN`, and `JEV_ROUTER_ENCRYPTION_KEY`. The first two authenticate clients; the third encrypts managed model credentials and must be backed up separately from SQLite. Run `chmod 600 .env`. Keep the file private; Git and image builds exclude it. Configure generation and decision API keys through the CLI/API after startup, rather than adding them to `.env`.
```sh
docker compose up -d
docker compose ps
```

Open http://127.0.0.1:8080/dashboard/ and enter the settings credential. The API base URL is `http://127.0.0.1:8080/v1`; clients use the inference credential. Change `ROUTER_PORT` for a different host port. `ROUTER_IMAGE` accepts a complete image reference, including a version tag or digest.

The default binding is local only. For remote access, provide an HTTPS reverse proxy and configure the network binding deliberately. The UI requires HTTPS outside loopback.

## Configure models

The initial database has no providers or models. Container health confirms the management API responds, not upstream availability or routing readiness.

```sh
docker compose exec -T router jev-router schema
docker compose exec -T router jev-router schema providers.put
docker compose exec -T router jev-router schema decision.put
docker compose exec -T router jev-router call status.get --json - <<'JSON'
{}
JSON
```

Use the returned current version for each write. Example provider input:

```json
{"id":"cloud","base_url":"https://api.openai.com/v1","api_key":"YOUR_PROVIDER_API_KEY"}
```

Run credential configuration yourself in your terminal. Create `provider.json` privately (for example, set `umask 077` before creating it), replace the placeholder locally, and pass it through stdin. Do not put the real key in command arguments, shell history or chat. Replace `VERSION` with the current configuration version:

```sh
docker compose exec -T router jev-router call providers.put \
  --json - --expected-version VERSION --idempotency-key setup-provider-1 < provider.json
```

Follow the [companion SKILL](../skills/jev-router/SKILL.md) to create a disabled model, test it, and enable it. Multiple-candidate `auto` also needs `decision.put`. Discover its schema first; then create a protected `decision.json` with `base_url`, `model` and write-only `api_key`, plus `path` when required by a third-party Jev-compatible service. Official Jev uses `https://api.typesafe.ai` and `jev-1.13.0`. Submit it with `docker compose exec -T router jev-router call decision.put --json - --expected-version VERSION --idempotency-key setup-decision-1 < decision.json`, using a freshly read version. Remove local credential files after verified success. Configuration remains managed through the shared API; `.env` does not create model resources. Model connection tests and routing checks may call paid services.

Inside the container, `localhost` is the container itself. Use a reachable upstream hostname, not the host machine's loopback address. After changing environment keys, recreate the service with `docker compose up -d`.

## Remotely managed API keys

Check both `jev-router schema providers.put` and `jev-router schema decision.put` for write-only `api_key`; older published images may support only provider credentials. If `decision.put` lacks this field, upgrade to a release containing managed decision credentials or use the source-build override below. Do not assume `latest` includes unreleased changes.

With `JEV_ROUTER_ENCRYPTION_KEY` configured at startup, add or rotate generation and decision API keys through protected JSON files or stdin. Supply exactly one of `api_key` and `secret_ref`. Managed key changes require no Compose edits or restarts. Existing `env:`/`file:` references remain supported. Optional `PROVIDER_API_KEY` and `TYPESAFE_API_KEY` mappings are retained only for existing deployments; additional external variables require explicit Compose mappings as well as `.env` values. Preserve those variables until existing references have been migrated.

Users should execute key configuration themselves. An Agent may assist with actual keys only after the user explicitly confirms a secure environment without leakage risk and explicitly provides the key for configuration. Never commit keys or share rendered Compose configuration containing them. Back up the encryption master key separately; losing it makes managed keys unrecoverable.
See [remote CLI instructions](../docs/cli-installation.md#managed-provider-keys) and [encryption and recovery](../docs/configuration.md#managed-provider-credentials). Remote secret submission requires HTTPS; loopback HTTP also works through an SSH tunnel. The reverse proxy must terminate TLS before forwarding privately to the container. Managed keys are never returned in plaintext. Old encrypted revisions are retained for in-flight requests; deleting a provider does not erase historical ciphertext/backups. Master-key rotation is not implemented: changing its value prevents startup with existing managed records.

## Build from source

```sh
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

This builds the frontend and Go binary from the repository root and uses `jev-model-router:local`, leaving the published image tag untouched.

## Operations

```sh
docker compose logs -f router
docker compose pull                 # fetch the published image
docker compose up -d                # recreate using that image; retain data
docker compose down                 # stop; retain the SQLite volume
```

`docker compose down -v` deletes the database volume, including configuration and records. Back up the volume with the service stopped before upgrades; do not run multiple instances against the same SQLite volume. See [startup configuration](../docs/configuration.md) for file-based secrets and other settings.

## Publishing (maintainers)

The Release workflow builds Linux amd64/arm64 images. Set the required Actions variable `DOCKERHUB_IMAGE` to `zeroneai/jev-model-router` (namespace/repository, without a registry host or tag). Configure repository secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` with push access to `zeroneai/jev-model-router`. A `v*` version tag publishes a versioned image; stable releases also update `latest`. Manual dispatch publishes `latest` from the selected ref. PR runs only the local container smoke test; the image publication job is skipped. First publication requires these credentials and an explicit release action; adding this workflow alone does not publish an image.

## Huawei Cloud SWR mirror

When Docker Hub is difficult to reach, set `ROUTER_IMAGE` in `.env` to the published SWR image, then run `docker compose pull && docker compose up -d`. Example target (availability depends on the maintainers' configured registry and first publication):

```dotenv
ROUTER_IMAGE=swr.cn-east-3.myhuaweicloud.com/zerone/jev-model-router:latest
```

Maintainers: use the same repository variable/secret names as Agent Hub:

| Kind | Name | Value |
| --- | --- | --- |
| Variable | `DOCKERHUB_IMAGE` | Docker Hub namespace/repository, e.g. `zeroneai/jev-model-router` |
| Variable | `REGISTRY_HOST` | SWR host, e.g. `swr.cn-east-3.myhuaweicloud.com` |
| Variable | `REGISTRY_IMAGE` | Full untagged image path, e.g. `swr.cn-east-3.myhuaweicloud.com/zerone/jev-model-router` |
| Secret | `DOCKER_REGISTRY_USER` | SWR login username |
| Secret | `DOCKER_REGISTRY_PASSWORD` | SWR login password |

With these configured, Release pushes the same build and version/latest tags to both Docker Hub and SWR. Without both SWR variables it publishes only to Docker Hub; partial or mismatched configuration fails. Registry authentication or push failures fail the release; verify both registries before announcing availability. PR builds never log in or publish. A private SWR repository requires users to authenticate; make the mirror public for an unauthenticated quickstart.

### Dashboard sessions

For a remote dashboard, set `JEV_ROUTER_DASHBOARD_ORIGIN=https://router.example.com` in `.env` and recreate the container. Terminate TLS at a proxy that preserves Host, and keep the backend private. With no origin configured, browser login works only on loopback HTTP. Login lasts 7 days across refresh and same-credential restart; logout revokes the current request session. CLI still uses Bearer. See [session deployment and recovery](../docs/configuration.md#dashboard-sessions) for capacity, credential rotation and backup recovery.

## Inference protocols

OpenAI clients use the Router `/v1` base URL; Anthropic SDK clients use the Router root. `/v1/chat/completions` supports OpenAI-compatible and native Anthropic upstreams; `/v1/messages` currently supports only OpenAI-compatible upstreams. See [request examples](../README.md#call-the-inference-api) and [native Anthropic/BigModel configuration](../docs/configuration.md#native-anthropic-generation-upstream).
