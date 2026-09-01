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
  let reject;
  const promise = new Promise((_resolve, rejectPromise) => {
    reject = rejectPromise;
  });
  return { promise, reject };
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

it("shows an empty loading state and disables refresh", () => {
  render(<TransferCenter open batches={[]} state="loading" />);
  expect(screen.getByText("Loading transfer queues...")).toBeVisible();
  expect(screen.getByText("No active transfers.")).toBeVisible();
  expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
});
