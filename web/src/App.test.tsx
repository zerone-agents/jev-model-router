import { it, expect, vi, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "./App";
afterEach(() => vi.unstubAllGlobals());
it("unavailable capabilities stay unavailable and disconnect clears connection state", async () => {
  localStorage.setItem("jev-language", "en");
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockImplementation(() =>
        Promise.resolve(
          new Response(JSON.stringify({ ok: true, data: [], meta: {} })),
        ),
      ),
  );
  const user = userEvent.setup();
  render(<App />);
  await user.type(screen.getByLabelText("Settings credential"), "sentinel");
  await user.click(screen.getByRole("button", { name: "Connect to instance" }));
  expect(
    await screen.findByText(
      "This capability is not available on this instance.",
    ),
  ).toBeInTheDocument();
  expect(localStorage.getItem("jev-language")).toBe("en");
  expect(JSON.stringify({ ...localStorage, ...sessionStorage })).not.toContain(
    "sentinel",
  );
  await user.click(
    screen.getByRole("button", { name: /^Disconnect$/ }),
  );
  expect(screen.getByLabelText("Settings credential")).toHaveValue("");
});
