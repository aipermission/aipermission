import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { TransferCenter } from "./transfer-center";

const batch = {
  id: 41,
  status: "pending_approval",
  direction: "upload",
  source: "mcp",
  target_name: "Example target",
  total_items: 2,
  completed_items: 0,
  canceled_items: 0,
  failed_items: 0,
  transferred_bytes: 0,
  items: [
    { id: 101, status: "pending_approval", remote_path: "/tmp/one", size_bytes: 10 },
    { id: 102, status: "pending_approval", remote_path: "/tmp/two", size_bytes: 20 },
  ],
};

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

it("awaits approval, blocks duplicate decisions, reports failure, and permits retry", async () => {
  const user = userEvent.setup();
  const first = deferred();
  const onApprove = vi.fn().mockReturnValueOnce(first.promise).mockResolvedValueOnce();
  const onDecline = vi.fn();
  render(<TransferCenter open batches={[batch]} state="ready" onApprove={onApprove} onDecline={onDecline} />);

  await user.click(screen.getByPlaceholderText("Optional approval or rejection note."));
  await user.type(screen.getByPlaceholderText("Optional approval or rejection note."), "approved files");
  const approve = screen.getByRole("button", { name: "Approve selected (2)" });
  fireEvent.click(approve);
  fireEvent.click(approve);
  expect(onApprove).toHaveBeenCalledOnce();
  expect(onApprove).toHaveBeenCalledWith(41, [101, 102], "approved files");
  expect(screen.getByRole("button", { name: "Decline all" })).toBeDisabled();

  first.reject(new Error("Approval is stale"));
  expect(await screen.findByText("Approval is stale")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Approve selected (2)" }));
  await waitFor(() => expect(onApprove).toHaveBeenCalledTimes(2));
  expect(onDecline).not.toHaveBeenCalled();
});

it("declines a batch with its note without calling approval", async () => {
  const user = userEvent.setup();
  const onApprove = vi.fn();
  const onDecline = vi.fn().mockResolvedValue();
  render(<TransferCenter open batches={[batch]} state="ready" onApprove={onApprove} onDecline={onDecline} />);
  await user.type(screen.getByPlaceholderText("Optional approval or rejection note."), "send newer files");
  await user.click(screen.getByRole("button", { name: "Decline all" }));
  await waitFor(() => expect(onDecline).toHaveBeenCalledWith(41, "send newer files"));
  expect(onApprove).not.toHaveBeenCalled();
});

it("keeps an in-flight approval fenced when polling changes pending items", async () => {
  const approval = deferred();
  const onApprove = vi.fn(() => approval.promise);
  const view = render(<TransferCenter open batches={[batch]} state="ready" onApprove={onApprove} />);

  fireEvent.click(screen.getByRole("button", { name: "Approve selected (2)" }));
  const updated = {
    ...batch,
    items: [...batch.items, { id: 103, status: "pending_approval", remote_path: "/tmp/three", size_bytes: 30 }],
  };
  view.rerender(<TransferCenter open batches={[updated]} state="ready" onApprove={onApprove} />);

  const approve = screen.getByRole("button", { name: "Approve selected (3)" });
  expect(approve).toBeDisabled();
  fireEvent.click(approve);
  expect(onApprove).toHaveBeenCalledOnce();

  approval.reject(new Error("retired approval failed"));
  await waitFor(() => expect(approve).toBeEnabled());
  expect(screen.queryByText("retired approval failed")).not.toBeInTheDocument();
});

it("routes running and paused queue controls while keeping completed batches compact", async () => {
  const user = userEvent.setup();
  const onPause = vi.fn();
  const onResume = vi.fn();
  const onCancel = vi.fn();
  const running = { ...batch, id: 51, status: "running", direction: "download", source: "ui", items: [], bytes_per_second: 20 };
  const paused = { ...batch, id: 52, status: "paused", source: "ui", items: [] };
  const completed = { ...batch, id: 53, status: "completed", source: "ui", items: [], completed_items: 2 };
  render(
    <TransferCenter
      open
      batches={[running, paused, completed]}
      state="ready"
      error="Refresh warning"
      onPause={onPause}
      onResume={onResume}
      onCancel={onCancel}
    />,
  );

  expect(screen.getByText("Refresh warning")).toBeVisible();
  await user.click(screen.getByTitle("Pause"));
  await user.click(screen.getByTitle("Resume"));
  for (const cancel of screen.getAllByTitle("Cancel")) await user.click(cancel);
  expect(onPause).toHaveBeenCalledWith(51);
  expect(onResume).toHaveBeenCalledWith(52);
  expect(onCancel.mock.calls).toEqual([[51], [52]]);
  expect(screen.getByText("Recent")).toBeVisible();
});

it.each([
  ["Pause", "pause", "Pause failed"],
  ["Resume", "resume", "Resume failed"],
  ["Cancel", "cancel", "Cancel failed"],
])("reports a rejected %s control and clears it after a successful retry", async (title, status, message) => {
  const user = userEvent.setup();
  const handler = vi.fn().mockRejectedValueOnce(new Error(message)).mockResolvedValueOnce();
  const controlBatch = {
    ...batch,
    id: 61,
    status: status === "resume" ? "paused" : "running",
    source: "ui",
    items: [],
  };
  const props = {
    onPause: status === "pause" ? handler : vi.fn(),
    onResume: status === "resume" ? handler : vi.fn(),
    onCancel: status === "cancel" ? handler : vi.fn(),
  };
  render(<TransferCenter open batches={[controlBatch]} state="ready" {...props} />);

  await user.click(screen.getByTitle(title));
  expect(await screen.findByText(message)).toBeVisible();
  expect(screen.getByText(/0\/2 processed, 0 completed/)).toBeVisible();

  await user.click(screen.getByTitle(title));
  await waitFor(() => expect(handler).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(screen.queryByText(message)).not.toBeInTheDocument());
});

it("blocks concurrent transfer controls while one action is pending", async () => {
  const first = deferred();
  const onPause = vi.fn().mockReturnValue(first.promise);
  const onCancel = vi.fn();
  const running = { ...batch, id: 62, status: "running", source: "ui", items: [] };
  render(<TransferCenter open batches={[running]} state="ready" onPause={onPause} onCancel={onCancel} />);

  fireEvent.click(screen.getByTitle("Pause"));
  fireEvent.click(screen.getByTitle("Pause"));
  expect(screen.getByTitle("Pause")).toBeDisabled();
  expect(screen.getByTitle("Cancel")).toBeDisabled();
  fireEvent.click(screen.getByTitle("Cancel"));
  expect(onPause).toHaveBeenCalledOnce();
  expect(onCancel).not.toHaveBeenCalled();

  first.reject(new Error("Pause failed"));
  expect(await screen.findByText("Pause failed")).toBeVisible();
  expect(screen.getByTitle("Cancel")).toBeEnabled();
});

it("shows an empty loading state and disables refresh", () => {
  render(<TransferCenter open batches={[]} state="loading" />);
  expect(screen.getByText("Loading transfer queues...")).toBeVisible();
  expect(screen.getByText("No active transfers.")).toBeVisible();
  expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
});

it("ignores a control result after the batch state changes", async () => {
  const pending = deferred();
  const running = { ...batch, id: 71, status: "running", source: "ui", items: [] };
  const view = render(<TransferCenter open batches={[running]} state="ready" onPause={() => pending.promise} />);

  fireEvent.click(screen.getByTitle("Pause"));
  view.rerender(<TransferCenter open batches={[{ ...running, status: "paused" }]} state="ready" />);
  pending.reject(new Error("late failure"));
  await Promise.resolve();

  expect(screen.queryByText("late failure")).not.toBeInTheDocument();
  expect(screen.getByTitle("Resume")).toBeEnabled();
  fireEvent.click(screen.getByTitle("Resume"));
  await waitFor(() => expect(screen.getByTitle("Resume")).toBeEnabled());
});

it("ignores a successful control result after the batch state changes", async () => {
  const pending = deferred();
  const running = { ...batch, id: 73, status: "running", source: "ui", items: [] };
  const view = render(<TransferCenter open batches={[running]} state="ready" onPause={() => pending.promise} />);

  fireEvent.click(screen.getByTitle("Pause"));
  view.rerender(<TransferCenter open batches={[{ ...running, status: "paused" }]} state="ready" />);
  pending.resolve();
  await Promise.resolve();

  expect(screen.getByTitle("Resume")).toBeEnabled();
});

it("uses the control fallback when a rejection has no message", async () => {
  const onPause = vi.fn().mockRejectedValue(null);
  const running = { ...batch, id: 72, status: "running", source: "ui", items: [] };
  render(<TransferCenter open batches={[running]} state="ready" onPause={onPause} />);

  fireEvent.click(screen.getByTitle("Pause"));
  expect(await screen.findByText("Could not pause this transfer.")).toBeVisible();
});
