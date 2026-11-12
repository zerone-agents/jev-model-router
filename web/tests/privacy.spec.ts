import { test, expect } from "@playwright/test";

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 390, height: 844 },
]) {
  test.use({ timezoneId: "Asia/Shanghai" });
  test(`privacy prompt and records at ${viewport.width}px`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize(viewport);
    await page.route("**/admin/v1/call/privacy.get", (route) =>
      route.fulfill({
        json: { ok: true, data: { enabled: true }, meta: {} },
      }),
    );
    await page.route("**/admin/v1/call/records.list", (route) => {
      expect(route.request().postDataJSON().input).toMatchObject({
        privacy_only: true,
      });
      return route.fulfill({
        json: {
          ok: true,
          data: {
            total: 1,
            records: [
              {
                request_id: "privacy-browser-fixture",
                created_at: "2026-11-12T03:25:03.287725705Z",
                privacy_outcome: "non_sensitive",
                privacy_restriction: "unrestricted",
                privacy_duration_ms: 12,
              },
            ],
          },
          meta: {},
        },
      });
    });
    await page.goto("/dashboard/");
    await page.getByLabel("Settings credential").fill("ui-test-settings");
    await page.getByRole("button", { name: "Connect to instance" }).click();
    await expect(
      page.getByRole("heading", { name: "A clear view of your router." }),
    ).toBeVisible();
    if (viewport.width < 700)
      await page.getByRole("button", { name: "Open navigation" }).click();
    await page
      .getByRole("button", { name: "Privacy prompt", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "Privacy prompt" }),
    ).toBeVisible();
    const prompt = page.getByLabel("Prompt content");
    const text = `Treat confidential information as sensitive at ${viewport.width}.`;
    await prompt.fill(text);
    await page.getByRole("button", { name: "Save changes" }).click();
    await expect(page.getByText("Saved", { exact: false })).toBeVisible();
    const response = await request.post("/admin/v1/call/privacy.prompt.get", {
      headers: { Authorization: "Bearer ui-test-settings" },
      data: { input: {} },
    });
    expect((await response.json()).data.text).toBe(text);
    if (viewport.width < 700)
      await page.getByRole("button", { name: "Open navigation" }).click();
    await page
      .getByRole("button", { name: "Privacy records", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "Privacy records" }),
    ).toBeVisible();
    await expect(
      page.locator('small[title="2026-11-12T03:25:03.287725705Z"]'),
    ).toHaveText("11/12/2026, 11:25:03 GMT+8");
    await page.screenshot({
      path: `test-results/privacy-${viewport.width}.png`,
      fullPage: true,
    });
    await page.getByRole("button", { name: "Switch language" }).click();
    await expect(page.getByRole("heading", { name: "隐私记录" })).toBeVisible();
  });
}

for (const width of [1440, 390]) {
  test(`privacy navigation hidden when disabled at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/dashboard/");
    await page.getByLabel("Settings credential").fill("ui-test-settings");
    await page.getByRole("button", { name: "Connect to instance" }).click();
    await expect(
      page.getByRole("heading", { name: "A clear view of your router." }),
    ).toBeVisible();
    if (width < 700)
      await page.getByRole("button", { name: "Open navigation" }).click();
    await expect(
      page.getByRole("button", { name: "Privacy prompt", exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "Privacy records", exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "Routing records", exact: true }),
    ).toBeVisible();
    await page.screenshot({
      path: `test-results/privacy-disabled-${width}.png`,
      fullPage: true,
    });
  });
}
