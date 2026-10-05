# Management UI

React/TypeScript application embedded in the Go binary at `/dashboard/`. `jev-router dashboard` opens it; `--no-open` returns the address. There is no separate production frontend process.

The UI reads status, models, the prompt and routing records. Only model descriptions and the prompt are editable. Provider setup, model mapping, capability changes and enabling/disabling models remain CLI operations. All business operations use existing management capabilities discovered from the running instance.

Settings credentials are exchanged once for a fixed 7-day HttpOnly cookie session. Refresh/reopen restores login; explicit disconnect revokes the request session and clears drafts and loaded data only after success or confirmed expiry. Remote use requires HTTPS and the configured public dashboard origin; see [session configuration](../docs/configuration.md#dashboard-sessions). Unknown write outcomes retain an identical request for explicit retry within 24 hours; conflicts preserve drafts for deliberate reconciliation. Routing records are best-effort metadata, not audit logs or conversation history.

## Development

Use Node 24 LTS and npm. Dependencies are locked in package-lock.json.

```sh
npm --prefix web ci
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build
go build -o /tmp/jev-router ./cmd/jev-router
npm --prefix web exec -- playwright install chromium
npm --prefix web run test:e2e
```

Browser tests start their own Go instance with temporary SQLite and synthetic credentials; they never call generation or decision endpoints. Port 18763 must be free. For manual development, rebuild and restart the Go binary to serve the latest same-origin assets.

`dist/` is committed so a clean Go checkout builds without Node. Rebuild and commit assets with source changes; CI rebuilds and checks all changes, including new files. Do not edit generated files directly. Fonts are bundled locally; the Zerone logo is reused from the Zerone home-page project. Radix supplies dialog behavior, and Phosphor supplies navigation icons.
