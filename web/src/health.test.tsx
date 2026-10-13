import type { PageProps } from "./components";
import { act, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { HealthBadge, HealthDetails } from "./health";
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

it("polls only visible health, resumes immediately and stops after unmount", async () => {
  vi.useFakeTimers();
  let visible = "visible";
  vi.spyOn(document, "visibilityState", "get").mockImplementation(
    () => visible as DocumentVisibilityState,
  );
  const health = {
    state: "healthy",
    last_success_at: null,
    last_failure_at: null,
    next_probe_at: null,
  };
  const call = vi.fn().mockResolvedValue({ data: { resource: { health } } });
  const onHealth = vi.fn();
  const { unmount } = render(
    <HealthDetails
      client={{ call } as unknown as PageProps["client"]}
      lang="en"
      id="m"
      canTest
      onError={vi.fn()}
      onHealth={onHealth}
    />,
  );
  try {
    expect(screen.queryByText("Refresh health")).not.toBeInTheDocument();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15000);
    });
    expect(call).toHaveBeenCalledTimes(1);
    expect(call.mock.calls[0][0]).toBe("models.get");
    expect(onHealth).toHaveBeenCalledWith(health);
    visible = "hidden";
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30000);
    });
    expect(call).toHaveBeenCalledTimes(1);
    visible = "visible";
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(call).toHaveBeenCalledTimes(2);
    unmount();
    await vi.advanceTimersByTimeAsync(30000);
    expect(call).toHaveBeenCalledTimes(2);
  } finally {
    unmount();
    vi.restoreAllMocks();
    vi.useRealTimers();
  }
});
