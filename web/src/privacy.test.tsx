import { expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createManagementClient } from "./api";
import { PromptEditor } from "./editors";
import { PrivacyRecords } from "./pages";

it("edits the privacy prompt through the privacy management capability", async () => {
  const fetcher = vi.fn().mockImplementation((path: string) =>
    Promise.resolve(
      new Response(
        JSON.stringify({
          ok: true,
          data: path.endsWith("privacy.prompt.put")
            ? { version: 4 }
            : { version: 3, text: "Initial" },
          meta: {},
        }),
      ),
    ),
  );
  const client = createManagementClient("x", fetcher);
  render(
    <PromptEditor
      client={client}
      lang="en"
      onError={() => {}}
      onDirty={() => {}}
      canEdit
      privacyPrompt
    />,
  );
  const editor = await screen.findByLabelText("Prompt content");
  fireEvent.change(editor, { target: { value: "Protect secrets" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some((call) =>
        String(call[0]).endsWith("privacy.prompt.put"),
      ),
    ).toBe(true),
  );
  const write = fetcher.mock.calls.find((call) =>
    String(call[0]).endsWith("privacy.prompt.put"),
  );
  expect(JSON.parse(write![1].body)).toMatchObject({
    input: { text: "Protect secrets" },
    expected_version: 3,
  });
});

it.each(["en", "zh"] as const)(
  "renders privacy metadata without exposing classifier input in %s",
  async (lang) => {
    const data = {
      total: 1,
      records: [
        {
          request_id: "one",
          created_at: "2026-01-01T00:00:00Z",
          privacy_outcome: "uncertain",
          privacy_duration_ms: 0,
          privacy_restriction: "local_only",
          privacy_failure: "classification_unavailable",
        },
      ],
    };
    const fetcher = vi
      .fn()
      .mockImplementation(() =>
        Promise.resolve(
          new Response(JSON.stringify({ ok: true, data, meta: {} })),
        ),
      );
    const client = createManagementClient("x", fetcher);
    render(<PrivacyRecords client={client} lang={lang} onError={() => {}} />);
    expect(
      await screen.findByText(lang === "zh" ? "不确定" : "Uncertain"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(lang === "zh" ? "仅本地" : "Local only"),
    ).toBeInTheDocument();
    expect(screen.getByText("0 ms")).toBeInTheDocument();
    expect(screen.queryByText("old")).not.toBeInTheDocument();
    const timestamp = document.querySelector(
      "small[title='2026-01-01T00:00:00Z']",
    );
    expect(timestamp).not.toHaveTextContent("2026-01-01T00:00:00Z");
    expect(timestamp?.textContent).toMatch(/GMT|UTC/);
    expect(JSON.parse(fetcher.mock.calls[0][1].body).input).toEqual({
      offset: 0,
      limit: 20,
      privacy_only: true,
    });
    fireEvent.click(screen.getByText("one"));
    expect(screen.getByText(/"privacy_outcome": "uncertain"/)).toBeVisible();
  },
);
