import { expect, it } from "vitest";
import { replyErrorText } from "./errors";
it("distinguishes minute, daily and concurrency limits in both languages", () => {
  for (const scope of ["session_minute", "instance_minute"]) {
    expect(replyErrorText("playground_rate_limited", scope, "en")).toContain(
      "per-minute",
    );
    expect(replyErrorText("playground_rate_limited", scope, "zh")).toContain(
      "每分钟",
    );
  }
  expect(replyErrorText("playground_rate_limited", "day", "en")).toContain(
    "daily",
  );
  expect(replyErrorText("playground_rate_limited", "day", "zh")).toContain(
    "今日",
  );
  for (const scope of ["session_concurrency", "instance_concurrency"]) {
    expect(replyErrorText("playground_rate_limited", scope, "en")).toContain(
      "in progress",
    );
    expect(replyErrorText("playground_rate_limited", scope, "zh")).toContain(
      "同时进行",
    );
  }
  expect(replyErrorText("playground_rate_limited", "unknown", "en")).toContain(
    "Playground limit reached",
  );
});
