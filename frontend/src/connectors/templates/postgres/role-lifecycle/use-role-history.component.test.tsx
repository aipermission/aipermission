import { act, renderHook, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost, currentWorkspaceBinding } from "../../../../lib/api";
import {
  roleHistoryPageFixture,
  roleHistoryCursorPageFixture,
  deferredRoleHistoryReply as deferred,
} from "../../../../test/postgres/role-history-fixtures.test";
import { useRoleHistory } from "./use-role-history";

vi.mock("../../../../lib/api", () => ({ apiPost: vi.fn(), currentWorkspaceBinding: vi.fn() }));
const post = vi.mocked(apiPost);
const binding = vi.mocked(currentWorkspaceBinding);

function page(start = 1, count = 1, hasMore = false, targetID = 1) {
  return roleHistoryCursorPageFixture(targetID, start, count, hasMore);
}

beforeEach(() => {
  post.mockReset().mockResolvedValue(page());
  binding.mockReset().mockReturnValue("workspace-a");
});

it("loads only the read-only status operation with a cleanup signal", async () => {
  const { result, unmount } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.state).toBe("ready"));
  expect(post).toHaveBeenCalledExactlyOnceWith(
    "/api/connector-targets/1/operations/role-lifecycle-status",
    {},
    { signal: expect.any(AbortSignal), workspaceBinding: "workspace-a" },
  );
  const signal = post.mock.calls[0][2]!.signal!;
  unmount();
  expect(signal.aborted).toBe(false); // Completed requests are released, not aborted on disposal.
});

it.each(["invalid", "transport"])("releases the guard after a %s failure", async (kind) => {
  if (kind === "invalid") post.mockResolvedValueOnce({});
  else post.mockRejectedValueOnce(new Error("transport failed"));
  const { result, unmount } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.state).toBe("error"));
  const signal = post.mock.calls[0][2]!.signal!;
  unmount();
  expect(signal.aborted).toBe(false);
});

it("survives StrictMode cleanup without accepting the first effect's late response", async () => {
  const pending = deferred();
  post.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(page(2));
  const { result } = renderHook(() => useRoleHistory(1), { wrapper: StrictMode });
  await waitFor(() => expect(result.current.state).toBe("ready"));
  expect(post.mock.calls[0][2]!.signal!.aborted).toBe(true);
  await act(async () => {
    pending.resolve(page());
    await pending.promise;
  });
  expect(result.current.page?.entries[0].resource_id).toBe("2");
});

it("drops the old snapshot during refresh and rejects a superseded late response", async () => {
  const { result } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.state).toBe("ready"));
  const old = deferred();
  post.mockReturnValueOnce(old.promise).mockResolvedValueOnce(page(2));
  let pending!: Promise<void>;
  act(() => {
    pending = result.current.refresh();
  });
  expect(result.current.state).toBe("loading");
  expect(result.current.page).toBeNull();
  const signal = post.mock.calls[1][2]!.signal!;
  await act(async () => {
    await result.current.refresh();
  });
  expect(signal.aborted).toBe(true);
  await act(async () => {
    old.resolve(page());
    await pending;
  });
  expect(result.current.page?.entries[0].resource_id).toBe("2");
});

it("rejects a retained Next callback before it can acquire ownership during refresh", async () => {
  post.mockResolvedValueOnce(page(1, 64, true));
  const { result } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.canNext).toBe(true));
  const oldNext = result.current.next;
  const pending = deferred();
  post.mockReturnValueOnce(pending.promise);
  let refresh!: Promise<void>;
  act(() => {
    refresh = result.current.refresh();
  });
  const signal = post.mock.calls[1][2]!.signal!;
  await act(async () => {
    await oldNext();
  });
  expect(post).toHaveBeenCalledTimes(2);
  expect(signal.aborted).toBe(false);
  expect(result.current.state).toBe("loading");
  await act(async () => {
    pending.resolve(page(3));
    await refresh;
  });
  expect(result.current.page?.entries[0].resource_id).toBe("3");
});

