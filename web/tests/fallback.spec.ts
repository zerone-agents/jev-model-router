import { test, expect } from "@playwright/test";

test("fallback displays final model and safe attempt details on desktop and mobile", async ({
  page,
}) => {
  const attempts = [
    {
      index: 1,
      stage: "generation",
      model_id: "first-model",
      outcome: "error",
      diagnostic: {
        code: "upstream_unavailable",
        upstream_status: 503,
        upstream_error: {
          body: { error: { message: "Provider busy" } },
          truncated: false,
        },
      },
    },
  ];
  await page.route("**/admin/v1/playground/completions", async (route) => {
    const events = [
      [
        "route",
        {
          request_id: "fallback-preview",
          model_id: "first-model",
          path: "jev_choice",
          config_version: 1,
          decision_ms: 2,
          attempt: 1,
        },
      ],
      ["retry", { attempt: 2, attempts }],
      [
        "route",
        {
          request_id: "fallback-preview",
          model_id: "second-model",
          path: "single_candidate",
          config_version: 1,
          decision_ms: 3,
          attempt: 2,
          attempts,
        },
      ],
      ["delta", { content: "The backup model completed this response." }],
      ["done", { finish_reason: "stop" }],
    ];
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: events
        .map(
          ([event, data]) =>
            `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`,
        )
        .join(""),
    });
  });
  await page.goto("/dashboard/");
  await page.getByLabel("Settings credential").fill("ui-test-settings");
  await page
    .getByRole("button", { name: "Connect to instance", exact: true })
    .click();
  await page.getByRole("button", { name: "Playground", exact: true }).click();
  await page
    .getByLabel("Message", { exact: true })
    .fill("Try another model if the first is busy.");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.locator(".pg-answer > small").first()).toHaveText(
    "second-model",
  );
  await expect(page.locator(".pg-turn")).toHaveCount(1);
  await page.getByText("Routing attempts", { exact: true }).click();
  await expect(page.getByText("HTTP 503", { exact: true })).toBeVisible();
  await page.screenshot({ path: "/tmp/fallback-desktop.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByText("HTTP 503", { exact: true })).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({ path: "/tmp/fallback-mobile.png", fullPage: true });
});
