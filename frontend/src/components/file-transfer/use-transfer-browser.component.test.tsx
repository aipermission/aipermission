import { act, render, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { useTransferBrowser } from "./use-transfer-browser";
import { remoteBrowserResponse } from "./transfer-contracts";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));

function deferred() {
  let resolve: (_value: unknown) => void = () => {
    throw new Error("Deferred request is not initialized");
  };
  const promise = new Promise<unknown>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

function BrowserHarness({ targetID = 7 }: { targetID?: number }) {
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

beforeEach(() => {
  vi.mocked(apiPost).mockReset();
});

function requestSignal(index = 0) {
  const options: unknown = vi.mocked(apiPost).mock.calls[index]?.[2];
  if (!options || typeof options !== "object" || !("signal" in options) || !(options.signal instanceof AbortSignal))
    throw new Error("Missing browser request signal");
  return options.signal;
}

it("rejects an older browse response after a newer path wins", async () => {
  const user = userEvent.setup();
  const older = deferred();
  vi.mocked(apiPost)
    .mockReturnValueOnce(older.promise)
    .mockResolvedValueOnce({ path: "/new", entries: [{ name: "new.txt", type: "file", path: "/new/new.txt" }] });
  render(<BrowserHarness />);

  await user.click(screen.getByRole("button", { name: "Open" }));
  const oldSignal = requestSignal();
  await user.click(screen.getByRole("button", { name: "Load new" }));
  expect(oldSignal.aborted).toBe(true);
  expect(await screen.findByTestId("entries")).toHaveTextContent("new.txt");

  older.resolve({ path: "/upload", entries: [{ name: "old.txt", type: "file", path: "/upload/old.txt" }] });
  await waitFor(() => expect(screen.getByTestId("entries")).not.toHaveTextContent("old.txt"));
});

it("aborts an in-flight browse request when its owner unmounts", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  vi.mocked(apiPost).mockReturnValueOnce(pending.promise);
  const view = render(<BrowserHarness />);

  await user.click(screen.getByRole("button", { name: "Open" }));
  const signal = requestSignal();
  view.unmount();
  expect(signal.aborted).toBe(true);
  pending.resolve({ path: "/upload", entries: [] });
  await Promise.resolve();
});

it("appends the next page without losing existing entries or opaque cursors", async () => {
  vi.mocked(apiPost)
    .mockResolvedValueOnce({
      path: "/home",
      entries: [{ type: "file", name: "one", path: "/home/one" }],
      next_cursor: "opaque/page+1",
      has_more: true,
    })
    .mockResolvedValueOnce({ path: "/home", entries: [{ type: "file", name: "two", path: "/home/two" }], has_more: false });
  const { result } = renderHook(() =>
    useTransferBrowser({
      runtimeTarget: { id: 7 },
      defaultRemoteDir: "/home",
      remoteDir: "/home",
      normalizeRemoteDirectoryInput: (path) => path,
      onUseDirectory: vi.fn(),
    }),
  );
  await act(async () => result.current.loadBrowser("/home", "download"));
  await act(async () => result.current.loadBrowser("/home", "download", { append: true, cursor: "opaque/page+1" }));
  expect(result.current.browser.data?.entries.map((entry) => entry.name)).toEqual(["one", "two"]);
  expect(apiPost).toHaveBeenLastCalledWith(
    "/api/file-transfers/browse",
    { runtime_id: 7, path: "/home", cursor: "opaque/page+1" },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});

it("discards a pending browse after the runtime changes", async () => {
  const pending = deferred();
  vi.mocked(apiPost).mockReturnValueOnce(pending.promise);
  const { result, rerender } = renderHook(
    (id) =>
      useTransferBrowser({
        runtimeTarget: { id },
        defaultRemoteDir: "/home",
        remoteDir: "/home",
        normalizeRemoteDirectoryInput: (path) => path,
        onUseDirectory: vi.fn(),
      }),
    { initialProps: 7 },
  );
  let loading: Promise<void> | undefined;
  act(() => {
    loading = result.current.loadBrowser("/home", "upload");
  });
  rerender(8);
  expect(requestSignal().aborted).toBe(true);
  await act(async () => {
    pending.resolve({ path: "/home", entries: [{ type: "file", name: "old", path: "/home/old" }] });
    await loading;
  });
  expect(result.current.browser.data).toBeNull();
});

it.each([
  { entries: [{ type: "file", name: "bad", path: "/bad", size: -1 }] },
  { entries: [], path: 7 },
  { entries: [], parent: false },
  { entries: [], next_cursor: {} },
  { entries: [], has_more: "true" },
  { entries: [{ type: "file", path: "/bad", name: "bad", modified_at: {} }] },
])("rejects malformed browser metadata safely", (response) => {
  expect(() => remoteBrowserResponse(response)).toThrow(/Invalid remote/);
});

it("retains non-selectable remote entries in the browser response", () => {
  const entries = [{ type: "other", path: "/home/link", name: "link", size: 7, modified_at: "2026-09-26T00:00:00Z" }];
  expect(remoteBrowserResponse({ entries }).entries).toEqual(entries);
});

it("does not reuse a prior folder cursor during navigation", async () => {
  const pending = deferred();
  vi.mocked(apiPost)
    .mockResolvedValueOnce({
      path: "/a",
      entries: [{ type: "file", name: "one", path: "/a/one" }],
      has_more: true,
      next_cursor: "cursor-a",
    })
    .mockReturnValueOnce(pending.promise);
  const { result } = renderHook(() =>
    useTransferBrowser({
      runtimeTarget: { id: 7 },
      defaultRemoteDir: "/home",
      remoteDir: "/a",
      normalizeRemoteDirectoryInput: (path) => path,
      onUseDirectory: vi.fn(),
    }),
  );
  await act(async () => result.current.loadBrowser("/a", "upload"));
  let navigation: Promise<void> | undefined;
  act(() => {
    navigation = result.current.loadBrowser("/b", "upload");
  });
  await act(async () => result.current.loadBrowser("/b", "upload", { append: true, cursor: "cursor-a" }));
  expect(apiPost).toHaveBeenCalledTimes(2);
  expect(requestSignal(1).aborted).toBe(false);
  await act(async () => {
    pending.resolve({ path: "/b", entries: [] });
    await navigation;
  });
  expect(result.current.browser.data?.entries).toEqual([]);
  expect(result.current.browser.path).toBe("/b");
});

it("retains the loaded page and allows retry after an append failure", async () => {
  const pending = deferred();
  vi.mocked(apiPost)
    .mockResolvedValueOnce({
      path: "/a",
      entries: [{ type: "file", name: "one", path: "/a/one" }],
      has_more: true,
      next_cursor: "cursor-a",
    })
    .mockReturnValueOnce(pending.promise)
    .mockResolvedValueOnce({ path: "/a", entries: [{ type: "file", name: "two", path: "/a/two" }], has_more: false });
  const { result } = renderHook(() =>
    useTransferBrowser({
      runtimeTarget: { id: 7 },
      defaultRemoteDir: "/home",
      remoteDir: "/a",
      normalizeRemoteDirectoryInput: (path) => path,
      onUseDirectory: vi.fn(),
    }),
  );
  await act(async () => result.current.loadBrowser("/a", "upload"));
  // A malformed page must be retryable just like a transport failure.
  let append: Promise<void> | undefined;
  act(() => {
    append = result.current.loadBrowser("/a", "upload", { append: true, cursor: "cursor-a" });
  });
  await act(async () => {
    pending.resolve({ entries: null });
    await append;
  });
  expect(result.current.browser.state).toBe("ready");
  expect(result.current.browser.error).toMatch(/Invalid remote/);
  expect(result.current.browser.data?.entries.map((entry) => entry.name)).toEqual(["one"]);
  await act(async () => result.current.loadBrowser("/a", "upload", { append: true, cursor: "cursor-a" }));
  expect(result.current.browser.data?.entries.map((entry) => entry.name)).toEqual(["one", "two"]);
  expect(result.current.browser.error).toBeNull();
});
