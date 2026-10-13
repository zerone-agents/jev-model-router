import { expect, test } from "vitest";
import { initialState, reducePlayground } from "./state";
test("retry keeps one turn and waits for the final model", () => {
  let state = reducePlayground(initialState(), {
    type: "start",
    id: 1,
    prompt: "hello",
  });
  state = reducePlayground(state, {
    type: "event",
    id: 1,
    event: {
      type: "retry",
      attempt: 2,
      attempts: [
        {
          index: 1,
          stage: "generation",
          model_id: "a",
          outcome: "error",
          diagnostic: { code: "upstream_error" },
        },
      ],
    },
  });
  expect(state.phase).toBe("retrying");
  expect(state.turns).toHaveLength(1);
  expect(state.turns[0].attempts?.[0].model_id).toBe("a");
});
