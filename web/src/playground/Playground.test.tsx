import { expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createManagementClient } from "../api";
import { Playground } from "./Playground";
it("defaults to auto and renders a completed routed response", async () => {
  const fetcher = vi.fn(async (path: RequestInfo | URL) =>
    String(path).endsWith("completions")
      ? new Response(
          'event: route\ndata: {"request_id":"r","model_id":"flash","path":"single_candidate","config_version":1,"decision_ms":3}\n\nevent: delta\ndata: {"reasoning_content":"hidden-secret"}\n\nevent: delta\ndata: {"content":"Hello there"}\n\nevent: done\ndata: {"finish_reason":"stop"}\n\n',
          { headers: { "Content-Type": "text/event-stream" } },
        )
      : String(path).endsWith("models.list")
        ? new Response(
            JSON.stringify({
              ok: true,
              data: {
                items: [{ id: "flash", enabled: true }],
                next_cursor: "",
              },
              meta: {},
            }),
          )
        : new Response(
            JSON.stringify({
              enabled: true,
              limits: {
                input_bytes: 32768,
                max_messages: 100,
                output_tokens: 4096,
                daily_requests: 200,
                session_rpm: 6,
                instance_rpm: 20,
              },
              timeout_seconds: 120,
              quota: {
                day_remaining: 200,
                session_remaining: 6,
                instance_remaining: 20,
                reset_at: "2030-01-01T00:00:00Z",
              },
            }),
          ),
  );
  render(
    <Playground
      client={createManagementClient("csrf", fetcher)}
      lang="en"
      onError={() => {}}
    />,
  );
  const user = userEvent.setup();
  const picker = await screen.findByRole("button", {
    name: "Model",
  });
  expect(picker).toHaveTextContent("Auto");
  await user.click(picker);
  await user.click(await screen.findByRole("menuitemradio", { name: "flash" }));
  expect(picker).toHaveTextContent("flash");
  await user.click(picker);
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  expect(picker).toHaveFocus();
  const input = screen.getByRole("textbox", { name: "Message" });
  await user.type(input, "hello");
  await user.keyboard("{Shift>}{Enter}{/Shift}world");
  expect(input).toHaveValue("hello\nworld");
  fireEvent.keyDown(input, { key: "Enter", isComposing: true });
  fireEvent.keyDown(input, { key: "Enter", keyCode: 229 });
  fireEvent.keyDown(input, { key: "Enter", repeat: true });
  expect(
    fetcher.mock.calls.filter(([path]) => String(path).endsWith("completions")),
  ).toHaveLength(0);
  expect(screen.getByText("11 / 32,768 bytes")).toBeInTheDocument();
  await user.keyboard("{Enter}");
  expect(await screen.findByText("Hello there")).toBeInTheDocument();
  expect(screen.queryByText("hidden-secret")).not.toBeInTheDocument();
  expect(screen.queryByText("Completed")).not.toBeInTheDocument();
  expect(screen.getByText("11 / 32,768 bytes")).toBeInTheDocument();
  await user.type(input, "a");
  expect(screen.getByText("36 / 32,768 bytes")).toBeInTheDocument();
});
