import { it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { Overview, Records, Models } from "./pages";
import { createManagementClient } from "./api";
const client = (data: unknown) =>
  createManagementClient(
    "x",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ ok: true, data, meta: {} })),
      ),
  );
it("shows readiness separately from actual upstream health", async () => {
  render(
    <Overview
      client={client({
        ready: true,
        version: 3,
        enabled_models: 2,
        records_degraded: true,
      })}
      lang="en"
      onError={() => {}}
    />,
  );
  expect(await screen.findByText("Ready")).toBeInTheDocument();
  expect(screen.getByText(/not an upstream health check/)).toBeInTheDocument();
  expect(screen.getByText(/degraded/i)).toBeInTheDocument();
});
it("empty records do not fabricate metrics", async () => {
  render(
    <Records
      client={client({ records: [], next_cursor: "" })}
      lang="en"
      onError={() => {}}
    />,
  );
  expect(await screen.findByText("No routing records yet")).toBeInTheDocument();
});
it("model descriptions are rendered as text", async () => {
  render(
    <Models
      client={client({
        version: 1,
        items: [
          {
            id: "m",
            description: "<script>evil()</script>",
            enabled: true,
            capabilities: {},
          },
        ],
        next_cursor: "",
      })}
      lang="en"
      onError={() => {}}
      canEdit={false}
      onDirty={() => {}}
    />,
  );
  await waitFor(() =>
    expect(screen.getByText("<script>evil()</script>")).toBeInTheDocument(),
  );
  expect(document.querySelector("script")).toBeNull();
});

it("late model details cannot replace the newly selected model", async () => {
  const pending: Array<(r: Response) => void> = [];
  const c = createManagementClient(
    "x",
    vi.fn(() => new Promise<Response>((r) => pending.push(r))),
  );
  const { ModelRead } = await import("./pages");
  const props = { client: c, lang: "en" as const, onError: () => {} };
  const view = render(<ModelRead {...props} id="old" />);
  view.rerender(<ModelRead {...props} id="new" />);
  pending[1](
    new Response(
      JSON.stringify({
        ok: true,
        data: { version: 2, resource: { id: "new" } },
        meta: {},
      }),
    ),
  );
  await screen.findByText(/"new"/);
  pending[0](
    new Response(
      JSON.stringify({
        ok: true,
        data: { version: 1, resource: { id: "old" } },
        meta: {},
      }),
    ),
  );
  await waitFor(() =>
    expect(screen.queryByText(/"old"/)).not.toBeInTheDocument(),
  );
});
