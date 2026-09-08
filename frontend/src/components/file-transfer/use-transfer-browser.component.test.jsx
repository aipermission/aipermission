import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { useTransferBrowser } from "./use-transfer-browser";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));

function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

function BrowserHarness({ targetID = 7 }) {
  const browser = useTransferBrowser({
    runtimeTarget: { id: targetID },
    defaultRemoteDir: "/home",
    remoteDir: "/upload",
    normalizeRemoteDirectoryInput: (path) => path || "/",
    onUseDirectory: vi.fn(),
  });
  return (
    <div>
      <button type="button" onClick={() => browser.openBrowser("upload")}>
        Open
      </button>
      <button type="button" onClick={() => void browser.loadBrowser("/new", "upload")}>
        Load new
      </button>
      <p data-testid="path">{browser.browser.path}</p>
      <p data-testid="entries">{browser.browser.data?.entries?.map((entry) => entry.name).join(",")}</p>
    </div>
  );
}

beforeEach(() => apiPost.mockReset());

it("rejects an older browse response after a newer path wins", async () => {
  const user = userEvent.setup();
  const older = deferred();
  apiPost.mockReturnValueOnce(older.promise).mockResolvedValueOnce({ path: "/new", entries: [{ name: "new.txt" }] });
  render(<BrowserHarness />);

  await user.click(screen.getByRole("button", { name: "Open" }));
  const oldSignal = apiPost.mock.calls[0][2].signal;
  await user.click(screen.getByRole("button", { name: "Load new" }));
  expect(oldSignal.aborted).toBe(true);
  expect(await screen.findByTestId("entries")).toHaveTextContent("new.txt");

  older.resolve({ path: "/upload", entries: [{ name: "old.txt" }] });
  await waitFor(() => expect(screen.getByTestId("entries")).not.toHaveTextContent("old.txt"));
});

it("aborts an in-flight browse request when its owner unmounts", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPost.mockReturnValueOnce(pending.promise);
  const view = render(<BrowserHarness />);

  await user.click(screen.getByRole("button", { name: "Open" }));
  const signal = apiPost.mock.calls[0][2].signal;
  view.unmount();
  expect(signal.aborted).toBe(true);
  pending.resolve({ path: "/upload", entries: [] });
  await Promise.resolve();
});