it("does not revive page-two callbacks after leaving and returning to the same target", async () => {
  post.mockResolvedValueOnce(page(1, 64, true)).mockResolvedValueOnce(page(65));
  const { result, rerender } = renderHook((id) => useRoleHistory(id), { initialProps: 1 });
  await waitFor(() => expect(result.current.canNext).toBe(true));
  await act(async () => {
    await result.current.next();
  });
  const oldRefresh = result.current.refresh;
  post.mockResolvedValueOnce(page(10, 1, false, 2)).mockResolvedValueOnce(page());
  rerender(2);
  await waitFor(() => expect(result.current.page?.target_id).toBe(2));
  rerender(1);
  await waitFor(() => expect(result.current.page?.target_id).toBe(1));
  expect(result.current.pageNumber).toBe(1);
  await act(async () => {
    await oldRefresh();
  });
  expect(post).toHaveBeenCalledTimes(4);
  expect(result.current.state).toBe("ready");
  expect(result.current.pageNumber).toBe(1);
  expect(result.current.page?.entries[0].resource_id).toBe("1");
});

it.each(["resolve", "reject"] as const)("aborts on unmount and ignores late %s", async (settle) => {
  const pending = deferred();
  post.mockReturnValueOnce(pending.promise);
  const { result, unmount } = renderHook(() => useRoleHistory(1));
  const refresh = result.current.refresh;
  const signal = post.mock.calls[0][2]!.signal!;
  unmount();
  expect(signal.aborted).toBe(true);
  await act(async () => {
    if (settle === "resolve") pending.resolve(page());
    else pending.reject(new Error("late failure"));
    await pending.promise.catch(() => {});
    await refresh();
  });
  expect(post).toHaveBeenCalledTimes(1);
});

it("resets pages for a target swap and ignores the old target response and callback", async () => {
  const pending = deferred();
  post.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(page(10, 1, false, 2));
  const { result, rerender } = renderHook((id) => useRoleHistory(id), { initialProps: 1 });
  const oldRefresh = result.current.refresh;
  const signal = post.mock.calls[0][2]!.signal!;
  rerender(2);
  expect(result.current.page).toBeNull();
  expect(signal.aborted).toBe(true);
  await waitFor(() => expect(result.current.state).toBe("ready"));
  await act(async () => {
    pending.resolve(page());
    await pending.promise;
    await oldRefresh();
  });
  expect(result.current.page?.target_id).toBe(2);
  expect(result.current.pageNumber).toBe(1);
  expect(post).toHaveBeenCalledTimes(2);
});

it.each([false, true])("clears and freezes on workspace drift (rerender: %s)", async (rerenderNow) => {
  const pending = deferred();
  post.mockReturnValueOnce(pending.promise);
  const { result, rerender } = renderHook(() => useRoleHistory(1));
  binding.mockReturnValue("workspace-b");
  if (rerenderNow) rerender();
  await act(async () => {
    pending.resolve(page());
    await pending.promise;
  });
  expect(result.current.state).toBe("error");
  expect(result.current.error).toMatch(/^Workspace changed/);
  expect(result.current.page).toBeNull();
  expect(post.mock.calls[0][2]!.signal!.aborted).toBe(true);
  binding.mockReturnValue("workspace-a");
  await act(async () => {
    await result.current.refresh();
  });
  expect(result.current.workspaceChanged).toBe(true);
  expect(post).toHaveBeenCalledTimes(1);
});

it("detects workspace drift after completion without another response or render", async () => {
  const { result } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.state).toBe("ready"));
  binding.mockReturnValue("workspace-b");
  await waitFor(() => expect(result.current.workspaceChanged).toBe(true));
  expect(result.current.page).toBeNull();
  expect(post).toHaveBeenCalledTimes(1);
});

it.each([
  null,
  {},
  { ...page(), target_id: 2 },
  { ...page(), entries: [{ ...page().entries[0], resource_id: "01" }] },
  { ...page(), entries: [{ ...page().entries[0], resource_id: 1 }] },
  { ...page(), entries: page(2, 2).entries.reverse() },
  { ...page(), has_more: true, next_after_resource_id: "1" },
])("fails closed for an invalid page: %j", async (raw) => {
  post.mockResolvedValueOnce(raw);
  const { result } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.state).toBe("error"));
  expect(result.current.page).toBeNull();
  expect(result.current.error).toBe("Unable to load Postgres role history.");
});

it("pages in canonical order, replaces entries, and refreshes the current page", async () => {
  post.mockResolvedValueOnce(page(1, 64, true)).mockResolvedValueOnce(page(65));
  const { result } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.canNext).toBe(true));
  await act(async () => {
    await result.current.next();
  });
  expect(post.mock.calls[1][1]).toEqual({ after_resource_id: "64" });
  expect(result.current.page?.entries.map((entry) => entry.resource_id)).toEqual(["65"]);
  expect(result.current.pageNumber).toBe(2);
  expect(result.current.canNext).toBe(false);
  post.mockResolvedValueOnce(page(66)).mockResolvedValueOnce(page(1, 64, true));
  await act(async () => {
    await result.current.refresh();
  });
  expect(post.mock.calls[2][1]).toEqual({ after_resource_id: "64" });
  await act(async () => {
    await result.current.previous();
  });
  expect(post.mock.calls[3][1]).toEqual({});
  expect(result.current.pageNumber).toBe(1);
  expect(result.current.canPrevious).toBe(false);
});

