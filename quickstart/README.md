# Quick Start

English | [简体中文](README.zh-CN.md)

Run Jev Model Router with Docker Compose. One container serves the API and management UI; a named volume persists SQLite. Requires Docker with Compose and Git.

## Run (recommended)

The default stack pulls `zeroneai/jev-model-router:latest` from Docker Hub. No Go, Node, or source build is needed. The image must be published before this path can run; if it is unavailable, use the source-build override below.

```sh
git clone https://github.com/zerone-agents/jev-model-router.git
cd jev-model-router/quickstart
cp .env.example .env
```

Edit `.env`: set **two different** random values for `JEV_ROUTER_SETTINGS_TOKEN` and `JEV_ROUTER_INFERENCE_TOKEN` (run `openssl rand -hex 32` twice). Add your generation provider key and TypeSafe key when configuring those services. Keep `.env` private; it is ignored by Git and excluded from image builds. For more providers, explicitly add their environment variables to Compose as well as `.env`.

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
docker compose exec -T router jev-router call status.get --json - <<'JSON'
{}
JSON
```

Use the returned current version for each write. Example provider input:

```json
{"id":"cloud","base_url":"https://api.openai.com/v1","secret_ref":"env:PROVIDER_API_KEY"}
```

Save it as `provider.json` on your host, then send it through stdin (replace `VERSION` with the current version):

```sh
docker compose exec -T router jev-router call providers.put \
  --json - --expected-version VERSION --idempotency-key setup-provider-1 < provider.json
```

Follow the [companion SKILL](../skills/jev-router/SKILL.md) to create a disabled model, test it, and enable it. Multiple-candidate `auto` also needs `decision.put`, using `https://api.typesafe.ai`, `jev-1.13.0`, and `env:TYPESAFE_API_KEY`. Configuration remains managed through the shared API; `.env` does not create model resources. Model connection tests and routing checks may call paid services.

Inside the container, `localhost` is the container itself. Use a reachable upstream hostname, not the host machine's loopback address. After changing environment keys, recreate the service with `docker compose up -d`.


## Remotely managed API keys (after v0.1.1)

Use a server and CLI build containing managed-credential support; v0.1.1 rejects `api_key`. Until a release includes it, use the source-build override. Generate one master key with `openssl rand -hex 32` and place it in `JEV_ROUTER_ENCRYPTION_KEY` in the server's private `.env` (`chmod 600 .env`). Recreate the container once. Back up this master key separately from SQLite; losing it makes stored provider keys unrecoverable. Never commit it or share rendered Compose configuration containing its value.

After that setup, add or replace provider API keys through the remote CLI using a protected JSON file or stdin. The input has `id`, `base_url`, and write-only `api_key` instead of `secret_ref`. No Compose edit or restart is needed for subsequent provider-key changes. Keep `TYPESAFE_API_KEY` for decision-backend credentials. Existing `env:`/`file:` provider references remain supported.

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
