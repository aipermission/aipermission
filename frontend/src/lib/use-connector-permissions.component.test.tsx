import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPut } from "./api";
import { connectorActionCacheKey, useConnectorPermissions } from "./use-connector-permissions";
import {
  connectorPermissionFixture as permission,
  connectorPermissionSnapshot as snapshot,
} from "../test/connector-permission-fixtures.ts";

vi.mock("./api", () => ({
  apiGet: vi.fn(),
  apiPut: vi.fn(),
}));

function deferred() {
  let resolve!: (_value: unknown) => void;
  const promise = new Promise<unknown>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

describe("useConnectorPermissions", () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset();
    vi.mocked(apiPut).mockReset();
  });

  it("does not let an older permission load overwrite the latest token set", async () => {
    const first = deferred();
    const second = deferred();
    vi.mocked(apiGet).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const { result } = renderHook(() => useConnectorPermissions());

    act(() => {
      void result.current.loadAllConnectorPermissions([{ id: 1 }]);
      void result.current.loadAllConnectorPermissions([{ id: 2 }]);
    });
    await act(async () => first.resolve({ items: [{ action_name: "old" }] }));
    expect(result.current.connectorPermissionState.data).toEqual({});

    await act(async () => second.resolve(snapshot([permission({ action_name: "new" })], "r2")));
    expect(result.current.connectorPermissionState.data).toEqual({ 2: [permission({ action_name: "new" })] });
  });

  it("rejects a strict permission refresh when a newer load supersedes it", async () => {
    const stale = deferred();
    vi.mocked(apiGet).mockReturnValueOnce(stale.promise);
    const { result } = renderHook(() => useConnectorPermissions());

    let strictRefresh: ReturnType<ReturnType<typeof useConnectorPermissions>["loadAllConnectorPermissions"]> | undefined;
    await act(async () => {
      strictRefresh = result.current.loadAllConnectorPermissions([{ id: 1 }], { requireCurrent: true });
      await result.current.loadAllConnectorPermissions([]);
      stale.resolve({ items: [{ action_name: "stale" }] });
      await expect(strictRefresh).rejects.toThrow("Permission refresh was superseded");
    });

    expect(result.current.connectorPermissionState.data).toEqual({});
  });

  it("rejects a strict permission refresh when the current request fails", async () => {
    vi.mocked(apiGet).mockRejectedValueOnce(new Error("permission service unavailable"));
    const { result } = renderHook(() => useConnectorPermissions());

    await act(async () => {
      await expect(result.current.loadAllConnectorPermissions([{ id: 1 }], { requireCurrent: true })).rejects.toThrow(
        "permission service unavailable",
      );
    });

    expect(result.current.connectorPermissionState).toMatchObject({ state: "error", error: "permission service unavailable" });
  });

  it("rejects a malformed current permission snapshot without committing it", async () => {
    vi.mocked(apiGet).mockResolvedValueOnce({ items: [permission({ execution_rule: "future" })], revision: "r1" });
    const { result } = renderHook(() => useConnectorPermissions());

    await act(async () => {
      await expect(result.current.loadAllConnectorPermissions([{ id: 1 }], { requireCurrent: true })).rejects.toThrow(
        "Invalid token permission response from gateway.",
      );
    });

    expect(result.current.connectorPermissionState).toMatchObject({ state: "error", data: {} });
  });

  it("keeps the newest action catalog for the same target profile", async () => {
    const first = deferred();
    const second = deferred();
    vi.mocked(apiGet).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const target = { connector_kind: "example", target_id: 7, profile_id: 9 };
    const cacheKey = connectorActionCacheKey(target, target.profile_id);
    const { result } = renderHook(() => useConnectorPermissions());

    act(() => {
      void result.current.loadConnectorActions(target);
      void result.current.loadConnectorActions(target);
    });
    await act(async () => second.resolve({ items: [{ name: "new" }] }));
    expect(result.current.connectorPermissionState.actionsByTargetRef[cacheKey]).toEqual([{ name: "new" }]);

    await act(async () => first.resolve({ items: [{ name: "old" }] }));
    expect(result.current.connectorPermissionState.actionsByTargetRef[cacheKey]).toEqual([{ name: "new" }]);
    for (const invalid of [
      { description: "missing name" },
      { name: "" },
      { name: "read", risk: true },
      { name: "read", category: [] },
      { name: "read", description: {} },
    ]) {
      vi.mocked(apiGet).mockResolvedValueOnce({ items: [invalid] });
      await act(async () => result.current.loadConnectorActions(target));
      expect(result.current.connectorPermissionState.actionsByTargetRef[cacheKey]).toEqual([{ name: "new" }]);
      expect(result.current.connectorPermissionState.error).toBe("Invalid connector action catalog from gateway.");
    }
  });

  it("does not let an older permission GET overwrite a successful mutation", async () => {
    const load = deferred();
    let loadSignal: AbortSignal | undefined;
    vi.mocked(apiGet).mockImplementationOnce((_path, options) => {
      loadSignal = options?.signal;
      return load.promise;
    });
    vi.mocked(apiPut).mockResolvedValueOnce(snapshot([permission({ action_name: "write", execution_rule: "always_run" })], "r2"));
    const { result } = renderHook(() => useConnectorPermissions());

    let loadResult: ReturnType<ReturnType<typeof useConnectorPermissions>["loadAllConnectorPermissions"]> | undefined;
    await act(async () => {
      loadResult = result.current.loadAllConnectorPermissions([{ id: 1 }]);
      await result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "write", execution_rule: "always_run" },
      ]);
    });
    expect(loadSignal?.aborted).toBe(true);
    expect(result.current.connectorPermissionState.data[1]).toEqual([permission({ action_name: "write", execution_rule: "always_run" })]);

    await act(async () => load.resolve({ items: [{ action_name: "old", execution_rule: "disabled" }] }));
    await loadResult;
    expect(result.current.connectorPermissionState.data[1]).toEqual([permission({ action_name: "write", execution_rule: "always_run" })]);
  });

  it("submits the server revision and advances it after a permission update", async () => {
    vi.mocked(apiGet).mockResolvedValueOnce({ items: [], revision: "permissions-1" });
    vi.mocked(apiPut).mockResolvedValueOnce(snapshot([permission({ action_name: "read" })], "permissions-2"));
    const { result } = renderHook(() => useConnectorPermissions());

    await act(async () => result.current.loadAllConnectorPermissions([{ id: 1 }]));
    await act(async () =>
      result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "read", execution_rule: "approval_required" },
      ]),
    );

    expect(apiPut).toHaveBeenCalledWith(
      "/api/tokens/1/connector-permissions",
      {
        permissions: [
          {
            target_id: 7,
            profile_id: 9,
            action_name: "read",
            execution_rule: "approval_required",
            expires_at: undefined,
          },
        ],
        expected_revision: "permissions-1",
      },
      { signal: expect.any(AbortSignal) },
    );
    expect(result.current.connectorPermissionState.revisionsByToken[1]).toBe("permissions-2");
  });

  it("keeps only the newest mutation result for one token", async () => {
    const first = deferred();
    const second = deferred();
    const signals: (AbortSignal | undefined)[] = [];
    vi.mocked(apiPut)
      .mockImplementationOnce((_path, _body, options) => {
        signals.push(options?.signal);
        return first.promise;
      })
      .mockImplementationOnce((_path, _body, options) => {
        signals.push(options?.signal);
        return second.promise;
      });
    const { result } = renderHook(() => useConnectorPermissions());

    let firstResult: ReturnType<ReturnType<typeof useConnectorPermissions>["replaceTokenConnectorPermissions"]> | undefined;
    let secondResult: ReturnType<ReturnType<typeof useConnectorPermissions>["replaceTokenConnectorPermissions"]> | undefined;
    act(() => {
      firstResult = result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "read", execution_rule: "approval_required" },
      ]);
      secondResult = result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "read", execution_rule: "always_run" },
      ]);
    });
    expect(signals[0]?.aborted).toBe(true);

    await act(async () => second.resolve(snapshot([permission({ action_name: "read", execution_rule: "always_run" })], "r2")));
    await secondResult;
    await act(async () => first.resolve({ items: [{ action_name: "read", execution_rule: "approval_required" }] }));
    await firstResult;

    expect(result.current.connectorPermissionState.data[1]).toEqual([permission({ action_name: "read", execution_rule: "always_run" })]);
  });

  it("does not let a superseded mutation invalidate a later permission load", async () => {
    const staleMutation = deferred();
    const currentLoad = deferred();
    vi.mocked(apiPut)
      .mockReturnValueOnce(staleMutation.promise)
      .mockResolvedValueOnce(snapshot([permission({ action_name: "read", execution_rule: "always_run" })], "r2"));
    vi.mocked(apiGet).mockReturnValueOnce(currentLoad.promise);
    const { result } = renderHook(() => useConnectorPermissions());

    let staleResult: ReturnType<ReturnType<typeof useConnectorPermissions>["replaceTokenConnectorPermissions"]> | undefined;
    await act(async () => {
      staleResult = result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "read", execution_rule: "approval_required" },
      ]);
      await result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "read", execution_rule: "always_run" },
      ]);
    });

    let loadResult: ReturnType<ReturnType<typeof useConnectorPermissions>["loadAllConnectorPermissions"]> | undefined;
    act(() => {
      loadResult = result.current.loadAllConnectorPermissions([{ id: 1 }]);
    });
    await act(async () => staleMutation.resolve({ items: [{ action_name: "read", execution_rule: "approval_required" }] }));
    await staleResult;
    await act(async () => currentLoad.resolve(snapshot([permission({ action_name: "read", execution_rule: "always_run" })], "r3")));
    await loadResult;

    expect(result.current.connectorPermissionState.state).toBe("ready");
    expect(result.current.connectorPermissionState.data[1]).toEqual([permission({ action_name: "read", execution_rule: "always_run" })]);
  });
});
