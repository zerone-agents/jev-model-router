import { test, expect, type Page, type BrowserContext } from "@playwright/test";
const cookieName = "jev_router_session";
async function auth(
  page: Page,
  action: "login" | "status" | "logout",
  csrf = "",
) {
  return page.evaluate(
    async ({ action, csrf }) => {
      const response = await fetch(
        "/admin/v1/session" + (action === "status" ? "" : "/" + action),
        {
          method: action === "status" ? "GET" : "POST",
          credentials: "same-origin",
          headers:
            action === "login"
              ? {
                  Authorization: "Bearer ui-test-settings",
                  "Content-Type": "application/json",
                }
              : action === "logout"
                ? { "X-CSRF-Token": csrf, "Content-Type": "application/json" }
                : { "X-Jev-Session": "1" },
          body: action === "status" ? undefined : "{}",
        },
      );
      return { status: response.status, body: await response.json() };
    },
    { action, csrf },
  );
}
async function cookie(context: BrowserContext) {
  const cookies = await context.cookies();
  return cookies.find((c) => c.name === cookieName)!;
}
async function assertUsable(
  page: Page,
  context: BrowserContext,
  token: string,
) {
  expect((await cookie(context)).value).toBe(token);
  const status = await auth(page, "status");
  expect(status.status).toBe(200);
  const code = await page.evaluate(
    async (csrf) =>
      (
        await fetch("/admin/v1/call/status.get", {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
          body: '{"input":{}}',
        })
      ).status,
    status.body.data.csrf_token,
  );
  expect(code).toBe(200);
}
for (const scenario of [
  "late status 401",
  "late logout 200",
  "late logout 401",
] as const) {
  test(`${scenario} cannot erase a newer cookie in another tab`, async ({
    page,
    context,
  }) => {
    await page.goto("/dashboard/");
    const first = await auth(page, "login");
    expect(first.status).toBe(200);
    const s0 = (await cookie(context)).value;
    const tab = await context.newPage();
    await tab.goto("/dashboard/");
    await expect(
      tab.getByRole("heading", { name: "A clear view of your router." }),
    ).toBeVisible();
    const endpoint =
      scenario === "late status 401"
        ? "/admin/v1/session"
        : "/admin/v1/session/logout";
    let release!: () => void, entered!: () => void;
    const barrier = new Promise<void>((r) => {
      release = r;
    });
    const ready = new Promise<void>((r) => {
      entered = r;
    });
    await page.route(`**${endpoint}`, async (route) => {
      // Preserve the actual S0 request; a later route.fetch must not pick up S1.
      const headers = await route.request().allHeaders();
      expect(headers.cookie).toContain(s0);
      const early =
        scenario === "late logout 200"
          ? await route.fetch({ headers })
          : undefined;
      entered();
      await barrier;
      const response = early ?? (await route.fetch({ headers }));
      expect(response.status()).toBe(
        scenario === "late logout 200" ? 200 : 401,
      );
      expect(response.headers()["set-cookie"]).toBeUndefined();
      await route.fulfill({ response });
    });
    const pending = auth(
      page,
      scenario === "late status 401" ? "status" : "logout",
      first.body.data.csrf_token,
    );
    await ready;
    const second = await auth(tab, "login");
    expect(second.status).toBe(200);
    const s1 = (await cookie(context)).value;
    expect(s1).not.toBe(s0);
    release();
    await pending;
    await page.unroute(`**${endpoint}`);
    await assertUsable(tab, context, s1);
    const replay = await context.request.get("/admin/v1/session", {
      headers: { Cookie: `${cookieName}=${s0}`, "X-Jev-Session": "1" },
    });
    expect(replay.status()).toBe(401);
    expect(replay.headers()["set-cookie"]).toBeUndefined();
  });
}
test("S1 cookie with S0 CSRF cannot revoke S1", async ({ page, context }) => {
  await page.goto("/dashboard/");
  const old = await auth(page, "login");
  const tab = await context.newPage();
  await tab.goto("/dashboard/");
  await auth(tab, "login");
  const s1 = (await cookie(context)).value;
  expect((await auth(page, "logout", old.body.data.csrf_token)).status).toBe(
    403,
  );
  await assertUsable(tab, context, s1);
});
test("dashboard refresh restores HttpOnly session without persistent credentials", async ({
  page,
  context,
}) => {
  const bearerPaths: string[] = [];
  page.on("request", (request) => {
    if (request.headers().authorization)
      bearerPaths.push(new URL(request.url()).pathname);
  });
  await page.goto("/dashboard/");
  await page.getByLabel("Settings credential").fill("ui-test-settings");
  await page
    .getByRole("button", { name: "Connect to instance", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "A clear view of your router." }),
  ).toBeVisible();
  const c = await cookie(context);
  expect(c).toMatchObject({
    httpOnly: true,
    sameSite: "Strict",
    path: "/admin/",
    secure: false,
  });
  expect(c.expires - Date.now() / 1000).toBeGreaterThan(86300);
  expect(c.expires - Date.now() / 1000).toBeLessThanOrEqual(86400);
  expect(await page.evaluate(() => document.cookie)).not.toContain(cookieName);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "A clear view of your router." }),
  ).toBeVisible();
  expect(bearerPaths).toEqual(["/admin/v1/session/login"]);
  const storage = await page.evaluate(() =>
    JSON.stringify({ ...localStorage, ...sessionStorage }),
  );
  expect(storage).not.toContain("ui-test-settings");
  expect(storage).not.toContain(c.value);
  const info = await auth(page, "status");
  expect(storage).not.toContain(info.body.data.csrf_token);
  await page.getByRole("button", { name: "Disconnect", exact: true }).click();
  await expect(page.getByLabel("Settings credential")).toBeVisible();
  expect((await cookie(context)).value).toBe(c.value);
  expect((await auth(page, "status")).status).toBe(401);
});
