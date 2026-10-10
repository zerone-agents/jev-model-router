import { expect, it } from "vitest";
import { initialState, reducePlayground, history } from "./state";
it("never returns to thinking after content and keeps reasoning separate", () => {
  let s = reducePlayground(initialState(), {
    type: "start",
    id: 1,
    prompt: "hi",
  });
  expect(s.phase).toBe("waiting");
  s = reducePlayground(s, {
    type: "event",
    id: 1,
    event: { type: "delta", reasoning_content: "hidden" },
  });
  expect(s.phase).toBe("thinking");
  s = reducePlayground(s, {
    type: "event",
    id: 1,
    event: { type: "delta", content: "visible" },
  });
  s = reducePlayground(s, {
    type: "event",
    id: 1,
    event: { type: "delta", reasoning_content: "more" },
  });
  expect(s.phase).toBe("responding");
  expect(s.turns[0].content).toBe("visible");
  s = reducePlayground(s, {
    type: "event",
    id: 1,
    event: { type: "done", finish_reason: "stop" },
  });
  expect(s.phase).toBe("completed");
  expect(history(s)).toEqual([
    { role: "user", content: "hi" },
    { role: "assistant", content: "visible", reasoning_content: "hiddenmore" },
  ]);
});
it("ignores late events and excludes partial pairs from history", () => {
  let s = reducePlayground(initialState(), {
    type: "start",
    id: 1,
    prompt: "hi",
  });
  s = reducePlayground(s, {
    type: "event",
    id: 1,
    event: { type: "delta", content: "partial" },
  });
  s = reducePlayground(s, { type: "stop", id: 1 });
  expect(history(s)).toEqual([]);
  expect(s.turns[0].content).toBe("partial");
  s = reducePlayground(s, {
    type: "event",
    id: 1,
    event: { type: "delta", content: "late" },
  });
  expect(s.turns[0].content).toBe("partial");
  s = reducePlayground(s, { type: "reset" });
  s = reducePlayground(s, {
    type: "event",
    id: 1,
    event: { type: "done", finish_reason: "stop" },
  });
  expect(s.turns).toEqual([]);
});

it("ignores empty deltas and clears thinking on failures and truncation", () => {
  let s = reducePlayground(initialState(), {
    type: "start",
    id: 9,
    prompt: "hello",
  });
  s = reducePlayground(s, {
    type: "event",
    id: 9,
    event: { type: "delta", content: "", reasoning_content: "" },
  });
  expect(s.phase).toBe("waiting");
  s = reducePlayground(s, {
    type: "event",
    id: 9,
    event: { type: "delta", reasoning_content: "hidden" },
  });
  expect(s.phase).toBe("thinking");
  s = reducePlayground(s, {
    type: "event",
    id: 9,
    event: { type: "done", finish_reason: "length" },
  });
  expect(s.phase).toBe("truncated");
  expect(history(s)).toEqual([]);
  s = reducePlayground(s, { type: "start", id: 10, prompt: "next" });
  s = reducePlayground(s, { type: "fail", id: 10 });
  expect(s.phase).toBe("failed");
  expect(history(s)).toEqual([]);
});

it("keeps each failure with its reply and preserves partial content", () => {
  let s = reducePlayground(initialState(), {
    type: "start",
    id: 1,
    prompt: "hello",
  });
  s = reducePlayground(s, {
    type: "event",
    id: 1,
    event: { type: "delta", content: "partial" },
  });
  s = reducePlayground(s, {
    type: "event",
    id: 1,
    event: { type: "error", code: "upstream_error", message: "failure" },
  });
  s = reducePlayground(s, { type: "start", id: 2, prompt: "next" });
  s = reducePlayground(s, {
    type: "fail",
    id: 2,
    errorCode: "playground_rate_limited",
  });
  expect(s.turns[0]).toMatchObject({
    content: "partial",
    errorCode: "upstream_error",
  });
  expect(s.turns[1]).toMatchObject({ errorCode: "playground_rate_limited" });
  expect(history(s)).toEqual([]);
});
