import { it, expect, vi } from "vitest";
import { Session } from "./session";
const info = { expires_at: "2030-01-01T00:00:00Z", csrf_token: "csrf-one" };
const reply = (data: unknown) =>
  new Response(JSON.stringify({ ok: true, data, meta: {} }));
const failure = (code: string, status: number) =>
  new Response(JSON.stringify({ ok: false, error: { code }, meta: {} }), {
    status,
  });
it("exchanges Bearer once, restores via cookie, and disposes without revoking", async () => {
  const calls: [string, RequestInit][] = [];
  const f = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    calls.push([path, init!]);
    return reply(path.endsWith("schema") ? [] : info);
  });
  const s = new Session(f);
  expect(await s.connect("secret")).toBe(true);
  expect(calls[0][1].headers).toMatchObject({ Authorization: "Bearer secret" });
  expect(calls[1][1].headers).not.toHaveProperty("Authorization");
  await s.client!.call("status.get", { input: {} });
  expect(calls[2][1]).toMatchObject({
    credentials: "same-origin",
    headers: { "X-CSRF-Token": "csrf-one" },
    cache: "no-store",
    redirect: "error",
  });
  s.dispose();
  expect(calls).toHaveLength(3);
  expect(await s.restore()).toBe(true);
  expect(calls[3][0]).toBe("/admin/v1/session");
  expect(
    calls.slice(1).every(([, init]) => !("Authorization" in init.headers!)),
  ).toBe(true);
});
it("disposal prevents pending authentication from restoring UI", async () => {
  let resolve!: (r: Response) => void;
  const f = vi.fn(
    () =>
      new Promise<Response>((r) => {
        resolve = r;
      }),
  );
  const s = new Session(f);
  const pending = s.connect("secret");
  s.dispose();
  resolve(reply(info));
  await expect(pending).resolves.toBe(false);
  expect(s.client).toBeNull();
  expect(s.capabilities).toEqual([]);
});
it("401 restores login, while network failure remains an error", async () => {
  const f = vi
    .fn()
    .mockResolvedValueOnce(failure("unauthorized", 401))
    .mockRejectedValueOnce(new Error("offline"));
  const s = new Session(f);
  expect(await s.restore()).toBe(false);
  await expect(s.restore()).rejects.toMatchObject({ code: "network_error" });
});
it("logout failure retains the current client for explicit retry; 401 confirms expiration", async () => {
  let logout = 0;
  const f = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input);
    if (path.endsWith("logout")) {
      logout++;
      if (logout === 1) throw new Error("offline");
      return failure("unauthorized", 401);
    }
    return reply(path.endsWith("schema") ? [] : info);
  });
  const s = new Session(f);
  await s.connect("secret");
  const client = s.client;
  await expect(s.logout()).rejects.toMatchObject({ code: "network_error" });
  expect(s.client).toBe(client);
  await s.logout();
  expect(s.client).toBeNull();
});
it("stale CSRF does not fetch the new token and retry logout", async () => {
  const f = vi.fn(async (input: RequestInfo | URL) =>
    String(input).endsWith("logout")
      ? failure("forbidden", 403)
      : reply(String(input).endsWith("schema") ? [] : info),
  );
  const s = new Session(f);
  await s.connect("secret");
  await expect(s.logout()).rejects.toMatchObject({ code: "session_changed" });
  expect(s.client).not.toBeNull();
  expect(f).toHaveBeenCalledTimes(3);
});
it("serializes auth actions and resolves unknown login before another mutation", async () => {
  let resolve!: (r: Response) => void;
  const f = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise<Response>((r) => {
          resolve = r;
        }),
    )
    .mockRejectedValueOnce(new Error("offline"));
  const s = new Session(f);
  const pending = s.connect("one");
  await expect(s.connect("two")).rejects.toMatchObject({ code: "auth_busy" });
  resolve(reply(info));
  await expect(pending).rejects.toMatchObject({ code: "network_error" });
  f.mockImplementation(async (input: RequestInfo | URL) =>
    reply(String(input).endsWith("schema") ? [] : info),
  );
  await s.connect("three");
  expect(f.mock.calls[2][0]).toBe("/admin/v1/session");
});
it("a late error from an old client cannot dispose the current session", async () => {
  const f = vi.fn(async (input: RequestInfo | URL) =>
    reply(String(input).endsWith("schema") ? [] : info),
  );
  const s = new Session(f);
  await s.connect("one");
  const old = s.client;
  await s.connect("two");
  s.expire(old!);
  expect(s.client).not.toBeNull();
});

it("resolves a truncated successful login through status before another login", async () => {
  const calls: string[] = [];
  const f = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input);
    calls.push(path);
    if (calls.length === 1)
      return new Response('{"ok":true,"data":', { status: 200 });
    return reply(path.endsWith("schema") ? [] : info);
  });
  const s = new Session(f);
  await expect(s.connect("secret")).rejects.toMatchObject({
    code: "invalid_response",
    status: 200,
  });
  await s.connect("secret");
  expect(calls[1]).toBe("/admin/v1/session");
});

it.each(["html", "unexpected-code"])(
  "retains logout retry state after an unconfirmed 401 (%s)",
  async (kind) => {
    let attempts = 0;
    const f = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith("logout")) {
        attempts++;
        if (attempts === 1)
          return kind === "html"
            ? new Response("<html>Proxy authentication required</html>", {
                status: 401,
              })
            : failure("proxy_auth_required", 401);
        return reply({ revoked: true });
      }
      return reply(path.endsWith("schema") ? [] : info);
    });
    const s = new Session(f);
    await s.connect("secret");
    const client = s.client;
    await expect(s.logout()).rejects.toMatchObject({
      code: kind === "html" ? "invalid_response" : "proxy_auth_required",
      status: 401,
    });
    expect(s.client).toBe(client);
    await s.logout();
    expect(s.client).toBeNull();
    expect(attempts).toBe(2);
  },
);
