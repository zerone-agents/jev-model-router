import { test, expect, type APIRequestContext } from "@playwright/test";
const headers = { Authorization: "Bearer ui-test-settings" };
async function write(request: APIRequestContext, id: string, input: unknown) {
  const s = await request.post("/admin/v1/call/status.get", {
    headers,
    data: { input: {} },
  });
  const r = await request.post(`/admin/v1/call/${id}`, {
    headers,
    data: {
      input,
      expected_version: (await s.json()).data.version,
      idempotency_key: crypto.randomUUID(),
    },
  });
  expect(r.ok()).toBeTruthy();
}
test("health sits beside enabled and a manual check restores an isolated model", async ({
  page,
  request,
}) => {
  await write(request, "providers.put", {
    id: "health",
    base_url: "http://127.0.0.1:18764/v1",
    secret_ref: "env:UI_PROVIDER_KEY",
  });
  await write(request, "models.put", {
    id: "health-model",
    provider_id: "health",
    upstream_name: "health-test",
    description: "Health test",
    enabled: true,
    location: "cloud",
    capabilities: {
      context_limit: 8192,
      tools: false,
      images: false,
      structured_output: false,
    },
  });
  const failed = await request.post("/v1/chat/completions", {
    headers: { Authorization: "Bearer ui-test-inference" },
    data: {
      model: "health-model",
      messages: [{ role: "user", content: "fail" }],
    },
  });
  expect(failed.status()).toBe(429);
  await page.goto("/dashboard/");
  await page.getByLabel("Settings credential").fill("ui-test-settings");
  await page
    .getByRole("button", { name: "Connect to instance", exact: true })
    .click();
  await page.getByRole("button", { name: "Models", exact: true }).click();
  const row = page.locator(".model-row").filter({ hasText: "health-model" });
  await expect(row.locator(".badge.enabled + .health-isolated")).toBeVisible();
  await row.click();
  await expect(page.getByText("Isolated", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Check now", exact: true }).click();
  await expect(page.getByText("Healthy", { exact: true })).toBeVisible();
  await expect(page.getByText("Connection check succeeded.")).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(
    page.getByRole("button", { name: "Check now", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
});
