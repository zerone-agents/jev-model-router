import { test, expect } from "@playwright/test";

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 390, height: 844 },
]) {
  test(`privacy prompt and records at ${viewport.width}px`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize(viewport);
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
    await page.getByRole("button", { name: "Switch language" }).click();
    await expect(page.getByRole("heading", { name: "隐私记录" })).toBeVisible();
  });
}
