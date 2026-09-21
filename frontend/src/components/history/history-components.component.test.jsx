import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { apiDownload } from "../../lib/api";
import { HistoryDialog, StatusBadge, retryPolicyGuidance } from "./history-components";

vi.mock("../../lib/api", () => ({ apiDownload: vi.fn() }));

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

describe("history outcome uncertainty", () => {
  it("uses a visible warning status and persisted retry guidance", () => {
    const guidance = "Inspect the object metadata before submitting another mutation.";
    render(
      <>
        <StatusBadge status="outcome_unknown" />
        <HistoryDialog
          item={{
            id: 43,
            status: "outcome_unknown",
            activity_type: "action",
            action_name: "delete_object",
            target_name: "object-store",
            created_at: "2026-09-03T09:00:00Z",
            retry_policy_json: JSON.stringify({ class: "conditional", guidance }),
          }}
          onClose={() => {}}
          onAttachLabel={async () => {}}
          onDetachLabel={async () => {}}
        />
      </>,
    );

    expect(screen.getAllByText("outcome_unknown").length).toBeGreaterThan(0);
    expect(screen.getByText(/Remote outcome unknown/)).toBeInTheDocument();
    expect(screen.getByText(new RegExp(guidance))).toBeInTheDocument();
  });

  it("fails closed when persisted guidance cannot be decoded", () => {
    expect(retryPolicyGuidance({ retry_policy_json: "not-json" })).toMatch(/Inspect the target state/);
  });

  it("shows persisted retry guidance after a failed precondition", () => {
    const guidance = "Read fresh metadata and submit a new precondition.";
    render(
      <HistoryDialog
        item={{
          id: 44,
          status: "failed",
          activity_type: "action",
          action_name: "delete_object",
          target_name: "object-store",
          created_at: "2026-09-03T09:00:00Z",
          output_json: JSON.stringify({ code: "precondition_failed" }),
          retry_policy_json: JSON.stringify({ class: "conditional", guidance }),
        }}
        onClose={() => {}}
        onAttachLabel={async () => {}}
        onDetachLabel={async () => {}}
      />,
    );

    expect(screen.getByText(/Precondition failed/)).toBeInTheDocument();
    expect(screen.getByText(new RegExp(guidance))).toBeInTheDocument();
  });
});

it("allows history label suggestions to be selected with the keyboard", async () => {
  const user = userEvent.setup();
  const onAttachLabel = vi.fn().mockResolvedValue(null);
  render(
    <HistoryDialog
      item={{ id: 42, status: "completed", labels: [], target_name: "Test target", created_at: "2026-09-01T00:00:00Z" }}
      labels={[{ id: 1, name: "Investigate" }]}
      onClose={vi.fn()}
      onAttachLabel={onAttachLabel}
      onDetachLabel={vi.fn()}
    />,
  );

  const input = screen.getByRole("textbox", { name: "Add history label" });
  await user.click(input);
  await user.type(input, "Invest");
  await user.tab();
  expect(screen.getByText("Investigate").closest("button")).toHaveFocus();
  await user.keyboard("{Enter}");

  await waitFor(() => expect(onAttachLabel).toHaveBeenCalledWith(42, { name: "Investigate" }));
});

it("cancels pending history label timers when the dialog unmounts", () => {
  const clearTimeoutSpy = vi.spyOn(window, "clearTimeout");
  const { unmount } = render(
    <HistoryDialog
      item={{ id: 42, status: "completed", labels: [], target_name: "Test target", created_at: "2026-09-01T00:00:00Z" }}
      labels={[{ id: 1, name: "Investigate" }]}
      onClose={vi.fn()}
      onAttachLabel={vi.fn()}
      onDetachLabel={vi.fn()}
    />,
  );

  fireEvent.blur(screen.getByRole("textbox", { name: "Add history label" }));
  unmount();

  expect(clearTimeoutSpy).toHaveBeenCalled();
  clearTimeoutSpy.mockRestore();
});

