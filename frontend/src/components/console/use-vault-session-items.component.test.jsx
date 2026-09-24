import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import { useVaultSessionItems } from "./use-vault-session-items";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn() }));

beforeEach(() => {
  apiGet.mockReset();
});

it("pages past the first 100 Vault items within the selected project", async () => {
  const first = Array.from({ length: 100 }, (_, index) => ({ id: index + 1, name: `KEY_${index + 1}` }));
  apiGet.mockImplementation(async (path) => {
    const params = new URL(path, "http://localhost").searchParams;
    expect(params.get("project_id")).toBe("4");
    expect(params.get("limit")).toBe("100");
    return { items: params.get("offset") === "100" ? [{ id: 101, name: "KEY_101" }] : first, total: 101 };
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
  let resolveOld;
  const oldResult = new Promise((resolve) => {
    resolveOld = resolve;
  });
  apiGet.mockImplementation((path) => {
    const query = new URL(path, "http://localhost").searchParams.get("q");
    return query === "old" ? oldResult : Promise.resolve({ items: [{ id: 2, name: "NEW_KEY" }], total: 1 });
  });
  const { result, rerender } = renderHook(({ query }) => useVaultSessionItems({ open: true, runtimeID: 2, projectID: "4", query }), {
    initialProps: { query: "old" },
  });
  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(1));
  const oldSignal = apiGet.mock.calls[0][1].signal;

  rerender({ query: "new" });
  expect(oldSignal.aborted).toBe(true);
  await waitFor(() => expect(result.current.items.map((item) => item.name)).toEqual(["NEW_KEY"]));
  await act(async () => resolveOld({ items: [{ id: 1, name: "OLD_KEY" }], total: 1 }));
  expect(result.current.items.map((item) => item.name)).toEqual(["NEW_KEY"]);
});

it("keeps a failed search retryable without changing the project scope", async () => {
  apiGet.mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({ items: [{ id: 7, name: "KEY_7" }], total: 1 });
  const { result } = renderHook(() => useVaultSessionItems({ open: true, runtimeID: 2, projectID: "4", query: "KEY_7" }));
  await waitFor(() => expect(result.current.error).toBe("offline"));
  act(() => result.current.retry());
  await waitFor(() => expect(result.current.items.map((item) => item.name)).toEqual(["KEY_7"]));
  expect(apiGet.mock.calls).toHaveLength(2);
  expect(apiGet.mock.calls[1][0]).toContain("project_id=4");
});
