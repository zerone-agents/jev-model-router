import { test, expect } from "@playwright/test";

test("real routing records show summaries, selection timing and numbered pages", async ({
  page,
  request,
}, testInfo) => {
  // Invalid model requests exercise the actual recorder without any upstream call.
  for (let i = 0; i < 25; i++) {
    const response = await request.post("/v1/chat/completions", {
      headers: { Authorization: "Bearer ui-test-inference" },
      data: {
        model: "missing-record-test-model",
        messages: [
          {
            role: "user",
            content: `请求摘要 ${i}：请比较两个模型的编程能力，并说明适合哪些任务。`,
          },
        ],
      },
    });
    expect(response.ok()).toBe(false);
  }
  const headers = { Authorization: "Bearer ui-test-settings" };
  const response = await request.post("/admin/v1/call/records.list", {
    headers,
    data: { input: { offset: 0, limit: 20 } },
  });
  expect(response.ok()).toBe(true);
  const { data } = await response.json();
  expect(data.total).toBeGreaterThanOrEqual(25);
  expect(data.records).toHaveLength(20);
  expect(data.records[0].request_summary).toContain("请求摘要 24");
  expect(data.records[0].decision_ms).toBeGreaterThanOrEqual(0);
  expect(data.records[0]).not.toHaveProperty("generation_ms");
  const mixed = await request.post("/admin/v1/call/records.list", {
    headers,
    data: { input: { offset: 1, cursor: data.next_cursor } },
  });
  expect(mixed.status()).toBe(400);

  await page.goto("/dashboard/");
  await page.getByLabel("Settings credential").fill("ui-test-settings");
  await page
    .getByRole("button", { name: "Connect to instance", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Routing records", exact: true })
    .click();
  await expect(
    page.getByRole("columnheader", { name: "Model selection time" }),
  ).toBeVisible();
  await expect(page.locator("tbody tr")).toHaveCount(20);
  await expect(
    page.getByText(data.records[0].request_summary, { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Page 2", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Page 2", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await expect(
    page.getByText(data.records[0].request_summary, { exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Page 1", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await page
    .getByRole("button", { name: "Records per page", exact: true })
    .click();
  await page.getByRole("menuitemradio", { name: "10 / page" }).click();
  await expect(page.locator("tbody tr")).toHaveCount(10);
  await page.getByRole("button", { name: "Switch language" }).click();
  await expect(
    page.getByRole("columnheader", { name: "选模型耗时" }),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("records-desktop.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(
    page.getByRole("navigation", { name: "记录分页" }),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("records-mobile.png"),
    fullPage: true,
  });
});
