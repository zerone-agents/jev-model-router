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

it("searches while typing and expands and highlights matching descriptions", async () => {
  const user = (await import("@testing-library/user-event")).default.setup();
  const fetcher = vi.fn(
    async (_url, init) =>
      new Response(
        JSON.stringify({
          ok: true,
          meta: {},
          data: {
            version: 1,
            total: 1,
            next_cursor: "",
            items: [
              {
                id: "m",
                enabled: true,
                description: "A very long description ending with NEEDLE",
                provider_id: "p",
                upstream_name: "u",
                capabilities: {},
              },
            ],
          },
        }),
      ),
  );
  render(
    <Models
      client={createManagementClient("x", fetcher)}
      lang="en"
      onError={() => {}}
      canEdit={false}
      onDirty={() => {}}
    />,
  );
  await screen.findByText("A very long description ending with NEEDLE");
  await user.type(
    screen.getByRole("searchbox", { name: "Search models" }),
    "needle",
  );
  await waitFor(() =>
    expect(JSON.parse(fetcher.mock.calls.at(-1)![1].body).input).toMatchObject({
      query: "needle",
      offset: 0,
      language: "en",
    }),
  );
  const hit = await screen.findByText("NEEDLE");
  expect(hit.tagName).toBe("MARK");
  expect(hit.closest("p")).toHaveClass("search-expanded");
  await user.clear(screen.getByRole("searchbox", { name: "Search models" }));
  await waitFor(() => expect(document.querySelector("mark")).toBeNull());
});

it("shows reasoning combinations in model details without losing false", async () => {
  const { ModelEditor } = await import("./editors");
  render(
    <ModelEditor
      id="m"
      client={client({
        version: 1,
        resource: {
          id: "m",
          provider_id: "p",
          upstream_name: "upstream",
          description: "model",
          enabled: true,
          capabilities: {
            reasoning: [
              { enable_thinking: false },
              { reasoning_effort: "high" },
            ],
          },
        },
      })}
      lang="en"
      onError={() => {}}
      canEdit={false}
      onDirty={() => {}}
    />,
  );
  expect(
    await screen.findByText(
      'reasoning: [{"enable_thinking":false},{"reasoning_effort":"high"}]',
    ),
  ).toBeInTheDocument();
  expect(screen.queryByText(/\[object Object\]/)).not.toBeInTheDocument();
});

it("shows selection time and summaries with numbered record pagination", async () => {
  const { default: userEvent } = await import("@testing-library/user-event");
  const user = userEvent.setup();
  let total = 41;
  const fetcher = vi.fn(async (_url, init) => {
    const { input } = JSON.parse(init.body);
    return new Response(
      JSON.stringify({
        ok: true,
        meta: {},
        data: {
          total,
          next_cursor: "",
          records:
            input.offset >= total
              ? []
              : [
                  {
                    request_id: `request-${input.offset}`,
                    created_at: "2026-10-05",
                    request_summary: "<script>摘要</script>",
                    model_id: "model-a",
                    decision_ms: 12,
                    generation_ms: 900,
                    outcome: "success",
                  },
                ],
        },
      }),
    );
  });
  render(
    <Records
      client={createManagementClient("x", fetcher)}
      lang="en"
      onError={() => {}}
    />,
  );
  await screen.findByText("request-0");
  expect(screen.getByText("12 ms")).toBeInTheDocument();
  expect(screen.queryByText("912 ms")).toBeNull();
  expect(document.querySelector("script")).toBeNull();
  expect(screen.getByText("<script>摘要</script>")).toBeInTheDocument();
  expect(screen.getAllByRole("columnheader").map((x) => x.textContent)).toEqual(
    [
      "Request / time",
      "Request summary",
      "Selected model",
      "Path",
      "Model selection time",
      "Result",
    ],
  );
  expect(screen.getByText("41 records")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Page 3" }));
  await screen.findByText("request-40");
  await user.click(screen.getByRole("button", { name: "Records per page" }));
  await user.click(screen.getByRole("menuitemradio", { name: "10 / page" }));
  await screen.findByText("request-0");
  expect(JSON.parse(fetcher.mock.calls.at(-1)![1].body).input).toEqual({
    offset: 0,
    limit: 10,
  });
  // Cleanup shrinks the last page between requests; automatically recover.
  total = 1;
  await user.click(screen.getByRole("button", { name: "Page 5" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Page 1" })).toHaveAttribute(
      "aria-current",
      "page",
    ),
  );
  expect(await screen.findByText("request-0")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await screen.findByText("request-0");
  expect(JSON.parse(fetcher.mock.calls.at(-1)![1].body).input).toEqual({
    offset: 0,
    limit: 10,
  });
});
