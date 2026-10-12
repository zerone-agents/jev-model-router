import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { HealthBadge } from "./health";
import { descriptionInput } from "./edit";
it("shows localized health states and does not mistake missing data for unknown", () => {
  const { rerender } = render(
    <HealthBadge
      lang="en"
      health={{
        state: "isolated",
        last_success_at: null,
        last_failure_at: null,
        next_probe_at: null,
      }}
    />,
  );
  expect(screen.getByText("Isolated")).toBeInTheDocument();
  rerender(
    <HealthBadge
      lang="zh"
      health={{
        state: "unknown",
        last_success_at: null,
        last_failure_at: null,
        next_probe_at: null,
      }}
    />,
  );
  expect(screen.getByText("未知")).toBeInTheDocument();
  rerender(<HealthBadge lang="en" />);
  expect(screen.getByText("Health unavailable")).toBeInTheDocument();
});
it("does not send read-only health in description writes", () => {
  expect(
    descriptionInput(
      { id: "m", description: "old", health: { state: "isolated" } },
      "new",
    ),
  ).toEqual({ id: "m", description: "new" });
});
