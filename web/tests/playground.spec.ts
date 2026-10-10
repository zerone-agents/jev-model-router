import {
  test,
  expect,
  type Page,
  type APIRequestContext,
} from "@playwright/test";
const headers = { Authorization: "Bearer ui-test-settings" };
async function call(
  r: APIRequestContext,
  id: string,
  input: unknown,
  write = false,
) {
  let extra = {};
  if (write) {
    const state = await r.post("/admin/v1/call/status.get", {
      headers,
      data: { input: {} },
    });
    extra = {
      expected_version: (await state.json()).data.version,
      idempotency_key: crypto.randomUUID(),
    };
  }
  const res = await r.post("/admin/v1/call/" + id, {
    headers,
    data: { input, ...extra },
  });
  expect(res.ok()).toBeTruthy();
  return (await res.json()).data;
}
async function login(page: Page) {
  await page.goto("/dashboard/");
  await page.getByLabel("Settings credential").fill("ui-test-settings");
  await page
    .getByRole("button", { name: "Connect to instance", exact: true })
    .click();
  await page.getByRole("button", { name: "Playground", exact: true }).click();
  await expect(page.getByLabel("Model")).toBeVisible();
}
async function send(page: Page, prompt: string) {
  await page.getByLabel("Message", { exact: true }).fill(prompt);
  await page.getByRole("button", { name: "Send", exact: true }).click();
}
test.beforeAll(async ({ request }) => {
  const list = await call(request, "models.list", { limit: 200 });
  for (const m of list.items) {
    if (m.enabled)
      await call(request, "models.put", { ...m, enabled: false }, true);
  }
  await call(
    request,
    "providers.put",
    {
      id: "pg-test",
      base_url: "http://127.0.0.1:18764/v1",
      secret_ref: "env:UI_PROVIDER_KEY",
    },
    true,
  );
  await call(
    request,
    "models.put",
    {
      id: "pg-model",
      provider_id: "pg-test",
      upstream_name: "gpt-4o-mini",
      description: "Playground test",
      enabled: true,
      location: "cloud",
      capabilities: {
        context_limit: 64000,
        images: false,
        tools: false,
        structured_output: false,
      },
    },
    true,
  );
});
test("auto routes a real stream, hides reasoning and preserves production CSP", async ({
  page,
}) => {
  await login(page);
  const external: string[] = [];
  page.on("request", (r) => {
    if (r.url().startsWith("https://pg-external.invalid"))
      external.push(r.url());
  });
  const violations: string[] = [];
  await page.exposeFunction("reportPGViolation", (s: string) =>
    violations.push(s),
  );
  await page.evaluate(() => {
    document.addEventListener("securitypolicyviolation", (e) =>
      (
        window as unknown as { reportPGViolation: (s: string) => void }
      ).reportPGViolation(e.violatedDirective),
    );
  });
  await send(page, "markdown");
  await expect(page.getByText("Thinking…", { exact: true })).toBeVisible();
  await expect(page.getByText("Completed", { exact: true })).toBeVisible();
  await expect(page.locator(".pg-markdown strong")).toHaveText("中文");
  await expect(page.locator(".pg-markdown code")).toContainText(
    "const answer = 42",
  );
  await expect(
    page.getByText("hidden-reasoning-sentinel", { exact: false }),
  ).toHaveCount(0);
  await expect(page.locator(".pg-detail dd").first()).toHaveText("pg-model");
  expect(external).toEqual([]);
  const documentResponse = await page.request.get("/dashboard/");
  expect(documentResponse.headers()["content-security-policy"]).toContain(
    "img-src 'self' data:",
  );
  await page.evaluate(() => {
    const img = document.createElement("img");
    img.src = "http://127.0.0.1:18764/csp-probe";
    document.body.append(img);
  });
  await expect.poll(() => violations).toContain("img-src");
  expect(external).toEqual([]);
  expect(
    await (await page.request.get("http://127.0.0.1:18764/probe-count")).text(),
  ).toBe("0");
  expect(
    await page.evaluate(() =>
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ),
  ).not.toContain("hidden-reasoning-sentinel");
  await page.screenshot({
    path: "test-results/playground-desktop.png",
    fullPage: true,
  });
});
test("stop clears thinking and another tab cannot bypass session concurrency", async ({
  page,
  context,
}) => {
  await login(page);
  await send(page, "slow");
  await expect(page.getByText("Thinking…", { exact: true })).toBeVisible();
  const other = await context.newPage();
  await other.goto("/dashboard/");
  await other.getByRole("button", { name: "Playground", exact: true }).click();
  await other.getByLabel("Model").selectOption("pg-model");
  await send(other, "hello");
  await expect(
    other.getByText("Playground limit reached. Wait before sending again."),
  ).toBeVisible();
  await page.getByRole("button", { name: "Stop", exact: true }).click();
  await expect(page.getByText("Stopped", { exact: true })).toBeVisible();
  await expect(page.getByText("Thinking…", { exact: true })).toHaveCount(0);
  await other.close();
});
test("mobile explicit model and Chinese UI remain usable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/dashboard/");
  await page.getByLabel("Settings credential").fill("ui-test-settings");
  await page
    .getByRole("button", { name: "Connect to instance", exact: true })
    .click();
  await page.getByRole("button", { name: "Open navigation" }).click();
  await page
    .getByRole("button", { name: "Playground", exact: true })
    .last()
    .click();
  await page.getByLabel("Model").selectOption("pg-model");
  await send(page, "hello");
  await expect(page.getByText("Completed", { exact: true })).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: "test-results/playground-mobile.png",
    fullPage: true,
  });
  await page.evaluate(() => localStorage.setItem("jev-language", "zh"));
  await page.reload();
  await page.getByRole("button", { name: "打开导航" }).click();
  await page
    .getByRole("button", { name: "Playground", exact: true })
    .last()
    .click();
  await expect(page.getByLabel("消息", { exact: true })).toBeVisible();
});
