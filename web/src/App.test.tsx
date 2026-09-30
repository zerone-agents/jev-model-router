import { it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "./App";
afterEach(() => vi.unstubAllGlobals());
it("unavailable capabilities stay unavailable and disconnect clears connection state", async () => {
  localStorage.setItem("jev-language", "en");
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockImplementation((path: string) =>
        Promise.resolve(
          path === "/admin/v1/session"
            ? new Response(
                JSON.stringify({
                  ok: false,
                  error: { code: "unauthorized" },
                  meta: {},
                }),
                { status: 401 },
              )
            : new Response(
                JSON.stringify({
                  ok: true,
                  data: path.endsWith("login")
                    ? { csrf_token: "csrf", expires_at: "2030-01-01T00:00:00Z" }
                    : [],
                  meta: {},
                }),
              ),
        ),
      ),
  );
  const user = userEvent.setup();
  render(<App />);
  await user.type(
    await screen.findByLabelText("Settings credential"),
    "sentinel",
  );
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
  await user.click(screen.getByRole("button", { name: /^Disconnect$/ }));
  expect(await screen.findByLabelText("Settings credential")).toHaveValue("");
});

it("restoration shows a bounded pending state and a retryable connection failure", async () => {
  localStorage.setItem("jev-language", "en");
  let reject!: (error: Error) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      () =>
        new Promise<Response>((_, r) => {
          reject = r;
        }),
    ),
  );
  render(<App />);
  expect(screen.getByText("Restoring session…")).toBeInTheDocument();
  expect(
    screen.queryByLabelText("Settings credential"),
  ).not.toBeInTheDocument();
  await waitFor(() => expect(reject).toBeTypeOf("function"));
  reject(new Error("offline"));
  expect(
    await screen.findByRole("button", { name: "Retry connection" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByLabelText("Settings credential"),
  ).not.toBeInTheDocument();
});
