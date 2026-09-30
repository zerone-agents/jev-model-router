import { it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { Overview, Records, Models } from "./pages";
import { createManagementClient } from "./api";
const client = (data: unknown) =>
  createManagementClient(
    "x",
    vi
      .fn()
      .mockImplementation(() =>
        Promise.resolve(
          new Response(JSON.stringify({ ok: true, data, meta: {} })),
        ),
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

it.each([false, true])(
  "only confirms returning when dirty=%s",
  async (dirty) => {
    const user = (await import("@testing-library/user-event")).default.setup();
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(
      <Models
        client={client({
          version: 1,
          items: [
            {
              id: "m",
              description: "original",
              enabled: true,
              capabilities: {},
            },
          ],
          next_cursor: "",
        })}
        lang="en"
        onError={() => {}}
        canEdit
        onDirty={() => {}}
        hasUnsavedChanges={() => dirty}
        renderEditor={() => <p>Model editor</p>}
      />,
    );
    await user.click(await screen.findByRole("button", { name: /original/ }));
    await user.click(screen.getByRole("button", { name: /All models/ }));
    expect(confirm).toHaveBeenCalledTimes(dirty ? 1 : 0);
    if (dirty) expect(screen.getByText("Model editor")).toBeInTheDocument();
    else
      expect(
        await screen.findByRole("button", { name: /original/ }),
      ).toBeInTheDocument();
    confirm.mockRestore();
  },
);

it("requests enabled-first pages, shows tags, and resets pagination on refresh", async () => {
  const user = (await import("@testing-library/user-event")).default.setup();
  const fetcher = vi.fn(async (_url, init) => {
    const { input } = JSON.parse(init.body);
    const second = Boolean(input.offset);
    return new Response(
      JSON.stringify({
        ok: true,
        meta: {},
        data: {
          version: 1,
          total: 21,
          next_cursor: second ? "" : "0:z",
          items: [
            {
              id: second ? "a-disabled" : "z-enabled",
              enabled: !second,
              location: "cloud",
              description: "test model",
              provider_id: "p",
              upstream_name: "m",
              capabilities: {
                tools: true,
                images: false,
                structured_output: true,
              },
            },
          ],
        },
      }),
    );
  });
  render(
    <Models
      client={createManagementClient("x", fetcher)}
      lang="en"
      onError={() => {}}
      canEdit={false}
      onDirty={() => {}}
    />,
  );
  await screen.findByText("z-enabled");
  expect(screen.getByText("Tools")).toBeInTheDocument();
  expect(screen.getByText("Structured output")).toBeInTheDocument();
  expect(screen.queryByText("Images")).toBeNull();
  expect(JSON.parse(fetcher.mock.calls[0][1].body).input).toEqual({
    offset: 0,
    limit: 20,
    sort: "enabled_first",
  });
  await user.click(screen.getByRole("button", { name: "Page 2" }));
  await screen.findByText("a-disabled");
  expect(screen.getByText("Disabled")).toHaveClass("disabled");
  expect(screen.getByRole("button", { name: "Page 2" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await screen.findByText("z-enabled");
  expect(screen.getByRole("button", { name: "Page 1" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await user.click(screen.getByRole("button", { name: "Models per page" }));
  await user.click(screen.getByRole("menuitemradio", { name: "50 / page" }));
  await waitFor(() =>
    expect(JSON.parse(fetcher.mock.calls.at(-1)![1].body).input).toEqual({
      offset: 0,
      limit: 50,
      sort: "enabled_first",
    }),
  );
});
