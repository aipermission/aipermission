import { act, render, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost, apiPostForm } from "../../lib/api";
import { useTransferBatch } from "./use-transfer-batch";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPostForm: vi.fn() }));

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_reason: unknown) => void;
  const promise = new Promise<unknown>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

type HarnessProps = Partial<Pick<Parameters<typeof useTransferBatch>[0], "onNotice" | "onUploadCompleted">>;

function BatchHarness({ onNotice = vi.fn(), onUploadCompleted = vi.fn() }: HarnessProps) {
  const file = new File(["payload"], "a.txt", { type: "text/plain" });
  const transfer = useTransferBatch({
    open: true,
    runtimeTarget: { id: 7 },
    mode: "upload",
    remoteDir: "/tmp",
    uploadQueue: [{ id: "a", name: "a.txt", relative_path: "a.txt", file }],
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
      <button type="button" onClick={() => transfer.clearBatch()}>
        Clear
      </button>
      <button type="button" onClick={() => void transfer.refreshBatch()}>
        Refresh
      </button>
      <p data-testid="status">{transfer.batch.item?.status || transfer.batch.state}</p>
      <p data-testid="conflicts">{transfer.overwritePrompt?.length || 0}</p>
    </div>
  );
}

function DownloadBatchHarness({ onNotice = vi.fn() }: Pick<HarnessProps, "onNotice">) {
  const transfer = useTransferBatch({
    open: true,
    runtimeTarget: { id: 9 },
    mode: "download",
    remoteDir: "/tmp",
    uploadQueue: [],
    downloadQueue: [{ path: "/var/log/app.log" }],
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
  vi.mocked(apiGet).mockReset();
  vi.mocked(apiPost).mockReset();
  vi.mocked(apiPostForm).mockReset();
});

it("owns paused queue edits and preserves batch identity on failure", async () => {
  const { result } = renderHook(() =>
    useTransferBatch({
      open: true,
      runtimeTarget: { id: 7 },
      mode: "download",
      remoteDir: "/tmp",
      uploadQueue: [],
      downloadQueue: [],
      queue: [],
      onNotice: vi.fn(),
    }),
  );
  await act(async () => {
    await result.current.refreshBatch();
    await result.current.updatePausedBatchQueue([]);
    await result.current.cancelBatch();
    await result.current.startQueue();
  });
  expect(apiPost).not.toHaveBeenCalled();
  const item = {
    id: 12,
    status: "paused",
    direction: "download",
    items: [
      { id: 1, status: "pending" },
      { id: 2, status: "completed" },
      { id: 3, status: "pending" },
    ],
  };
  act(() => result.current.setBatch({ state: "ready", item, error: null }));
  expect(result.current.pausedQueueWithout("1")).toEqual([3]);
  expect(result.current.movePausedQueueItem(3, -1)).toEqual([3, 1]);
  expect(result.current.movePausedQueueItem(1, -1)).toBeNull();
  expect(result.current.movePausedQueueItem(3, 1)).toBeNull();
  expect(result.current.movePausedQueueItem(99, 1)).toBeNull();
  apiPost.mockResolvedValueOnce({ ...item, items: [{ id: 3, status: "pending" }] });
  await act(async () => result.current.updatePausedBatchQueue([3]));
  expect(apiPost).toHaveBeenLastCalledWith("/api/file-transfer-batches/12/queue", { item_ids: [3] }, { signal: expect.any(AbortSignal) });
  expect(result.current.batch.item.items).toEqual([{ id: 3, status: "pending" }]);
  apiPost.mockRejectedValueOnce(new Error("queue unavailable"));
  await act(async () => result.current.updatePausedBatchQueue([3]));
  expect(result.current.batch).toMatchObject({ state: "error", item: { id: 12 }, error: "queue unavailable" });
});

it("owns upload creation and ordered pause, resume, and cancel transitions", async () => {
  const user = userEvent.setup();
  const onNotice = vi.fn();
  vi.mocked(apiPostForm).mockResolvedValue({ id: 12, status: "running", direction: "upload", items: [] });
  vi.mocked(apiPost).mockImplementation((path) => {
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
  const form = vi.mocked(apiPostForm).mock.calls[0][1];
  expect(form.get("runtime_id")).toBe("7");
  expect(form.get("remote_dir")).toBe("/tmp");
  expect(JSON.parse(String(form.get("relative_paths")))).toEqual(["a.txt"]);
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
  vi.mocked(apiPostForm).mockResolvedValue({ id: 12, status: "completed", direction: "upload", items: [] });
  render(<BatchHarness onUploadCompleted={onUploadCompleted} />);

  await user.click(screen.getByRole("button", { name: "Start" }));
  await waitFor(() => expect(onUploadCompleted).toHaveBeenCalledOnce());
});

it("owns upload overwrite conflicts without creating a batch", async () => {
  const user = userEvent.setup();
  vi.mocked(apiPostForm).mockRejectedValue(
    Object.assign(new Error("conflict"), { status: 409, data: { code: "remote_files_exist", conflicts: [{ remote_path: "/tmp/a.txt" }] } }),
  );
  render(<BatchHarness />);

  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(await screen.findByTestId("conflicts")).toHaveTextContent("1");
  expect(screen.getByTestId("status")).toHaveTextContent("idle");
});

it("does not accept a malformed batch creation response as a started transfer", async () => {
  const user = userEvent.setup();
  vi.mocked(apiPostForm).mockResolvedValue({ id: 12, status: "running", items: [] });
  render(<BatchHarness />);

  await user.click(screen.getByRole("button", { name: "Start" }));

  expect(await screen.findByTestId("status")).toHaveTextContent("error");
});

it("reuses the upload idempotency key when a response is lost", async () => {
  const user = userEvent.setup();
  vi.mocked(apiPostForm).mockRejectedValueOnce(new Error("network response lost")).mockResolvedValueOnce({
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
  expect(vi.mocked(apiPostForm).mock.calls[1][1].get("idempotency_key")).toBe(
    vi.mocked(apiPostForm).mock.calls[0][1].get("idempotency_key"),
  );
});

it("ignores upload completion after the dialog batch is reset", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  vi.mocked(apiPostForm).mockReturnValue(pending.promise);
  render(<BatchHarness />);

  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(screen.getByTestId("status")).toHaveTextContent("starting");
  await user.click(screen.getByRole("button", { name: "Reset" }));
  pending.resolve({ id: 12, status: "running", direction: "upload", items: [] });

  await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("idle"));
});

it.each([
  ["success", (pending: ReturnType<typeof deferred>) => pending.resolve({ id: 12, status: "completed", direction: "upload", items: [] })],
  ["failure", (pending: ReturnType<typeof deferred>) => pending.reject(new Error("refresh failed"))],
])("does not resurrect a cleared batch after late refresh %s", async (_outcome, settle) => {
  const user = userEvent.setup();
  const pending = deferred();
  vi.mocked(apiPostForm).mockResolvedValue({ id: 12, status: "completed", direction: "upload", items: [] });
  vi.mocked(apiGet).mockReturnValue(pending.promise);
  render(<BatchHarness />);

  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(await screen.findByTestId("status")).toHaveTextContent("completed");
  await user.click(screen.getByRole("button", { name: "Refresh" }));
  expect(screen.getByTestId("status")).toHaveTextContent("completed");
  await user.click(screen.getByRole("button", { name: "Clear" }));
  settle(pending);

  await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("idle"));
});

it("keeps the latest transition when an older transition completes last", async () => {
  const user = userEvent.setup();
  const paused = deferred();
  vi.mocked(apiPostForm).mockResolvedValue({ id: 12, status: "running", direction: "upload", items: [] });
  vi.mocked(apiPost)
    .mockReturnValueOnce(paused.promise)
    .mockResolvedValueOnce({ id: 12, status: "canceled", direction: "upload", items: [] });
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
  vi.mocked(apiPost).mockResolvedValue({ id: 22, status: "running", direction: "download", items: [] });
  vi.mocked(apiGet).mockResolvedValue({ id: 22, status: "completed", direction: "download", items: [] });
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

function batchOptions(overrides: Partial<Parameters<typeof useTransferBatch>[0]> = {}): Parameters<typeof useTransferBatch>[0] {
  const file = new File(["payload"], "a.txt");
  return {
    open: true,
    runtimeTarget: { id: 7 },
    mode: "upload",
    remoteDir: "/tmp",
    uploadQueue: [{ id: "a", name: "a.txt", file }],
    downloadQueue: [],
    queue: [{ id: "a" }],
    onNotice: vi.fn(),
    ...overrides,
  };
}

it("updates a paused queue using only pending item identities", async () => {
  const paused = {
    id: 12,
    status: "paused" as const,
    direction: "upload" as const,
    items: [
      { id: 1, status: "pending" as const },
      { id: 2, status: "pending" as const },
      { id: 3, status: "completed" as const },
    ],
  };
  const reordered = { ...paused, items: [paused.items[1], paused.items[0], paused.items[2]] };
  vi.mocked(apiPost).mockResolvedValue(reordered);
  const { result } = renderHook(() => useTransferBatch(batchOptions()));
  act(() => result.current.setBatch({ state: "ready", item: paused, error: null }));
  expect(result.current.pausedQueueWithout(1)).toEqual([2]);
  expect(result.current.movePausedQueueItem(1, 1)).toEqual([2, 1]);
  expect(result.current.movePausedQueueItem(1, -1)).toBeNull();
  expect(result.current.movePausedQueueItem(3, -1)).toBeNull();
  await act(async () => result.current.updatePausedBatchQueue([2, 1]));
  expect(apiPost).toHaveBeenCalledWith("/api/file-transfer-batches/12/queue", { item_ids: [2, 1] }, { signal: expect.any(AbortSignal) });
  expect(result.current.batch.state).toBe("ready");
  expect(result.current.batch.item).toEqual(reordered);
});

it("reports a current paused-queue failure without discarding the batch", async () => {
  vi.mocked(apiPost).mockRejectedValue(new Error("Queue unavailable"));
  const { result } = renderHook(() => useTransferBatch(batchOptions()));
  act(() => result.current.setBatch({ state: "ready", item: { id: 12, status: "paused", direction: "upload" }, error: null }));
  await act(async () => result.current.updatePausedBatchQueue([1]));
  expect(result.current.batch).toMatchObject({ state: "error", item: { id: 12 }, error: "Queue unavailable" });
});

it.each(["success", "failure"])("does not revive a reset batch after a delayed queue update %s", async (outcome) => {
  const pending = deferred();
  vi.mocked(apiPost).mockReturnValue(pending.promise);
  const { result } = renderHook(() => useTransferBatch(batchOptions()));
  act(() => result.current.setBatch({ state: "ready", item: { id: 12, status: "paused", direction: "upload" }, error: null }));
  let update!: Promise<void>;
  act(() => {
    update = result.current.updatePausedBatchQueue([1]);
  });
  const signal = vi.mocked(apiPost).mock.calls[0][2]?.signal;
  act(() => result.current.clearBatch());
  expect(signal?.aborted).toBe(true);
  await act(async () => {
    if (outcome === "success") pending.resolve({ id: 12, status: "paused", direction: "upload" });
    else pending.reject(new Error("Late queue failure"));
    await update;
  });
  expect(result.current.batch).toEqual({ state: "idle", item: null, error: null });
});

it.each([
  { label: "runtime", options: { runtimeTarget: null } },
  { label: "queue", options: { queue: [] } },
])("does not send starts or transitions without a $label or existing batch", async ({ options }) => {
  const { result } = renderHook(() => useTransferBatch(batchOptions(options)));
  await act(async () => {
    await result.current.startQueue();
    await result.current.pauseBatch();
    await result.current.refreshBatch();
    await result.current.updatePausedBatchQueue([]);
  });
  expect(apiPost).not.toHaveBeenCalled();
  expect(apiGet).not.toHaveBeenCalled();
  expect(apiPostForm).not.toHaveBeenCalled();
});
