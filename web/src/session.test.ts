import { it, expect, vi } from "vitest";
import { Session } from "./session";
it("a disconnected pending login cannot restore its token or data", async () => {
  let resolve!: (r: Response) => void;
  const s = new Session(
    vi.fn(
      () =>
        new Promise<Response>((r) => {
          resolve = r;
        }),
    ),
  );
  const pending = s.connect("secret");
  s.disconnect();
  resolve(new Response(JSON.stringify({ ok: true, data: [], meta: {} })));
  await expect(pending).resolves.toBe(false);
  expect(s.client).toBeNull();
  expect(s.capabilities).toEqual([]);
});
it("a late 401 from an old client cannot clear the new session", async () => {
  const f = vi
    .fn()
    .mockResolvedValue(
      new Response(JSON.stringify({ ok: true, data: [], meta: {} })),
    );
  const s = new Session(f);
  await s.connect("one");
  const old = s.client;
  f.mockResolvedValue(
    new Response(JSON.stringify({ ok: true, data: [], meta: {} })),
  );
  await s.connect("two");
  s.expire(old!);
  expect(s.client).not.toBeNull();
  s.disconnect();
  expect(s.capabilities).toEqual([]);
});
