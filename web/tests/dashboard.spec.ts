import {
  test,
  expect,
  type Page,
  type APIRequestContext,
} from "@playwright/test";
const headers = { Authorization: "Bearer ui-test-settings" };
async function call(
  request: APIRequestContext,
  id: string,
  input: unknown,
  write = false,
) {
  let envelope: Record<string, unknown> = { input };
  if (write) {
    const state = await request.post("/admin/v1/call/status.get", {
      headers,
      data: { input: {} },
    });
    envelope = {
      ...envelope,
      expected_version: (await state.json()).data.version,
      idempotency_key: crypto.randomUUID(),
    };
  }
  const r = await request.post("/admin/v1/call/" + id, {
    headers,
    data: envelope,
  });
  expect(r.ok()).toBeTruthy();
  return (await r.json()).data;
}
async function login(page: Page) {
  await page.goto("/dashboard/");
  await page.getByLabel("Settings credential").fill("ui-test-settings");
  await page
    .getByRole("button", { name: "Connect to instance", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "A clear view of your router." }),
  ).toBeVisible();
}
test.beforeAll(async ({ request }) => {
  await call(
    request,
    "providers.put",
    {
      id: "demo",
      base_url: "http://127.0.0.1:1/v1",
      secret_ref: "env:UI_PROVIDER_KEY",
    },
    true,
  );
  await call(
    request,
    "models.put",
    {
      id: "flash",
      provider_id: "demo",
      upstream_name: "mock-model",
      description: "Fast everyday tasks",
      location: "cloud",
      enabled: true,
      capabilities: {
        context_limit: 8192,
        tools: true,
        images: false,
        structured_output: true,
      },
    },
    true,
  );
});
test("real API read, edit, conflict, retry, language, disconnect and refresh", async ({
  page,
  request,
}) => {
  await login(page);
  await expect(page.getByText("Ready", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Models", exact: true }).click();
  await page.getByRole("button", { name: /flash Enabled/ }).click();
  const editor = page.getByLabel("Model description");
  await editor.fill("Updated description");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("status")).toContainText("Saved");
  expect(
    (await call(request, "models.get", { id: "flash" })).resource.description,
  ).toBe("Updated description");
  await page
    .getByRole("button", { name: "Routing prompt", exact: true })
    .click();
  const prompt = page.getByLabel("Prompt content");
  await expect(prompt).toBeVisible();
  await prompt.fill("My routing preference");
  await call(request, "prompt.put", { text: "Concurrent CLI update" }, true);
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("alert")).toContainText("Configuration changed");
  await expect(prompt).toHaveValue("My routing preference");
  await page.getByRole("button", { name: "Read latest version" }).click();
  await expect(page.getByRole("status")).toContainText("Latest version loaded");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("status")).toContainText("Saved");
  await prompt.fill("Response lost after commit");
  let calls: string[] = [];
  await page.route("**/admin/v1/call/prompt.put", async (route) => {
    calls.push(route.request().postData()!);
    if (calls.length === 1) {
      await route.fetch();
      await route.abort();
    } else await route.continue();
  });
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText(/Save outcome is unknown/)).toBeVisible();
  await page.getByRole("button", { name: "Retry same request" }).click();
  await expect(page.getByRole("status")).toContainText("Saved");
  expect(calls[0]).toBe(calls[1]);
  await page.unroute("**/admin/v1/call/prompt.put");
  await page
    .getByRole("button", { name: "Routing records", exact: true })
    .click();
  await expect(page.getByText("No routing records yet")).toBeVisible();
  expect(
    await page.evaluate(() =>
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ),
  ).not.toContain("ui-test-settings");
  await page.getByRole("button", { name: "Switch language" }).click();
  await expect(
    page.getByRole("heading", { name: "路由记录", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "断开连接", exact: true }).click();
  await expect(page.getByLabel("Settings 凭证")).toHaveValue("");
  await page.getByRole("button", { name: "切换语言" }).click();
  await login(page);
  await page.reload();
  await expect(page.getByLabel("Settings credential")).toBeVisible();
});
test("mobile navigation and visual evidence", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page);
  await page.screenshot({
    path: "test-results/mobile-overview.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "Open navigation" }).click();
  await page.getByRole("button", { name: "Models", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Models", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(390);
  await expect(
    page.getByRole("button", { name: /flash Enabled/ }),
  ).toBeVisible();
  await page.screenshot({
    path: "test-results/mobile-models.png",
    fullPage: true,
  });
});
test("desktop visual evidence and authentication boundaries", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("/dashboard/");
  await page.screenshot({ path: "test-results/desktop-connect.png" });
  await page.getByLabel("Settings credential").fill("ui-test-inference");
  await page
    .getByRole("button", { name: "Connect to instance", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("cannot manage");
  await login(page);
  await page.screenshot({ path: "test-results/desktop-overview.png" });
  await page.getByRole("button", { name: "Models", exact: true }).click();
  await expect(
    page.getByRole("button", { name: /flash Enabled/ }),
  ).toBeVisible();
  await page.screenshot({ path: "test-results/desktop-models.png" });
  expect((await request.get("/v1/models")).status()).toBe(401);
  expect((await request.get("/admin/v1/schema")).status()).toBe(401);
});

test("Chinese editor, keyboard focus, narrow screen and failure state", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/dashboard/");
  await page.getByLabel("Settings credential").focus();
  await page.keyboard.type("ui-test-settings");
  await page.keyboard.press("Tab");
  await expect(
    page.getByRole("button", { name: "Connect to instance", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("heading", { name: "A clear view of your router." }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Switch language" }).click();
  await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("button", { name: "路由提示词", exact: true }).click();
  await expect(page.getByLabel("提示词内容")).toBeVisible();
  await page.screenshot({
    path: "test-results/mobile-prompt-zh.png",
    fullPage: true,
  });
  await page.route("**/admin/v1/call/prompt.put", (route) =>
    route.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({
        ok: false,
        error: { code: "invalid_request", message: "private details" },
        meta: {},
      }),
    }),
  );
  await page.getByLabel("提示词内容").fill("测试偏好");
  await page.getByRole("button", { name: "保存修改" }).click();
  await expect(page.getByRole("alert")).toContainText("输入不符合实例契约");
  await expect(page.getByText("private details")).toHaveCount(0);
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(390);
  await page.screenshot({
    path: "test-results/mobile-error-zh.png",
    fullPage: true,
  });
});

test("returning from a saved model refreshes the registry", async ({
  page,
}) => {
  await login(page);
  await page.getByRole("button", { name: "Models", exact: true }).click();
  await page.getByRole("button", { name: /flash Enabled/ }).click();
  await page.getByLabel("Model description").fill("Fresh registry description");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("status")).toContainText("Saved");
  page.on("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "All models" }).click();
  await expect(
    page.getByRole("button", { name: /flash Enabled/ }),
  ).toContainText("Fresh registry description");
});