it("streams completed transfer downloads from History", async () => {
  apiDownload.mockResolvedValueOnce({ saved: false, canceled: true, method: "picker" });
  render(
    <HistoryDialog
      item={{
        id: 45,
        status: "completed",
        activity_type: "file_transfer",
        action_name: "download",
        source_ref_id: 91,
        summary: "/var/log/app.log",
        target_name: "Test target",
        created_at: "2026-09-01T00:00:00Z",
      }}
      onClose={vi.fn()}
      onAttachLabel={vi.fn()}
      onDetachLabel={vi.fn()}
    />,
  );

  await userEvent.click(screen.getByRole("button", { name: "Save download" }));

  await waitFor(() =>
    expect(apiDownload).toHaveBeenCalledWith(
      "/api/file-transfers/91/download",
      "app.log",
      expect.objectContaining({
        picker: true,
        requireStreaming: true,
        signal: expect.any(AbortSignal),
      }),
    ),
  );
  expect(screen.queryByText(/downloaded|saved/i)).not.toBeInTheDocument();
});

it("aborts and ignores a stale History download when the selected item changes", async () => {
  let rejectFirst;
  apiDownload.mockImplementationOnce(
    (_path, _name, options) =>
      new Promise((_resolve, reject) => {
        rejectFirst = () => reject(Object.assign(new Error("aborted"), { name: "AbortError" }));
        options.signal.addEventListener("abort", rejectFirst, { once: true });
      }),
  );
  const first = {
    id: 45,
    status: "completed",
    activity_type: "file_transfer",
    action_name: "download",
    source_ref_id: 91,
    summary: "/var/log/first.log",
    target_name: "Test target",
    created_at: "2026-09-01T00:00:00Z",
  };
  const { rerender } = render(<HistoryDialog item={first} onClose={vi.fn()} onAttachLabel={vi.fn()} onDetachLabel={vi.fn()} />);
  await userEvent.click(screen.getByRole("button", { name: "Save download" }));
  const signal = apiDownload.mock.calls.at(-1)[2].signal;

  rerender(
    <HistoryDialog
      item={{ ...first, id: 46, source_ref_id: 92, summary: "/var/log/second.log" }}
      onClose={vi.fn()}
      onAttachLabel={vi.fn()}
      onDetachLabel={vi.fn()}
    />,
  );

  await waitFor(() => expect(signal.aborted).toBe(true));
  expect(rejectFirst).toBeTypeOf("function");
  expect(screen.queryByText("aborted")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Save download" })).toBeEnabled();
});

it("surfaces a current History download failure and allows another attempt", async () => {
  apiDownload.mockRejectedValueOnce(new Error("stream failed"));
  render(
    <HistoryDialog
      item={{
        id: 47,
        status: "completed",
        activity_type: "file_transfer",
        action_name: "download",
        source_ref_id: 93,
        summary: "/var/log/failed.log",
        target_name: "Test target",
        created_at: "2026-09-01T00:00:00Z",
      }}
      onClose={vi.fn()}
      onAttachLabel={vi.fn()}
      onDetachLabel={vi.fn()}
    />,
  );

  await userEvent.click(screen.getByRole("button", { name: "Save download" }));

  expect(await screen.findByText("stream failed")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Save download" })).toBeEnabled();
});

it("keeps label and download busy states independent", async () => {
  const label = deferred();
  const download = deferred();
  apiDownload.mockReturnValueOnce(download.promise);
  render(
    <HistoryDialog
      item={{
        id: 48,
        status: "completed",
        activity_type: "file_transfer",
        action_name: "download",
        source_ref_id: 94,
        summary: "/var/log/overlap.log",
        labels: [],
        target_name: "Test target",
        created_at: "2026-09-01T00:00:00Z",
      }}
      onClose={vi.fn()}
      onAttachLabel={() => label.promise}
      onDetachLabel={vi.fn()}
    />,
  );

  const input = screen.getByRole("textbox", { name: "Add history label" });
  fireEvent.change(input, { target: { value: "Investigate" } });
  fireEvent.keyDown(input, { key: "Enter" });
  expect(input).toBeDisabled();

  fireEvent.click(screen.getByRole("button", { name: "Save download" }));
  await download.resolve({ saved: true });
  await waitFor(() => expect(screen.getByRole("button", { name: "Save download" })).toBeEnabled());
  expect(input).toBeDisabled();

  label.resolve();
  await waitFor(() => expect(input).toBeEnabled());
});

it("does not let a retired label completion change the replacement item", async () => {
  const firstAttach = deferred();
  const secondAttach = deferred();
  const onAttachLabel = vi.fn().mockReturnValueOnce(firstAttach.promise).mockReturnValueOnce(secondAttach.promise);
  const base = {
    status: "completed",
    labels: [],
    target_name: "Test target",
    created_at: "2026-09-01T00:00:00Z",
  };
  const view = render(
    <HistoryDialog item={{ ...base, id: 49 }} onClose={vi.fn()} onAttachLabel={onAttachLabel} onDetachLabel={vi.fn()} />,
  );

  let input = screen.getByRole("textbox", { name: "Add history label" });
  fireEvent.change(input, { target: { value: "First" } });
  fireEvent.keyDown(input, { key: "Enter" });
  view.rerender(
    <HistoryDialog item={{ ...base, id: 50 }} onClose={vi.fn()} onAttachLabel={onAttachLabel} onDetachLabel={vi.fn()} />,
  );
  input = screen.getByRole("textbox", { name: "Add history label" });
  fireEvent.change(input, { target: { value: "Second" } });
  fireEvent.keyDown(input, { key: "Enter" });

  firstAttach.reject(new Error("retired label failed"));
  await Promise.resolve();
  expect(screen.queryByText("retired label failed")).not.toBeInTheDocument();
  expect(input).toBeDisabled();

  secondAttach.resolve();
  await waitFor(() => expect(input).toBeEnabled());
});

it("keeps reopened label suggestions visible and closes them after the next blur delay", async () => {
  render(
    <HistoryDialog
      item={{ id: 42, status: "completed", labels: [], target_name: "Test target", created_at: "2026-09-01T00:00:00Z" }}
      labels={[{ id: 1, name: "Investigate" }]}
      onClose={vi.fn()}
      onAttachLabel={vi.fn()}
      onDetachLabel={vi.fn()}
    />,
  );

  const input = screen.getByRole("textbox", { name: "Add history label" });
  fireEvent.focus(input);
  fireEvent.blur(input);
  fireEvent.focus(input);
  await new Promise((resolve) => window.setTimeout(resolve, 150));
  expect(screen.getByText("Investigate")).toBeInTheDocument();

  fireEvent.blur(input);
  await waitFor(() => expect(screen.queryByText("Investigate")).not.toBeInTheDocument());
});

it("rejects duplicate labels and surfaces failed attach and detach operations", async () => {
  const onAttachLabel = vi.fn().mockRejectedValue(new Error("attach failed"));
  const onDetachLabel = vi.fn().mockRejectedValue(new Error("detach failed"));
  render(
    <HistoryDialog
      item={{
        id: 42,
        status: "completed",
        labels: [{ id: 1, name: "Investigate" }],
        target_name: "Test target",
        created_at: "2026-09-01T00:00:00Z",
      }}
      labels={[{ id: 1, name: "Investigate" }]}
      onClose={vi.fn()}
      onAttachLabel={onAttachLabel}
      onDetachLabel={onDetachLabel}
    />,
  );

  const input = screen.getByRole("textbox", { name: "Add history label" });
  fireEvent.change(input, { target: { value: "Investigate" } });
  fireEvent.keyDown(input, { key: "Enter" });
  expect(input).toHaveValue("");
  expect(onAttachLabel).not.toHaveBeenCalled();

  fireEvent.change(input, { target: { value: "Follow up" } });
  fireEvent.keyDown(input, { key: "Enter" });
  await waitFor(() => expect(screen.getByText("attach failed")).toBeInTheDocument());

  fireEvent.click(screen.getByRole("button", { name: "Remove Investigate label" }));
  await waitFor(() => expect(screen.getByText("detach failed")).toBeInTheDocument());
});
