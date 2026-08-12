import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet } from "./api";
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
});
