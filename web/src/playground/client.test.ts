import { expect, it } from "vitest";
import { consumePlayground, type PlaygroundEvent } from "./client";
it("decodes UTF-8 split across chunks and requires a terminal event", async () => {
  const bytes = new TextEncoder().encode(
    'event: delta\r\ndata: {"content":"中文"}\r\n\r\nevent: done\ndata: {"finish_reason":"stop"}\n\n',
  );
  const response = new Response(
    new ReadableStream({
      start(c) {
        for (const b of bytes) c.enqueue(new Uint8Array([b]));
        c.close();
      },
    }),
    { headers: { "Content-Type": "text/event-stream" } },
  );
  const events: unknown[] = [];
  await consumePlayground(response, (e) => events.push(e));
  expect(events).toEqual([
    { type: "delta", content: "中文" },
    { type: "done", finish_reason: "stop" },
  ]);
  await expect(
    consumePlayground(
      new Response('event: delta\ndata: {"content":"partial"}\n\n', {
        headers: { "Content-Type": "text/event-stream" },
      }),
      () => {},
    ),
  ).rejects.toThrow("interrupted");
});
it("rejects invalid events and preserves rate-limit metadata", async () => {
  await expect(
    consumePlayground(
      new Response('event: delta\ndata: {"content":3}\n\n', {
        headers: { "Content-Type": "text/event-stream" },
      }),
      () => {},
    ),
  ).rejects.toThrow("invalid_response");
  await expect(
    consumePlayground(
      new Response(
        JSON.stringify({
          error: {
            code: "playground_rate_limited",
            retry_after_seconds: 60,
            limit_scope: "day",
          },
        }),
        { status: 429, headers: { "Retry-After": "60" } },
      ),
      () => {},
    ),
  ).rejects.toMatchObject({
    code: "playground_rate_limited",
    retryAfter: 60,
    scope: "day",
  });
});

it("parses multiline SSE data across every chunk size", async () => {
  const bytes = new TextEncoder().encode(
    'event: delta\r\ndata: {\r\ndata: "content":"中文"}\r\n\r\nevent: done\r\ndata: {"finish_reason":"stop"}\r\n\r\n',
  );
  for (let size = 1; size <= bytes.length; size++) {
    const response = new Response(
      new ReadableStream({
        start(controller) {
          for (let offset = 0; offset < bytes.length; offset += size)
            controller.enqueue(bytes.slice(offset, offset + size));
          controller.close();
        },
      }),
      { headers: { "Content-Type": "text/event-stream" } },
    );
    const events: unknown[] = [];
    await consumePlayground(response, (event) => events.push(event));
    expect(events).toEqual([
      { type: "delta", content: "中文" },
      { type: "done", finish_reason: "stop" },
    ]);
  }
});
it("preserves safe generation diagnostics after route and partial content", async () => {
  const events: PlaygroundEvent[] = [];
  await expect(
    consumePlayground(
      new Response(
        'event: route\ndata: {"model_id":"m","request_id":"r","path":"explicit","config_version":1,"decision_ms":1}\n\nevent: delta\ndata: {"content":"partial"}\n\nevent: error\ndata: {"code":"upstream_rate_limit","message":"Provider rate limit reached","stage":"generation","request_id":"r","upstream_status":429}\n\n',
        { headers: { "Content-Type": "text/event-stream" } },
      ),
      (e) => events.push(e),
    ),
  ).rejects.toThrow("upstream_rate_limit");
  expect(events[0]).toMatchObject({ model_id: "m" });
  expect(events[2]).toMatchObject({
    stage: "generation",
    request_id: "r",
    upstream_status: 429,
  });
});
it("preserves request diagnostics before SSE starts", async () => {
  await expect(
    consumePlayground(
      new Response(
        JSON.stringify({
          error: { code: "no_candidates", stage: "routing", request_id: "r" },
        }),
        { status: 422 },
      ),
      () => {},
    ),
  ).rejects.toMatchObject({
    diagnostic: { stage: "routing", request_id: "r" },
  });
});

it("preserves upstream details for JSON and SSE failures", async () => {
  const upstream_error = {
    body: {
      detail: {
        title: "Typesafe is not available in your region.",
        status: 451,
      },
    },
    truncated: false,
  };
  await expect(
    consumePlayground(
      new Response(
        JSON.stringify({ error: { code: "upstream_error", upstream_error } }),
        { status: 451 },
      ),
      () => {},
    ),
  ).rejects.toMatchObject({ diagnostic: { upstream_error } });
  const events: PlaygroundEvent[] = [];
  await expect(
    consumePlayground(
      new Response(
        `event: error\ndata: ${JSON.stringify({ code: "upstream_error", message: "failed", upstream_error })}\n\n`,
        { headers: { "Content-Type": "text/event-stream" } },
      ),
      (e) => events.push(e),
    ),
  ).rejects.toThrow("upstream_error");
  expect(events[0]).toMatchObject({ upstream_error });
});
