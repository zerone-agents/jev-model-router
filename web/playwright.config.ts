import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  fullyParallel: false,
  workers: 1,
  use: { baseURL: "http://127.0.0.1:18763", locale: "en-US" },
  webServer: {
    command: "node tests/server.mjs",
    url: "http://127.0.0.1:18763/dashboard/",
    reuseExistingServer: false,
    timeout: 120000,
  },
  reporter: "list",
});
