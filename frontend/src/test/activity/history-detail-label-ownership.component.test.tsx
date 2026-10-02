import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDelete, apiGet, apiPost } from "../../lib/api";
import { useHistoryPageState } from "../../pages/use-history-page-state";
import { historyEntryFixture } from "../history-fixtures";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiDelete: vi.fn() }));

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: Error) => void;
  const promise = new Promise<unknown>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

const original = { id: 1, name: "Original", color: "blue" };
const added = { id: 2, name: "Current", color: "green" };
const item = historyEntryFixture({ labels: [original], summary: "List preview" });

describe("History detail and label ownership", () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset();
    vi.mocked(apiPost).mockReset();
    vi.mocked(apiDelete).mockReset();
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/history-labels") return [original, added];
      if (path === "/api/history/targets" || path === "/api/projects") return { items: [] };
      return { items: [], total: 0, limit: 50, has_more: false };
    });
  });

  it.each(["attach", "detach"])("keeps newer %s labels while applying the late detail fields", async (operation) => {
    const detail = deferred();
    vi.mocked(apiGet).mockImplementationOnce(() => Promise.resolve([original, added]));
    const hook = renderHook(() => useHistoryPageState());
    vi.mocked(apiGet).mockImplementation((path) => (path === "/api/history/1" ? detail.promise : Promise.resolve([])));
    let opening!: Promise<void>;
    act(() => {
      opening = hook.result.current.openHistoryItem(item);
    });
    const labels = operation === "attach" ? [original, added] : [];
    vi.mocked(apiPost).mockResolvedValue(labels);
    vi.mocked(apiDelete).mockResolvedValue(labels);
    await act(async () => {
      if (operation === "attach") await hook.result.current.attachLabel(1, { name: added.name });
      else await hook.result.current.detachLabel(1, original.id);
    });
    expect(hook.result.current.selected?.labels).toEqual(labels);
    await act(async () => {
      detail.resolve({ ...item, summary: "Full detail", labels: [original] });
      await opening;
    });
    expect(hook.result.current.selected?.labels).toEqual(labels);
    expect(hook.result.current.selected?.summary).toBe("Full detail");
  });

  it("does not roll back a committed label when detail loading fails", async () => {
    const detail = deferred();
    const hook = renderHook(() => useHistoryPageState());
    vi.mocked(apiGet).mockImplementation((path) => (path === "/api/history/1" ? detail.promise : Promise.resolve([])));
    vi.mocked(apiPost).mockResolvedValue([original, added]);
    let opening!: Promise<void>;
    act(() => {
      opening = hook.result.current.openHistoryItem(item);
    });
    await act(async () => {
      await hook.result.current.attachLabel(1, { name: added.name });
    });
    await act(async () => {
      detail.reject(new Error("detail unavailable"));
      await opening;
    });
    expect(hook.result.current.selected?.labels).toEqual([original, added]);
    expect(hook.result.current.selected?.summary).toBe("List preview");
  });

  it("accepts server labels when no label mutation completed for the selected detail", async () => {
    const detail = deferred();
    const hook = renderHook(() => useHistoryPageState());
    vi.mocked(apiGet).mockImplementation((path) => (path === "/api/history/1" ? detail.promise : Promise.resolve([])));
    let opening!: Promise<void>;
    act(() => {
      opening = hook.result.current.openHistoryItem(item);
    });
    await act(async () => {
      detail.resolve({ ...item, labels: [added], summary: "Server detail" });
      await opening;
    });
    expect(hook.result.current.selected?.labels).toEqual([added]);
  });

  it("does not let a late detail or label response replace a different selected entry", async () => {
    const detail = deferred();
    const mutation = deferred();
    const hook = renderHook(() => useHistoryPageState());
    const second = historyEntryFixture({ id: 2, labels: [added], summary: "Other detail" });
    vi.mocked(apiGet).mockImplementation((path) => (path === "/api/history/1" ? detail.promise : Promise.resolve(second)));
    vi.mocked(apiPost).mockReturnValue(mutation.promise);
    let opening!: Promise<void>;
    let attaching!: Promise<void>;
    act(() => {
      opening = hook.result.current.openHistoryItem(item);
      attaching = hook.result.current.attachLabel(1, { name: added.name });
    });
    await act(async () => {
      await hook.result.current.openHistoryItem(second);
    });
    await act(async () => {
      mutation.resolve([original, added]);
      await attaching;
      detail.resolve(item);
      await opening;
    });
    expect(hook.result.current.selected).toEqual(second);
  });

  it("keeps reopened same-entry detail ownership separate from a late prior read", async () => {
    const older = deferred();
    const newer = deferred();
    const hook = renderHook(() => useHistoryPageState());
    vi.mocked(apiGet)
      .mockImplementationOnce(() => older.promise)
      .mockImplementationOnce(() => newer.promise);
    let first!: Promise<void>;
    let second!: Promise<void>;
    act(() => {
      first = hook.result.current.openHistoryItem(item);
    });
    act(() => {
      hook.result.current.closeHistoryItem();
    });
    act(() => {
      second = hook.result.current.openHistoryItem(item);
    });
    vi.mocked(apiPost).mockResolvedValue([added]);
    await act(async () => {
      await hook.result.current.attachLabel(1, { name: added.name });
    });
    await act(async () => {
      newer.resolve({ ...item, summary: "Current detail" });
      await second;
    });
    await act(async () => {
      older.resolve({ ...item, summary: "Old detail" });
      await first;
    });
    expect(hook.result.current.selected?.summary).toBe("Current detail");
    expect(hook.result.current.selected?.labels).toEqual([added]);
  });
});
