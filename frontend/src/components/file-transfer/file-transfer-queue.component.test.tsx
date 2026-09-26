import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { QueueList, QueueSummary } from "./file-transfer-queue";

it("keeps local queue ordering bounded and dispatches opaque item IDs", async () => {
  const user = userEvent.setup();
  const onMove = vi.fn();
  const onRemove = vi.fn();
  render(<QueueList mode="upload" batch={null} queue={[{ id: "local-1", name: "one.txt", remote_path: "/one.txt" }, { id: "local-2", name: "two.txt", remote_path: "/two.txt" }]} active={false} canEditPausedBatch={false} onMove={onMove} onRemove={onRemove} />);
  const buttons = screen.getAllByRole("button");
  expect(buttons[0]).toBeDisabled();
  expect(buttons[4]).toBeDisabled();
  await user.click(buttons[1]);
  await user.click(buttons[5]);
  expect(onMove).toHaveBeenCalledWith("local-1", 1);
  expect(onRemove).toHaveBeenCalledWith("local-2");
});

it("permits editing only pending items in a paused remote batch", async () => {
  const onRemove = vi.fn();
  const onMove = vi.fn();
  render(<QueueList mode="download" queue={[]} batch={{ status: "paused", items: [{ id: 7, status: "completed", file_name: "done.txt" }, { id: 8, status: "pending", file_name: "one.txt" }, { id: 9, status: "pending", file_name: "two.txt" }] }} active canEditPausedBatch onMove={onMove} onRemove={onRemove} />);
  const buttons = screen.getAllByRole("button");
  expect(buttons).toHaveLength(6);
  expect(buttons[0]).toBeDisabled();
  expect(buttons[4]).toBeDisabled();
  await userEvent.click(buttons[1]);
  await userEvent.click(buttons[2]);
  expect(onMove).toHaveBeenCalledWith(8, 1);
  expect(onRemove).toHaveBeenCalledWith(8);
});

it("renders safe uncertainty feedback instead of hiding a failed transfer", () => {
  render(<QueueList mode="download" queue={[]} batch={{ status: "failed", items: [{ id: 1, status: "failed", path: "/data.txt", size_bytes: 2048, failure_kind: "outcome_unknown", error: "opaque error" }] }} active={false} canEditPausedBatch={false} onMove={vi.fn()} onRemove={vi.fn()} />);
  expect(screen.getByText(/remote operation may have completed/)).toBeInTheDocument();
  expect(screen.getByText("2.00 KiB")).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});

it("summarizes queued or active bytes without changing layout semantics", () => {
  const { rerender } = render(<QueueSummary mode="upload" batch={null} queue={[{ id: "1", size: 3 }, { id: "2", size: 4 }]} progress={{ percent: 0 }} />);
  expect(screen.getByText("2 items")).toBeInTheDocument();
  expect(screen.getByText("7 B")).toBeInTheDocument();
  rerender(<QueueSummary mode="download" batch={{ status: "running", size_bytes: 2048, total_items: 1, bytes_per_second: 1024, eta_seconds: 70 }} queue={[]} progress={{ percent: 50 }} />);
  expect(screen.getByText("1 item")).toBeInTheDocument();
  expect(screen.getByText("1.00 KiB/s")).toBeInTheDocument();
  expect(screen.getByText("1m 10s")).toBeInTheDocument();
});

it.each(["upload", "download"] as const)("renders the %s empty state", (mode) => {
  render(<QueueList mode={mode} queue={[]} batch={null} active={false} canEditPausedBatch={false} onMove={vi.fn()} onRemove={vi.fn()} />);
  expect(screen.getByText(mode === "upload" ? "Add local files to build an upload queue." : "Add remote files to build a download queue.")).toBeInTheDocument();
});
