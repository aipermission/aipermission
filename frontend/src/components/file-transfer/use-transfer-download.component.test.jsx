import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDownload } from "../../lib/api";
import { useTransferDownload } from "./use-transfer-download";

vi.mock("../../lib/api", () => ({ apiDownload: vi.fn() }));

const completedBatch = {
  state: "ready",
  item: { id: 12, status: "completed", direction: "download", archive_name: "reports.zip" },
  error: null,
};

function DownloadHarness({ onNotice = vi.fn(), onClose = vi.fn(), clearBatch = vi.fn(), clearQueue = vi.fn() }) {
  const [batch, setBatch] = useState(completedBatch);
  const download = useTransferDownload({ batch, setBatch, mode: "download", clearBatch, clearQueue, onNotice, onClose });
  return (
    <div>
      <button type="button" onClick={() => void download.saveDownloadBatch()}>
        Save
      </button>
      <button type="button" onClick={download.clearFinishedQueue}>
        Clear
      </button>
      <button type="button" onClick={download.requestClose}>
        Close
      </button>
      <button type="button" onClick={() => download.clearFinishedQueue({ force: true })}>
        Clear anyway
      </button>
      <button type="button" onClick={() => void download.saveThenClear()}>
        Save then clear
      </button>
      <button type="button" onClick={download.closeWithoutSaving}>
        Close anyway
      </button>
      <p data-testid="batch-state">{batch.state}</p>
      <p data-testid="clear-prompt">{String(download.clearDownloadPrompt)}</p>
      <p data-testid="close-prompt">{String(download.closeDownloadPrompt)}</p>
    </div>
  );
}

beforeEach(() => {
  apiDownload.mockReset();
});

it("owns completed download notices and allows retry after picker cancellation", async () => {
  const user = userEvent.setup();
  const onNotice = vi.fn();
  apiDownload.mockResolvedValueOnce({ canceled: true }).mockResolvedValueOnce({ saved: true });
  render(<DownloadHarness onNotice={onNotice} />);

  await waitFor(() =>
    expect(onNotice).toHaveBeenCalledWith({
      tone: "good",
      message: "Download queue completed. Click Save download to choose where to save it.",
    }),
  );
  await user.click(screen.getByRole("button", { name: "Save" }));
  expect(apiDownload).toHaveBeenLastCalledWith("/api/file-transfer-batches/12/download", "reports.zip", {
    picker: true,
    signal: expect.any(AbortSignal),
  });
  expect(onNotice).toHaveBeenLastCalledWith({
    tone: "warn",
    message: "Download was not saved. You can try Save download again.",
  });
  expect(screen.getByTestId("batch-state")).toHaveTextContent("ready");

  await user.click(screen.getByRole("button", { name: "Save" }));
  expect(apiDownload).toHaveBeenCalledTimes(2);
  expect(onNotice).toHaveBeenLastCalledWith({
    tone: "good",
    message: "Download saved. Review the summary, then clear when ready.",
  });
});

it("requires explicit confirmation before clearing or closing an unsaved download", async () => {
  const user = userEvent.setup();
  const onClose = vi.fn();
  const clearBatch = vi.fn();
  const clearQueue = vi.fn();
  render(<DownloadHarness onClose={onClose} clearBatch={clearBatch} clearQueue={clearQueue} />);

  await user.click(screen.getByRole("button", { name: "Clear" }));
  expect(screen.getByTestId("clear-prompt")).toHaveTextContent("true");
  expect(clearBatch).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Close" }));
  expect(screen.getByTestId("close-prompt")).toHaveTextContent("true");
  expect(onClose).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: "Clear anyway" }));
  expect(clearQueue).toHaveBeenCalledWith("download");
  expect(clearBatch).toHaveBeenCalledOnce();
  await user.click(screen.getByRole("button", { name: "Close anyway" }));
  expect(onClose).toHaveBeenCalledOnce();
});

it("saves before clearing and aborts an unfinished save when unmounted", async () => {
  const user = userEvent.setup();
  const clearBatch = vi.fn();
  const clearQueue = vi.fn();
  apiDownload.mockResolvedValueOnce({ saved: true });
  const view = render(<DownloadHarness clearBatch={clearBatch} clearQueue={clearQueue} />);

  await user.click(screen.getByRole("button", { name: "Save then clear" }));
  expect(clearQueue).toHaveBeenCalledWith("download");
  expect(clearBatch).toHaveBeenCalledOnce();

  let resolveDownload;
  apiDownload.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveDownload = resolve;
      }),
  );
  await user.click(screen.getByRole("button", { name: "Save" }));
  const signal = apiDownload.mock.calls.at(-1)[2].signal;
  view.unmount();
  expect(signal.aborted).toBe(true);
  resolveDownload({ saved: true });
});
