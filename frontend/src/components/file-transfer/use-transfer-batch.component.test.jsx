import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost, apiPostForm } from "../../lib/api";
import { useTransferBatch } from "./use-transfer-batch";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPostForm: vi.fn() }));

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function BatchHarness({ onNotice = vi.fn(), onUploadCompleted = vi.fn() }) {
  const file = new File(["payload"], "a.txt", { type: "text/plain" });
  const transfer = useTransferBatch({
    open: true,
    runtimeTarget: { id: 7 },
    mode: "upload",
    remoteDir: "/tmp",
    uploadQueue: [{ id: "a", name: "a.txt", relative_path: "a.txt", file, size: file.size }],
    downloadQueue: [],
    queue: [{ id: "a" }],
    onNotice,
    onUploadCompleted,
  });
  return (
    <div>
      <button type="button" onClick={() => void transfer.startQueue()}>
        Start
      </button>
      <button type="button" onClick={() => void transfer.pauseBatch()}>
        Pause
      </button>
      <button type="button" onClick={() => void transfer.resumeBatch()}>
        Resume
      </button>
      <button type="button" onClick={() => void transfer.cancelBatch()}>
        Cancel
      </button>
      <button type="button" onClick={() => transfer.resetBatch()}>
        Reset
      </button>
      <p data-testid="status">{transfer.batch.item?.status || transfer.batch.state}</p>
      <p data-testid="conflicts">{transfer.overwritePrompt?.length || 0}</p>
    </div>
  );
}

function DownloadBatchHarness({ onNotice = vi.fn() }) {
  const transfer = useTransferBatch({
    open: true,
    runtimeTarget: { id: 9 },
    mode: "download",
    remoteDir: "/tmp",
    uploadQueue: [],
    downloadQueue: [{ id: "remote", path: "/var/log/app.log", relative_path: "app.log" }],
    queue: [{ id: "remote" }],
    onNotice,
    onUploadCompleted: vi.fn(),
  });
  return (
    <div>
      <button type="button" onClick={() => void transfer.startQueue()}>
        Start download
      </button>
      <button type="button" onClick={() => void transfer.refreshBatch()}>
        Refresh download
      </button>
      <p data-testid="download-status">{transfer.batch.item?.status || transfer.batch.state}</p>
    </div>
  );
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  apiPostForm.mockReset();
});

it("owns upload creation and ordered pause, resume, and cancel transitions", async () => {
  const user = userEvent.setup();
  const onNotice = vi.fn();
  apiPostForm.mockResolvedValue({ id: 12, status: "running", direction: "upload", items: [] });
  apiPost.mockImplementation((path) => {
    const action = path.split("/").at(-1);
    return Promise.resolve({
      id: 12,
      status: action === "pause" ? "paused" : action === "resume" ? "running" : "canceled",
      direction: "upload",
    });
  });
  render(<BatchHarness onNotice={onNotice} />);

  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(await screen.findByTestId("status")).toHaveTextContent("running");
  const form = apiPostForm.mock.calls[0][1];
  expect(form.get("runtime_id")).toBe("7");
  expect(form.get("remote_dir")).toBe("/tmp");
  expect(JSON.parse(form.get("relative_paths"))).toEqual(["a.txt"]);
  expect(form.get("idempotency_key")).toMatch(/^[0-9a-f-]{36}$/);

  await user.click(screen.getByRole("button", { name: "Pause" }));
  expect(apiPost).toHaveBeenLastCalledWith("/api/file-transfer-batches/12/pause", {}, { signal: expect.any(AbortSignal) });
  expect(screen.getByTestId("status")).toHaveTextContent("paused");
  await user.click(screen.getByRole("button", { name: "Resume" }));
  expect(apiPost).toHaveBeenLastCalledWith("/api/file-transfer-batches/12/resume", {}, { signal: expect.any(AbortSignal) });
  expect(screen.getByTestId("status")).toHaveTextContent("running");
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(apiPost).toHaveBeenCalledWith("/api/file-transfer-batches/12/cancel", {}, { signal: expect.any(AbortSignal) });
  expect(screen.getByTestId("status")).toHaveTextContent("canceled");
  expect(onNotice).toHaveBeenLastCalledWith({ tone: "warn", message: "Transfer queue canceled." });
});

