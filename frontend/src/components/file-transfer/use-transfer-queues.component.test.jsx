import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import {
  fileTransferFailureText,
  formatBytes,
  formatETA,
  joinRemotePath,
  mergeUploadQueue,
  normalizeRemoteDirectoryInput,
  pendingBatchItemIDs,
  relocateUploadQueue,
  transferProgress,
} from "../../lib/file-transfer-utils";
import { useTransferQueues } from "./use-transfer-queues";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));

function QueueHarness({ recursive = true, runtimeTarget = { id: 7 } }) {
  const [notice, setNotice] = useState(null);
  const queues = useTransferQueues({
    runtimeTarget,
    defaultRemoteDir: "/tmp",
    recursive,
    joinRemotePath: (directory, name) => `${directory}/${name}`,
    onNotice: setNotice,
  });
  return (
    <div>
      <input aria-label="files" type="file" multiple onChange={queues.handleLocalFileChange} />
      <button type="button" onClick={() => queues.moveQueueItem(queues.uploadQueue[1]?.id, -1)}>
        Move
      </button>
      <button type="button" onClick={() => queues.removeQueueItem(queues.uploadQueue[0]?.id)}>
        Remove
      </button>
      <button type="button" onClick={() => void queues.addRemoteFiles([{ type: "directory", path: "/remote", name: "remote" }])}>
        Expand
      </button>
      <button type="button" onClick={() => queues.resetQueues()}>
        Reset
      </button>
      <button type="button" onClick={() => queues.clearQueue(queues.mode)}>
        Clear
      </button>
      <button type="button" onClick={() => queues.setMode("download")}>
        Download mode
      </button>
      <button type="button" onClick={() => queues.updateRemoteDirectory("/var/tmp")}>
        Relocate
      </button>
      <p>{notice?.message}</p>
      <p data-testid="mode">{queues.mode}</p>
      <p data-testid="remote-dir">{queues.remoteDir}</p>
      <p data-testid="uploads">{queues.uploadQueue.map((item) => item.name).join(",")}</p>
      <p data-testid="upload-paths">{queues.uploadQueue.map((item) => item.remote_path).join(",")}</p>
      <p data-testid="upload-count">{queues.uploadQueue.length}</p>
      <p data-testid="upload-sizes">{queues.uploadQueue.map((item) => item.size).join(",")}</p>
      <p data-testid="upload-markers">{queues.uploadQueue.map((item) => item.file.marker || "").join(",")}</p>
      <p data-testid="downloads">{queues.downloadQueue.map((item) => item.path).join(",")}</p>
    </div>
  );
}

beforeEach(() => apiPost.mockReset());

function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

it("owns upload deduplication, ordering, removal, and object limits", async () => {
  const user = userEvent.setup();
  render(<QueueHarness />);
  const input = screen.getByLabelText("files");
  const first = new File(["a"], "a.txt", { type: "text/plain", lastModified: 1 });
  const second = new File(["b"], "b.txt", { type: "text/plain", lastModified: 2 });

  await user.upload(input, [first, second]);
  expect(screen.getByTestId("uploads")).toHaveTextContent(/^a\.txt,b\.txt$/);
  expect(screen.getByTestId("upload-count")).toHaveTextContent("2");
  await user.upload(input, first);
  expect(screen.getByTestId("uploads")).toHaveTextContent(/^a\.txt,b\.txt$/);
  expect(screen.getByTestId("upload-count")).toHaveTextContent("2");
  await user.click(screen.getByRole("button", { name: "Move" }));
  expect(screen.getByTestId("uploads")).toHaveTextContent("b.txt,a.txt");
  await user.click(screen.getByRole("button", { name: "Remove" }));
  expect(screen.getByTestId("uploads")).toHaveTextContent("a.txt");

  const oversized = new File(["x"], "large.bin");
  Object.defineProperty(oversized, "size", { value: 512 * 1024 * 1024 + 1 });
  fireEvent.change(input, { target: { files: [oversized] } });
  expect(screen.getByText(/large\.bin exceeds/)).toBeVisible();
});

it("replaces destination conflicts while preserving distinct relative directories", async () => {
  const user = userEvent.setup();
  render(<QueueHarness />);
  const input = screen.getByLabelText("files");
  const original = new File(["a"], "same.txt", { type: "text/plain", lastModified: 1 });
  const replacement = new File(["replacement"], "same.txt", { type: "text/plain", lastModified: 2 });
  await user.upload(input, original);
  await user.upload(input, replacement);
  expect(screen.getByTestId("upload-count")).toHaveTextContent("1");
  expect(screen.getByTestId("upload-sizes")).toHaveTextContent(String(replacement.size));

  const nested = new File(["nested"], "same.txt", { type: "text/plain", lastModified: 3 });
  Object.defineProperty(nested, "webkitRelativePath", { value: "nested/same.txt" });
  await user.upload(input, nested);
  expect(screen.getByTestId("uploads")).toHaveTextContent(/^same\.txt,nested\/same\.txt$/);
  expect(screen.getByTestId("upload-count")).toHaveTextContent("2");
});

it("keeps the latest selected bytes when destination metadata is unchanged", async () => {
  const user = userEvent.setup();
  render(<QueueHarness />);
  const input = screen.getByLabelText("files");
  const original = new File(["old"], "same.txt", { type: "text/plain", lastModified: 1 });
  const replacement = new File(["new"], "same.txt", { type: "text/plain", lastModified: 1 });
  original.marker = "old";
  replacement.marker = "new";

  await user.upload(input, original);
  await user.upload(input, replacement);

  expect(screen.getByTestId("upload-count")).toHaveTextContent("1");
  expect(screen.getByTestId("upload-markers")).toHaveTextContent("new");
});

