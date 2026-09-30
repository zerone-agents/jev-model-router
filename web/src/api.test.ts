import { describe, it, expect, vi } from "vitest";
import { createManagementClient, APIError, secureOrigin } from "./api";
describe("management client", () => {
  it("calls only same-origin paths with a cookie session, and rejects redirects", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ ok: true, data: [], meta: {} })),
      );
    await createManagementClient("sentinel", fetcher).schema();
    expect(fetcher.mock.calls[0][1].headers).not.toHaveProperty(
      "Authorization",
    );
    expect(fetcher.mock.calls[0][0]).toBe("/admin/v1/schema");
    expect(fetcher.mock.calls[0][1]).toMatchObject({
      headers: { "X-Jev-Session": "1" },
      redirect: "error",
      credentials: "same-origin",
    });
  });
  it("never echoes server error messages", async () => {
    const client = createManagementClient(
      "sentinel",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            ok: false,
            error: { code: "forbidden", message: "sentinel" },
            meta: { operation_id: "op" },
          }),
          { status: 403 },
        ),
      ),
    );
    await expect(client.schema()).rejects.toMatchObject({
      code: "forbidden",
      status: 403,
      message: "forbidden",
      operationId: "op",
    });
  });
  it("classifies lost responses as unknown without retrying", async () => {
    const fetcher = vi.fn().mockRejectedValue(new Error("sentinel"));
    const client = createManagementClient("sentinel", fetcher);
    await expect(
      client.call("prompt.put", {
        input: { text: "x" },
        expected_version: 1,
        idempotency_key: "k",
      }),
    ).rejects.toBeInstanceOf(APIError);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("allows loopback HTTP but requires HTTPS remotely", () => {
    expect(secureOrigin(new URL("http://127.0.0.1:8080"))).toBe(true);
    expect(secureOrigin(new URL("http://[::1]:8080"))).toBe(true);
    expect(secureOrigin(new URL("http://example.com"))).toBe(false);
    expect(secureOrigin(new URL("https://example.com"))).toBe(true);
  });
});