it("rejects a next page that regresses behind the requested cursor", async () => {
  post.mockResolvedValueOnce(page(1, 64, true)).mockResolvedValueOnce(page(64));
  const { result } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.canNext).toBe(true));
  await act(async () => {
    await result.current.next();
  });
  expect(result.current.state).toBe("error");
  expect(result.current.page).toBeNull();
});

it("keeps resource cursors larger than Number.MAX_SAFE_INTEGER as exact decimal strings", async () => {
  const ids = Array.from({ length: 64 }, (_, index) => String(9007199254740993n + BigInt(index)));
  const first = roleHistoryPageFixture(1, ids);
  const cursor = ids.at(-1)!;
  post
    .mockResolvedValueOnce({ ...first, has_more: true, next_after_resource_id: cursor })
    .mockResolvedValueOnce(roleHistoryPageFixture(1, [String(BigInt(cursor) + 1n)]));
  const { result } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.canNext).toBe(true));
  await act(async () => {
    await result.current.next();
  });
  expect(post.mock.calls[1][1]).toEqual({ after_resource_id: "9007199254741056" });
  expect(result.current.page?.entries[0].resource_id).toBe("9007199254741057");
});

it("continues beyond 64 pages with bounded cursors, retained Previous, and First recovery", async () => {
  post.mockImplementation(async (_path, input) => page(Number(input.after_resource_id || 0) + 1, 64, true));
  const { result } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.canNext).toBe(true));
  for (let index = 1; index < 70; index++) {
    await act(async () => {
      await result.current.next();
    });
    expect(post.mock.calls.at(-1)![1]).toEqual({ after_resource_id: String(index * 64) });
    expect(result.current.pageNumber).toBe(index + 1);
  }
  expect(result.current.pageNumber).toBe(70);
  expect(result.current.pageOffset).toBe(6);
  expect(result.current.cursors).toHaveLength(64);
  expect(result.current.cursors[0]).toBe("384");
  expect(result.current.page?.entries).toHaveLength(64);
  expect(result.current.page?.entries[0].resource_id).toBe("4417");
  expect(result.current.canNext).toBe(true);
  expect(post).toHaveBeenCalledTimes(70);
  await act(async () => {
    await result.current.refresh();
  });
  expect(post.mock.calls.at(-1)![1]).toEqual({ after_resource_id: "4416" });
  expect(result.current.pageNumber).toBe(70);
  for (let index = 0; index < 63; index++)
    await act(async () => {
      await result.current.previous();
    });
  expect(result.current.pageNumber).toBe(7);
  expect(result.current.cursors).toEqual(["384"]);
  expect(result.current.page?.entries[0].resource_id).toBe("385");
  expect(result.current.canPrevious).toBe(false);
  expect(result.current.canFirst).toBe(true);
  const requests = post.mock.calls.length;
  await act(async () => {
    await result.current.previous();
  });
  expect(post).toHaveBeenCalledTimes(requests);
  await act(async () => {
    await result.current.first();
  });
  expect(post.mock.calls.at(-1)![1]).toEqual({});
  expect(result.current.pageNumber).toBe(1);
  expect(result.current.pageOffset).toBe(0);
  expect(result.current.cursors).toEqual(["0"]);
  expect(result.current.page?.entries[0].resource_id).toBe("1");
  expect(result.current.canFirst).toBe(false);
  expect(result.current.canNext).toBe(true);
});

it("does not dispatch an invalid target and allows retry after a transport error", async () => {
  const { result, rerender } = renderHook((id) => useRoleHistory(id), { initialProps: 0 });
  await waitFor(() => expect(result.current.state).toBe("error"));
  expect(post).not.toHaveBeenCalled();
  post.mockRejectedValueOnce(new Error("password=must-not-display"));
  rerender(1);
  await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(result.current.state).toBe("error"));
  expect(result.current.error).not.toContain("password");
  await act(async () => {
    await result.current.refresh();
  });
  expect(result.current.state).toBe("ready");
});
