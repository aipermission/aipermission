import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPut } from "./api";
import { connectorActionCacheKey, useConnectorPermissions } from "./use-connector-permissions";

vi.mock("./api", () => ({
  apiGet: vi.fn(),
  apiPut: vi.fn(),
}));

function deferred() {
  let resolve;
  const promise = new Promise((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

describe("useConnectorPermissions", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPut.mockReset();
  });

  it("does not let an older permission load overwrite the latest token set", async () => {
    const first = deferred();
    const second = deferred();
    apiGet.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const { result } = renderHook(() => useConnectorPermissions());

    act(() => {
      void result.current.loadAllConnectorPermissions([{ id: 1 }]);
      void result.current.loadAllConnectorPermissions([{ id: 2 }]);
    });
    await act(async () => first.resolve({ items: [{ action_name: "old" }] }));
    expect(result.current.connectorPermissionState.data).toEqual({});

    await act(async () => second.resolve({ items: [{ action_name: "new" }] }));
    expect(result.current.connectorPermissionState.data).toEqual({ 2: [{ action_name: "new" }] });
  });

  it("rejects a strict permission refresh when a newer load supersedes it", async () => {
    const stale = deferred();
    apiGet.mockReturnValueOnce(stale.promise);
    const { result } = renderHook(() => useConnectorPermissions());

    let strictRefresh;
    await act(async () => {
      strictRefresh = result.current.loadAllConnectorPermissions([{ id: 1 }], { requireCurrent: true });
      await result.current.loadAllConnectorPermissions([]);
      stale.resolve({ items: [{ action_name: "stale" }] });
      await expect(strictRefresh).rejects.toThrow("Permission refresh was superseded");
    });

    expect(result.current.connectorPermissionState.data).toEqual({});
  });

  it("rejects a strict permission refresh when the current request fails", async () => {
    apiGet.mockRejectedValueOnce(new Error("permission service unavailable"));
    const { result } = renderHook(() => useConnectorPermissions());

    await act(async () => {
      await expect(result.current.loadAllConnectorPermissions([{ id: 1 }], { requireCurrent: true })).rejects.toThrow(
        "permission service unavailable",
      );
    });

    expect(result.current.connectorPermissionState).toMatchObject({ state: "error", error: "permission service unavailable" });
  });

  it("keeps the newest action catalog for the same target profile", async () => {
    const first = deferred();
    const second = deferred();
    apiGet.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
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
  });

  it("does not let an older permission GET overwrite a successful mutation", async () => {
    const load = deferred();
    let loadSignal;
    apiGet.mockImplementationOnce((_path, options) => {
      loadSignal = options.signal;
      return load.promise;
    });
    apiPut.mockResolvedValueOnce({ items: [{ action_name: "write", execution_rule: "always" }] });
    const { result } = renderHook(() => useConnectorPermissions());

    let loadResult;
    await act(async () => {
      loadResult = result.current.loadAllConnectorPermissions([{ id: 1 }]);
      await result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "write", execution_rule: "always" },
      ]);
    });
    expect(loadSignal.aborted).toBe(true);
    expect(result.current.connectorPermissionState.data[1]).toEqual([{ action_name: "write", execution_rule: "always" }]);

    await act(async () => load.resolve({ items: [{ action_name: "old", execution_rule: "disabled" }] }));
    await loadResult;
    expect(result.current.connectorPermissionState.data[1]).toEqual([{ action_name: "write", execution_rule: "always" }]);
  });

  it("submits the server revision and advances it after a permission update", async () => {
    apiGet.mockResolvedValueOnce({ items: [], revision: "permissions-1" });
    apiPut.mockResolvedValueOnce({ items: [{ action_name: "read" }], revision: "permissions-2" });
    const { result } = renderHook(() => useConnectorPermissions());

    await act(async () => result.current.loadAllConnectorPermissions([{ id: 1 }]));
    await act(async () => result.current.replaceTokenConnectorPermissions(1, [{ target_id: 7, profile_id: 9, action_name: "read" }]));

    expect(apiPut).toHaveBeenCalledWith(
      "/api/tokens/1/connector-permissions",
      {
        permissions: [
          {
            target_id: 7,
            profile_id: 9,
            action_name: "read",
            execution_rule: undefined,
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
    const signals = [];
    apiPut
      .mockImplementationOnce((_path, _body, options) => {
        signals.push(options.signal);
        return first.promise;
      })
      .mockImplementationOnce((_path, _body, options) => {
        signals.push(options.signal);
        return second.promise;
      });
    const { result } = renderHook(() => useConnectorPermissions());

    let firstResult;
    let secondResult;
    act(() => {
      firstResult = result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "read", execution_rule: "prompt" },
      ]);
      secondResult = result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "read", execution_rule: "always" },
      ]);
    });
    expect(signals[0].aborted).toBe(true);

    await act(async () => second.resolve({ items: [{ action_name: "read", execution_rule: "always" }] }));
    await secondResult;
    await act(async () => first.resolve({ items: [{ action_name: "read", execution_rule: "prompt" }] }));
    await firstResult;

    expect(result.current.connectorPermissionState.data[1]).toEqual([{ action_name: "read", execution_rule: "always" }]);
  });

  it("does not let a superseded mutation invalidate a later permission load", async () => {
    const staleMutation = deferred();
    const currentLoad = deferred();
    apiPut.mockReturnValueOnce(staleMutation.promise).mockResolvedValueOnce({
      items: [{ action_name: "read", execution_rule: "always" }],
    });
    apiGet.mockReturnValueOnce(currentLoad.promise);
    const { result } = renderHook(() => useConnectorPermissions());

    let staleResult;
    await act(async () => {
      staleResult = result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "read", execution_rule: "prompt" },
      ]);
      await result.current.replaceTokenConnectorPermissions(1, [
        { target_id: 7, profile_id: 9, action_name: "read", execution_rule: "always" },
      ]);
    });

    let loadResult;
    act(() => {
      loadResult = result.current.loadAllConnectorPermissions([{ id: 1 }]);
    });
    await act(async () => staleMutation.resolve({ items: [{ action_name: "read", execution_rule: "prompt" }] }));
    await staleResult;
    await act(async () => currentLoad.resolve({ items: [{ action_name: "read", execution_rule: "always" }] }));
    await loadResult;

    expect(result.current.connectorPermissionState.state).toBe("ready");
    expect(result.current.connectorPermissionState.data[1]).toEqual([{ action_name: "read", execution_rule: "always" }]);
  });
});
