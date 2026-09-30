# CLI installation

English | [简体中文](cli-installation.zh-CN.md)

Download the archive matching your local machine from [GitHub Releases](https://github.com/zerone-agents/jev-model-router/releases). CLI assets are introduced with v0.1.1. Version numbers match the server release. The executable includes both remote management commands and `serve`; no Go or Node runtime is required.

| System | Architecture | Archive suffix |
| --- | --- | --- |
| macOS Intel | amd64 | `darwin_amd64.tar.gz` |
| macOS Apple Silicon | arm64 | `darwin_arm64.tar.gz` |
| Linux x86-64 | amd64 | `linux_amd64.tar.gz` |
| Linux ARM64 | arm64 | `linux_arm64.tar.gz` |
| Windows x86-64 | amd64 | `windows_amd64.zip` |

Download `SHA256SUMS` from the same release and compare the archive's SHA-256 hash before extracting. macOS: `shasum -a 256 <archive>`; Linux: `sha256sum <archive>`; PowerShell: `Get-FileHash <archive> -Algorithm SHA256`. The checksums detect corruption; they are not code signatures. The binaries are not signed/notarized.

For example, extract `jev-router_0.1.1_darwin_arm64.tar.gz`, then place its `jev-router` executable in a directory on your PATH. On Windows, extract the ZIP and use `jev-router.exe` from PowerShell or add its directory to PATH.

```sh
jev-router --version
jev-router --help
```

## Manage a remote instance

Use the server root URL, without `/v1`. Supply the remote instance's settings credential through your shell/secret manager, not command arguments:

```sh
export JEV_ROUTER_URL=https://router.example.com
# Securely set JEV_ROUTER_SETTINGS_TOKEN to the remote settings credential.
jev-router schema
jev-router call status.get --json - <<'JSON'
{}
JSON
jev-router dashboard
```

Alternatively keep Compose bound to the server's loopback interface and open an SSH tunnel:

```sh
ssh -N -L 18080:127.0.0.1:8080 user@server
# In another local terminal:
export JEV_ROUTER_URL=http://127.0.0.1:18080
```

The remote instance retains SQLite and upstream credentials. JSON configuration files passed to the local CLI are read locally and sent through the management API. Provider secret references must resolve on the server. See the [companion SKILL](../skills/jev-router/SKILL.md) for configuration workflows.

## Maintainers

`CLI Release` packages five targets from the tagged source and attaches archives plus `SHA256SUMS` to the corresponding GitHub Release. If none exists it creates a draft, leaving publication and release notes to the maintainer. Manual dispatch takes an existing version tag, allowing attachment backfills without moving tags. Source version must match the tag. PRs build and verify but never upload. Existing assets are not overwritten automatically; a duplicate upload fails.

Local packaging requires Go 1.27.0 and Python 3:

```sh
python3 scripts/package-cli.py --source /path/to/tag-checkout --version v0.1.1 --output /tmp/cli-assets
```

Use a fresh output directory. CI verifies checksums and runs the Linux amd64 binary; cross-compilation alone does not establish native execution coverage on every platform.

## Managed provider keys

This capability requires server/CLI builds newer than v0.1.1 and a server-side master key; installing a CLI alone does not upgrade the server. Check `jev-router schema providers.put` for `api_key`. Have your secret manager create a mode-0600 JSON file with `id`, `base_url`, and `api_key` (exactly one of `api_key` or `secret_ref`). Do not paste keys into shell arguments/history. Then use the current version from `status.get`:

```sh
jev-router call providers.put --json /secure/provider.json \
  --expected-version VERSION --idempotency-key UNIQUE_OPERATION_KEY
```

Use HTTPS or loopback HTTP via an SSH tunnel. The CLI refuses public HTTP for inline keys and does not follow redirects. Read back `providers.get`, test a disabled model, then enable it as described in the SKILL. To replace an upstream API key, submit the same provider ID and endpoint with the new `api_key`, a fresh current version and a new idempotency key. Retry an unknown outcome with exactly the same payload/version/idempotency key within 24 hours. For endpoint-only edits, preserve the returned managed `secret_ref`. Never substitute a masked display value for the key. Dispose of temporary plaintext input files according to your secret-management policy.

Managed writes are atomic and do not restart the server. Existing in-flight requests retain old credential revisions. The master key is distinct from supplier API keys: it requires separate backup and cannot currently be rotated in place. See [configuration](configuration.md#managed-provider-credentials).
