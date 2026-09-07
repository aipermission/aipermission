import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
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
      <p>{notice?.message}</p>
      <p data-testid="uploads">{queues.uploadQueue.map((item) => item.name).join(",")}</p>
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
  expect(screen.getByTestId("uploads")).toHaveTextContent("a.txt,b.txt");
  await user.upload(input, first);
  expect(screen.getByTestId("uploads")).toHaveTextContent("a.txt,b.txt");
  await user.click(screen.getByRole("button", { name: "Move" }));
  expect(screen.getByTestId("uploads")).toHaveTextContent("b.txt,a.txt");
  await user.click(screen.getByRole("button", { name: "Remove" }));
  expect(screen.getByTestId("uploads")).toHaveTextContent("a.txt");

  const oversized = new File(["x"], "large.bin");
  Object.defineProperty(oversized, "size", { value: 512 * 1024 * 1024 + 1 });
  fireEvent.change(input, { target: { files: [oversized] } });
  expect(screen.getByText(/large\.bin exceeds/)).toBeVisible();
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