it("publishes an upload completion once", async () => {
  const user = userEvent.setup();
  const onUploadCompleted = vi.fn();
  apiPostForm.mockResolvedValue({ id: 12, status: "completed", direction: "upload", items: [] });
  render(<BatchHarness onUploadCompleted={onUploadCompleted} />);

  await user.click(screen.getByRole("button", { name: "Start" }));
  await waitFor(() => expect(onUploadCompleted).toHaveBeenCalledOnce());
});

it("owns upload overwrite conflicts without creating a batch", async () => {
  const user = userEvent.setup();
  apiPostForm.mockRejectedValue(
    Object.assign(new Error("conflict"), { status: 409, data: { code: "remote_files_exist", conflicts: [{}] } }),
  );
  render(<BatchHarness />);

  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(await screen.findByTestId("conflicts")).toHaveTextContent("1");
  expect(screen.getByTestId("status")).toHaveTextContent("idle");
});

it("reuses the upload idempotency key when a response is lost", async () => {
  const user = userEvent.setup();
  apiPostForm.mockRejectedValueOnce(new Error("network response lost")).mockResolvedValueOnce({
    id: 12,
    status: "running",
    direction: "upload",
    items: [],
  });
  render(<BatchHarness />);

  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(await screen.findByTestId("status")).toHaveTextContent("error");
  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(await screen.findByTestId("status")).toHaveTextContent("running");

  expect(apiPostForm).toHaveBeenCalledTimes(2);
  expect(apiPostForm.mock.calls[1][1].get("idempotency_key")).toBe(apiPostForm.mock.calls[0][1].get("idempotency_key"));
});

it("ignores upload completion after the dialog batch is reset", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPostForm.mockReturnValue(pending.promise);
  render(<BatchHarness />);

  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(screen.getByTestId("status")).toHaveTextContent("starting");
  await user.click(screen.getByRole("button", { name: "Reset" }));
  pending.resolve({ id: 12, status: "running", direction: "upload", items: [] });

  await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("idle"));
});

it("keeps the latest transition when an older transition completes last", async () => {
  const user = userEvent.setup();
  const paused = deferred();
  apiPostForm.mockResolvedValue({ id: 12, status: "running", direction: "upload", items: [] });
  apiPost.mockReturnValueOnce(paused.promise).mockResolvedValueOnce({ id: 12, status: "canceled", direction: "upload", items: [] });
  render(<BatchHarness />);
  await user.click(screen.getByRole("button", { name: "Start" }));

  await user.click(screen.getByRole("button", { name: "Pause" }));
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(await screen.findByTestId("status")).toHaveTextContent("canceled");
  paused.resolve({ id: 12, status: "paused", direction: "upload", items: [] });

  await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("canceled"));
});

it("creates and refreshes an owned download batch", async () => {
  const user = userEvent.setup();
  apiPost.mockResolvedValue({ id: 22, status: "running", direction: "download", items: [] });
  apiGet.mockResolvedValue({ id: 22, status: "completed", direction: "download", items: [] });
  render(<DownloadBatchHarness />);

  await user.click(screen.getByRole("button", { name: "Start download" }));
  expect(apiPost).toHaveBeenCalledWith(
    "/api/file-transfers/download-batch",
    {
      runtime_id: 9,
      remote_paths: ["/var/log/app.log"],
      archive_name: "",
      idempotency_key: expect.stringMatching(/^[0-9a-f-]{36}$/),
    },
    { signal: expect.any(AbortSignal) },
  );
  expect(await screen.findByTestId("download-status")).toHaveTextContent("running");

  await user.click(screen.getByRole("button", { name: "Refresh download" }));
  expect(apiGet).toHaveBeenCalledWith("/api/file-transfer-batches/22", { signal: expect.any(AbortSignal) });
  expect(await screen.findByTestId("download-status")).toHaveTextContent("completed");
});
