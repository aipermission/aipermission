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
