import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import { useVaultSessionItems } from "./use-vault-session-items";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn() }));

beforeEach(() => {
  vi.mocked(apiGet).mockReset();
});

it("pages past the first 100 Vault items within the selected project", async () => {
  const first = Array.from({ length: 100 }, (_, index) => ({ id: index + 1, name: `KEY_${index + 1}`, owner_project_id: 4 }));
  vi.mocked(apiGet).mockImplementation(async (path) => {
    const params = new URL(path, "http://localhost").searchParams;
    expect(params.get("project_id")).toBe("4");
    expect(params.get("limit")).toBe("100");
    return { items: params.get("offset") === "100" ? [{ id: 101, name: "KEY_101", owner_project_id: 4 }] : first, total: 101 };
  });
  const { result } = renderHook(() => useVaultSessionItems({ open: true, runtimeID: 2, projectID: "4", query: "" }));
  await waitFor(() => expect(result.current.items).toHaveLength(100));
  expect(result.current.hasMore).toBe(true);

  act(() => result.current.loadMore());
  await waitFor(() => expect(result.current.items).toHaveLength(101));
  expect(result.current.items[100].name).toBe("KEY_101");
  expect(result.current.hasMore).toBe(false);
});

it("ignores a stale server search after the search term changes", async () => {
  let resolveOld: (_value: unknown) => void = () => { throw new Error("Deferred request is not initialized"); };
  const oldResult = new Promise<unknown>((resolve) => {
    resolveOld = resolve;
  });
  vi.mocked(apiGet).mockImplementation((path) => {
    const query = new URL(path, "http://localhost").searchParams.get("q");
    return query === "old" ? oldResult : Promise.resolve({ items: [{ id: 2, name: "NEW_KEY", owner_project_id: 4 }], total: 1 });
  });
  const { result, rerender } = renderHook(({ query }) => useVaultSessionItems({ open: true, runtimeID: 2, projectID: "4", query }), {
    initialProps: { query: "old" },
  });
  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(1));
  const options = vi.mocked(apiGet).mock.calls[0][1];
  if (!options || !("signal" in options) || !(options.signal instanceof AbortSignal)) throw new Error("Missing request signal");
  const oldSignal = options.signal;

  rerender({ query: "new" });
  expect(oldSignal.aborted).toBe(true);
  await waitFor(() => expect(result.current.items.map((item) => item.name)).toEqual(["NEW_KEY"]));
  await act(async () => resolveOld({ items: [{ id: 1, name: "OLD_KEY" }], total: 1 }));
  expect(result.current.items.map((item) => item.name)).toEqual(["NEW_KEY"]);
});

it("keeps a failed search retryable without changing the project scope", async () => {
  vi.mocked(apiGet).mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({ items: [{ id: 7, name: "KEY_7", owner_project_id: 4 }], total: 1 });
  const { result } = renderHook(() => useVaultSessionItems({ open: true, runtimeID: 2, projectID: "4", query: "KEY_7" }));
  await waitFor(() => expect(result.current.error).toBe("offline"));
  act(() => result.current.retry());
  await waitFor(() => expect(result.current.items.map((item) => item.name)).toEqual(["KEY_7"]));
  expect(vi.mocked(apiGet).mock.calls).toHaveLength(2);
  expect(vi.mocked(apiGet).mock.calls[1][0]).toContain("project_id=4");
});

it("keeps a validated first page when the appended page is malformed", async () => {
  vi.mocked(apiGet)
    .mockResolvedValueOnce({ items: [{ id: 1, name: "KEY_1", owner_project_id: 4 }], total: 2 })
    .mockResolvedValueOnce({ items: [{ id: 2, name: "KEY_2", owner_project_id: "wrong identity" }], total: 2 });
  const { result } = renderHook(() => useVaultSessionItems({ open: true, runtimeID: 2, projectID: "4", query: "" }));
  await waitFor(() => expect(result.current.items).toHaveLength(1));
  act(() => result.current.loadMore());
  await waitFor(() => expect(result.current.status).toBe("error"));
  expect(result.current.items.map((item) => item.id)).toEqual([1]);
  expect(result.current.nextOffset).toBe(1);
});

it("discards an older project page after the selected project changes", async () => {
  let resolveOld: (_value: unknown) => void = () => { throw new Error("Deferred request is not initialized"); };
  const oldResult = new Promise<unknown>((resolve) => { resolveOld = resolve; });
  vi.mocked(apiGet).mockReturnValueOnce(oldResult).mockResolvedValueOnce({ items: [{ id: 2, name: "NEW_PROJECT_KEY", owner_project_id: 5 }], total: 1 });
  const { result, rerender } = renderHook(({ projectID }) => useVaultSessionItems({ open: true, runtimeID: 2, projectID, query: "" }), { initialProps: { projectID: "4" } });
  await waitFor(() => expect(apiGet).toHaveBeenCalledOnce());
  rerender({ projectID: "5" });
  expect(result.current.items).toEqual([]);
  await waitFor(() => expect(result.current.items.map((item) => item.name)).toEqual(["NEW_PROJECT_KEY"]));
  await act(async () => resolveOld({ items: [{ id: 1, name: "OLD_PROJECT_KEY", owner_project_id: 4 }], total: 1 }));
  expect(result.current.items.map((item) => item.id)).toEqual([2]);
});