it("replaces a destination with the latest queue object", () => {
  const original = { id: "old", remote_path: "/tmp/artifact.bin", file: { version: "old" } };
  const replacement = { id: "new", remote_path: "/tmp/artifact.bin", file: { version: "new" } };

  expect(mergeUploadQueue([original], [replacement])).toEqual([replacement]);
  expect(mergeUploadQueue(undefined, [replacement])).toEqual([replacement]);
});

it("keeps transfer progress, failures, paths, and display values bounded", () => {
  expect(transferProgress({ size_bytes: 200, transferred_bytes: 50, status: "running" })).toEqual({
    percent: 25,
    label: "50 B / 200 B",
  });
  expect(transferProgress({ size_bytes: 200, transferred_bytes: 50, status: "canceled" }).percent).toBe(100);
  expect(fileTransferFailureText({ failure_kind: "outcome_unknown" })).toContain("may have completed");
  expect(fileTransferFailureText({ error: "remote unavailable" }, "fallback")).toBe("remote unavailable");
  expect(
    pendingBatchItemIDs({
      items: [
        { id: 1, status: "pending" },
        { id: 2, status: "completed" },
      ],
    }),
  ).toEqual([1]);

  expect(normalizeRemoteDirectoryInput("tmp/build/")).toBe("/tmp/build");
  expect(joinRemotePath("/", "/artifact.bin")).toBe("/artifact.bin");
  expect(relocateUploadQueue([{ name: "artifact.bin" }], "/tmp")).toEqual([{ name: "artifact.bin", remote_path: "/tmp/artifact.bin" }]);
  expect(formatBytes(1024)).toBe("1.00 KiB");
  expect(formatETA(65)).toBe("1m 5s");
  expect(formatETA(-1)).toBe("-");
});

it("expands recursive remote selections into a deduplicated download queue", async () => {
  const user = userEvent.setup();
  apiPost.mockResolvedValue({
    entries: [
      { type: "file", path: "/remote/a.txt", name: "a.txt", size: 1 },
      { type: "file", path: "/remote/b.txt", name: "b.txt", size: 2 },
    ],
  });
  render(<QueueHarness />);

  await user.click(screen.getByRole("button", { name: "Expand" }));
  await waitFor(() => expect(screen.getByTestId("downloads")).toHaveTextContent("/remote/a.txt,/remote/b.txt"));
  expect(apiPost).toHaveBeenCalledWith(
    "/api/file-transfers/expand",
    { runtime_id: 7, path: "/remote" },
    { signal: expect.any(AbortSignal) },
  );
});

it("cancels recursive expansion when its queue owner unmounts", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPost.mockReturnValueOnce(pending.promise);
  const view = render(<QueueHarness />);

  await user.click(screen.getByRole("button", { name: "Expand" }));
  const signal = apiPost.mock.calls[0][2].signal;
  view.unmount();
  expect(signal.aborted).toBe(true);
  pending.resolve({ entries: [{ type: "file", path: "/remote/a.txt", name: "a.txt", size: 1 }] });
  await Promise.resolve();
});

it("does not add an old expansion to a reset queue on the same target", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPost.mockReturnValueOnce(pending.promise);
  render(<QueueHarness />);

  await user.click(screen.getByRole("button", { name: "Expand" }));
  const signal = apiPost.mock.calls[0][2].signal;
  await user.click(screen.getByRole("button", { name: "Reset" }));
  expect(signal.aborted).toBe(true);
  pending.resolve({ entries: [{ type: "file", path: "/remote/old.txt", name: "old.txt", size: 1 }] });

  await waitFor(() => expect(screen.getByTestId("downloads")).toBeEmptyDOMElement());
});

it("relocates and clears upload queues before clearing download ownership", async () => {
  const user = userEvent.setup();
  render(<QueueHarness />);
  await user.upload(screen.getByLabelText("files"), new File(["a"], "a.txt"));

  await user.click(screen.getByRole("button", { name: "Relocate" }));
  expect(screen.getByTestId("remote-dir")).toHaveTextContent("/var/tmp");
  expect(screen.getByTestId("upload-paths")).toHaveTextContent("/var/tmp/a.txt");
  await user.click(screen.getByRole("button", { name: "Clear" }));
  expect(screen.getByTestId("uploads")).toBeEmptyDOMElement();
  expect(screen.getByTestId("remote-dir")).toHaveTextContent("/tmp");

  await user.click(screen.getByRole("button", { name: "Download mode" }));
  expect(screen.getByTestId("mode")).toHaveTextContent("download");
  await user.click(screen.getByRole("button", { name: "Clear" }));
  expect(screen.getByTestId("downloads")).toBeEmptyDOMElement();
});

it("reports a current recursive expansion failure", async () => {
  const user = userEvent.setup();
  apiPost.mockRejectedValueOnce(new Error("folder unavailable"));
  render(<QueueHarness />);

  await user.click(screen.getByRole("button", { name: "Expand" }));
  expect(await screen.findByText("folder unavailable")).toBeVisible();
  expect(screen.getByTestId("downloads")).toBeEmptyDOMElement();
});
