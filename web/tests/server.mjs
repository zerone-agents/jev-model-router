import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn, execFileSync } from "node:child_process";
const dir = mkdtempSync(join(tmpdir(), "jev-ui-e2e-"));
const bin = join(dir, "jev-router");
execFileSync("go", ["build", "-o", bin, "./cmd/jev-router"], {
  cwd: "..",
  stdio: "inherit",
});
const child = spawn(bin, ["serve"], {
  stdio: "inherit",
  env: {
    ...process.env,
    JEV_ROUTER_LISTEN: "127.0.0.1:18763",
    JEV_ROUTER_DATABASE: join(dir, "router.sqlite"),
    JEV_ROUTER_SETTINGS_TOKEN: "ui-test-settings",
    JEV_ROUTER_INFERENCE_TOKEN: "ui-test-inference",
    UI_PROVIDER_KEY: "ui-test-provider",
  },
});
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, () => child.kill(signal));
child.on("exit", (code) => {
  rmSync(dir, { recursive: true, force: true });
  process.exit(code || 0);
});
