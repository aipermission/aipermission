import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPut } from "../lib/api";
import { useGatewayResources } from "./use-gateway-resources";

// async-owner: src/components/use-gateway-activity-resources.js
// async-owner: src/components/use-gateway-core-resources.js

vi.mock("../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn() }));

function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

function renderResources(options = {}) {
  return renderHook(() =>
    useGatewayResources({
      pollIsCurrent: () => true,
      connectorKinds: options.connectorKinds || [],
      resolveConnectorModel: options.resolveConnectorModel || (() => null),
    }),
  );
}

describe("useGatewayResources", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPut.mockReset();
  });

  it("ignores a superseded target response", async () => {
    const older = deferred();
    apiGet.mockReturnValueOnce(older.promise).mockResolvedValueOnce({ items: [{ id: 2 }] });
    const { result } = renderResources();

    let oldLoad;
    await act(async () => {
      oldLoad = result.current.loadTargets();
      await result.current.loadTargets();
    });
    await act(async () => older.resolve({ items: [{ id: 1 }] }));
    await oldLoad;

    expect(result.current.targets.data).toEqual([{ id: 2 }]);
  });

  it("does not let an older MCP load overwrite a successful mutation", async () => {
    const older = deferred();
    apiGet.mockReturnValue(older.promise);
    apiPut.mockResolvedValue({ enabled: true, start_enabled: true });
    const { result } = renderResources();

    let load;
    act(() => {
      load = result.current.loadMCPRuntime();
    });
    await act(async () => result.current.setMCPRuntimeEnabled(true));
    await act(async () => older.resolve({ enabled: false, start_enabled: false }));
    await load;

    expect(result.current.mcpRuntime.data).toEqual({ enabled: true, start_enabled: true });
  });

  it("retains successful credential resources when one connector fails", async () => {
    const models = {
      good: { loadCredentialResources: vi.fn().mockResolvedValue([{ id: 1, name: "main" }]) },
      bad: { loadCredentialResources: vi.fn().mockRejectedValue(new Error("offline")) },
    };
    const { result } = renderResources({ connectorKinds: ["good", "bad"], resolveConnectorModel: (kind) => models[kind] });

    await act(async () => result.current.loadCredentials());

    expect(result.current.credentials.data).toEqual([
      expect.objectContaining({ id: 1, connector_kind: "good", resource_ref: "good:credential:1" }),
    ]);
    expect(result.current.credentials.errors).toEqual(["bad: offline"]);
  });

  it("keeps backup freshness records and provider failures visible together", async () => {
    apiGet.mockResolvedValue({
      items: [{ provider_id: 1, latest_remote_at: "2026-09-07T00:00:00Z" }],
      check_errors: [{ provider_id: 2, error: "offline" }],
    });
    const { result } = renderResources();

    await act(async () => result.current.loadBackupFreshness());

    expect(result.current.backupFreshness).toMatchObject({
      state: "ready",
      data: [{ provider_id: 1, latest_remote_at: "2026-09-07T00:00:00Z" }],
      checkErrors: [{ provider_id: 2, error: "offline" }],
      error: null,
    });
  });
});
