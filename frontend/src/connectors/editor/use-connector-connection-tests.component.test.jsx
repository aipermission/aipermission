import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { connectorTestKey, useConnectorConnectionTests } from "./use-connector-connection-tests";

const target = { connector_kind: "example", id: 7 };
const profile = { id: 9 };

afterEach(() => {
  vi.useRealTimers();
});

describe("useConnectorConnectionTests", () => {
  it("records success and releases the cooldown deterministically", async () => {
    vi.useFakeTimers();
    const model = { test: vi.fn(async () => ({ ok: true, error: null, data: { duration_ms: 3 } })) };
    const { result } = renderHook(() =>
      useConnectorConnectionTests({ modelForKind: () => model, onOperation: () => false, cooldownMs: 25 }),
    );

    await act(async () => result.current.run(target, profile));
    const key = connectorTestKey(target, profile);
    expect(result.current.tests[key]).toMatchObject({ state: "ok", cooldown: true, data: { duration_ms: 3 } });

    act(() => vi.advanceTimersByTime(25));
    expect(result.current.tests[key].cooldown).toBe(false);
  });

  it("rejects a missing profile before calling the connector model", async () => {
    const model = { test: vi.fn() };
    const { result } = renderHook(() => useConnectorConnectionTests({ modelForKind: () => model }));

    await act(async () => result.current.run(target, null));

    expect(model.test).not.toHaveBeenCalled();
    expect(result.current.tests[connectorTestKey(target, null)].error).toBe("Select a credential profile before testing.");
  });

  it("returns connector-owned recovery operations without inventing shared behavior", async () => {
    const recovery = { open: true, connector_kind: "example", type: "trust" };
    const onOperation = vi.fn(() => true);
    const model = {
      test: vi.fn(async () => Promise.reject(new Error("Trust required"))),
      operationFromError: vi.fn(() => recovery),
    };
    const { result } = renderHook(() => useConnectorConnectionTests({ modelForKind: () => model, onOperation }));

    await act(async () => result.current.run(target, profile));

    expect(onOperation).toHaveBeenCalledWith(recovery);
    expect(result.current.tests[connectorTestKey(target, profile)].state).toBe("idle");
  });
});
