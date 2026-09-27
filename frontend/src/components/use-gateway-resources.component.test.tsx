import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet as realGet, apiPut as realPut } from "../lib/api";
import { useGatewayResources } from "./use-gateway-resources";
import type { GatewayResourceOptions } from "./use-gateway-resources.ts";
import type { GatewayTarget } from "../lib/gateway-contracts/core-resource-contracts.ts";

const apiGet = vi.mocked(realGet);
const apiPut = vi.mocked(realPut);
type Model = NonNullable<ReturnType<NonNullable<GatewayResourceOptions["resolveConnectorModel"]>>>;

// async-owner: src/components/use-gateway-activity-resources.ts
// async-owner: src/components/use-gateway-core-resources.ts

vi.mock("../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn() }));

function deferred() {
  let resolve!: (_value: unknown) => void;
  const promise = new Promise<unknown>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

function target(id: number, extra: Partial<GatewayTarget> = {}): GatewayTarget {
  return {
    id,
    target_id: id,
    profile_id: id,
    project_id: 1,
    connector_kind: "fixture",
    ref: `fixture:${id}:${id}`,
    target_name: `Target ${id}`,
    profile_kind: "default",
    profile_label: "main",
    project_name: "My Project",
    project_slug: "my-project",
    status: "active",
    created_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-01T00:00:00Z",
    ...extra,
  };
}

function renderResources(options: Partial<GatewayResourceOptions> = {}) {
  return renderHook(() =>
    useGatewayResources({
      pollIsCurrent: () => true,
      connectorKinds: options.connectorKinds || [],
      resolveConnectorModel: options.resolveConnectorModel || (() => null),
    }),
  );
}

beforeEach(() => {
  vi.mocked(apiGet).mockReset();
  vi.mocked(apiPut).mockReset();
});

describe("useGatewayResources", () => {
  it("ignores a superseded target response", async () => {
    const older = deferred();
    apiGet.mockReturnValueOnce(older.promise).mockResolvedValueOnce({ items: [target(2)] });
    const { result } = renderResources();

    let oldLoad: Promise<unknown> | undefined;
    await act(async () => {
      oldLoad = result.current.loadTargets(1);
      await result.current.loadTargets(1);
    });
    await act(async () => older.resolve({ items: [target(1)] }));
    await oldLoad;

    expect(result.current.targets.data).toEqual([target(2)]);
    expect(apiGet.mock.calls[1][1]).toEqual(expect.objectContaining({ signal: expect.any(AbortSignal), timeoutMs: 4000 }));
  });

  it("keeps the last target snapshot through a transient failure and recovers", async () => {
    apiGet
      .mockResolvedValueOnce({ items: [target(1)] })
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ items: [target(2)] });
    const { result } = renderResources();

    await act(async () => result.current.loadTargets());
    await act(async () => result.current.loadTargets());
    expect(result.current.targets).toEqual({ state: "error", data: [target(1)], error: "offline" });

    await act(async () => result.current.loadTargets());
    expect(result.current.targets).toEqual({ state: "ready", data: [target(2)], error: null });
  });

  it("does not let an older MCP load overwrite a successful mutation", async () => {
    const older = deferred();
    apiGet.mockReturnValue(older.promise);
    apiPut.mockResolvedValue({ enabled: true, start_enabled: true });
    const { result } = renderResources();

    let load: Promise<unknown> | undefined;
    act(() => {
      load = result.current.loadMCPRuntime();
    });
    await act(async () => result.current.setMCPRuntimeEnabled(true));
    await act(async () => older.resolve({ enabled: false, start_enabled: false }));
    await load;

    expect(result.current.mcpRuntime.data).toEqual({ enabled: true, start_enabled: true });
  });

  it("retains successful credential resources when one connector fails", async () => {
    const models: Record<string, Model> = {
      good: { loadCredentialResources: vi.fn().mockResolvedValue([{ id: 1, name: "main" }]) },
      bad: { loadCredentialResources: vi.fn().mockRejectedValue(new Error("offline")) },
    };
    const { result } = renderResources({ connectorKinds: ["good", "bad"], resolveConnectorModel: (kind) => models[kind] });

    await act(async () => result.current.loadCredentials());

    expect(result.current.credentials.data).toEqual([
      expect.objectContaining({ id: 1, connector_kind: "good", resource_ref: "good:credential:1" }),
    ]);
    expect(result.current.credentials.errors).toEqual(["bad: offline"]);
    expect(models.good.loadCredentialResources).toHaveBeenCalledWith({ signal: expect.any(AbortSignal) });
  });

  it("keeps each connector credential slice through a transient failure", async () => {
    const ssh = {
      loadCredentialResources: vi
        .fn()
        .mockResolvedValueOnce([{ id: 1, name: "main" }])
        .mockRejectedValueOnce(new Error("ssh timeout")),
    };
    const { result } = renderResources({ connectorKinds: ["ssh"], resolveConnectorModel: () => ssh });

    await act(async () => result.current.loadCredentials(1));
    await act(async () => result.current.loadCredentials(2));

    expect(result.current.credentials.data).toEqual([
      expect.objectContaining({ id: 1, connector_kind: "ssh", resource_ref: "ssh:credential:1" }),
    ]);
    expect(result.current.credentials.errors).toEqual(["ssh: ssh timeout"]);
  });

  it("drops credential slices for connector kinds removed from the catalog", async () => {
    const kinds = ["ssh", "retired"];
    const models: Record<string, Model> = {
      ssh: { loadCredentialResources: vi.fn().mockResolvedValue([{ id: 1, name: "main" }]) },
      retired: { loadCredentialResources: vi.fn().mockResolvedValue([{ id: 2, name: "old" }]) },
    };
    const { result } = renderResources({ connectorKinds: kinds, resolveConnectorModel: (kind) => models[kind] });

    await act(async () => result.current.loadCredentials(1));
    kinds.pop();
    await act(async () => result.current.loadCredentials(2));

    expect(result.current.credentials.data).toEqual([expect.objectContaining({ id: 1, connector_kind: "ssh" })]);
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

  it("projects gateway state and keeps core snapshots through poll failures", async () => {
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/status") return { status: "running" };
      if (path === "/api/tokens") return [{ id: 3, name: "Client" }];
      if (path === "/api/settings/mcp-runtime") return { enabled: true, start_enabled: false };
      throw new Error(`Unexpected GET ${path}`);
    });
    const { result } = renderResources();
    expect(result.current.gatewayState).toBe("checking");

    await act(async () => Promise.all([result.current.loadStatus(1), result.current.loadTokens(1), result.current.loadMCPRuntime(1)]));
    expect(result.current.gatewayState).toBe("running");
    expect(result.current.tokens.data).toEqual([{ id: 3, name: "Client" }]);
    expect(result.current.mcpRuntime.data).toEqual({ enabled: true, start_enabled: false });

    apiGet.mockRejectedValue(new Error("gateway offline"));
    await act(async () => Promise.all([result.current.loadStatus(2), result.current.loadTokens(2), result.current.loadMCPRuntime(2)]));
    expect(result.current.gatewayState).toBe("unreachable");
    expect(result.current.tokens).toMatchObject({ state: "error", data: [{ id: 3, name: "Client" }], error: "gateway offline" });
    expect(result.current.mcpRuntime).toMatchObject({ state: "error", data: { enabled: true, start_enabled: false } });
  });

  it("derives live console targets from the latest target snapshot", async () => {
    const resolveConnectorModel = vi.fn(() => ({
      usesLiveConsole: () => true,
      liveConsoleRuntimeTarget: ({ target }: { target: GatewayTarget }) => ({
        ...target,
        id: target.runtime_id || 0,
        name: target.target_name,
        runtime: true,
      }),
    }));
    apiGet.mockResolvedValue({ items: [target(8, { runtime_id: 9 })] });
    const { result } = renderResources({ resolveConnectorModel });
    await act(async () => result.current.loadTargets());
    expect(result.current.liveConsoleTargets).toMatchObject({
      state: "ready",
      data: [{ id: 9, runtime_id: 9, connector_kind: "fixture", runtime: true }],
    });
  });
});

describe("captured native gateway services", () => {
  it("uses captured native services for default credential reads and runtime projection", async () => {
    const gatewayTarget = target(3, {
      connector_kind: "ssh",
      ref: "ssh:3:7",
      profile_id: 7,
      runtime_id: 19,
      config: { host: "endpoint", port: 22 },
      public: { username: "operator", ssh_key_id: 11 },
    });
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/targets") return { items: [gatewayTarget] };
      if (path === "/api/connectors/ssh/credentials") return [{ id: 11, name: "Credential", key_type: "ed25519" }];
      throw new Error(`Unexpected GET ${path}`);
    });
    const { result } = renderHook(() => useGatewayResources({ pollIsCurrent: () => true, connectorKinds: ["ssh", "postgres"] }));
    await act(async () => Promise.all([result.current.loadTargets(5), result.current.loadCredentials(5)]));
    expect(result.current.liveConsoleTargets.data).toEqual([
      expect.objectContaining({
        id: 19,
        connector_ref: "ssh:3:7",
        target_id: 3,
        profile_id: 7,
        host: "endpoint",
        username: "operator",
        target: gatewayTarget,
      }),
    ]);
    expect(result.current.credentials.data).toEqual([
      expect.objectContaining({ id: 11, connector_kind: "ssh", resource_ref: "ssh:ssh_key:11" }),
    ]);
    expect(result.current.credentials.errors).toEqual([]);
    expect(apiGet).toHaveBeenCalledWith(
      "/api/connectors/ssh/credentials",
      expect.objectContaining({ signal: expect.any(AbortSignal), timeoutMs: 4000 }),
    );
  });

  it("aborts superseded native credential reads and ignores their eventual payload", async () => {
    const older = deferred();
    apiGet.mockReturnValueOnce(older.promise).mockResolvedValueOnce([{ id: 12, name: "Current", key_type: "ed25519" }]);
    const { result } = renderHook(() => useGatewayResources({ pollIsCurrent: () => true, connectorKinds: ["ssh"] }));
    let oldLoad: Promise<unknown> | undefined;
    act(() => {
      oldLoad = result.current.loadCredentials(1);
    });
    const signal = apiGet.mock.calls[0][1]?.signal;
    expect(signal?.aborted).toBe(false);
    await act(async () => {
      await result.current.loadCredentials(2);
    });
    expect(signal?.aborted).toBe(true);
    await act(async () => {
      older.resolve([{ id: 11, name: "Stale", key_type: "ed25519" }]);
      await oldLoad;
    });
    expect(result.current.credentials.data).toEqual([expect.objectContaining({ id: 12, name: "Current" })]);
    expect(result.current.credentials.errors).toEqual([]);
  });

  it("surfaces native projection failures without crashing the shell and recovers on the next target load", async () => {
    const validTarget = target(3, {
      connector_kind: "docker",
      ref: "docker:3:7",
      profile_id: 7,
      runtime_id: 19,
      config: { transport_target_ref: "example:5:8" },
    });
    apiGet
      .mockResolvedValueOnce({ items: [{ ...validTarget, config: { transport_target_ref: [] } }] })
      .mockResolvedValueOnce({ items: [validTarget] });
    const { result } = renderHook(() => useGatewayResources({ pollIsCurrent: () => true, connectorKinds: ["docker"] }));
    await act(async () => {
      await result.current.loadTargets();
    });
    expect(result.current.liveConsoleTargets).toMatchObject({
      state: "error",
      data: [],
      error: "Invalid Docker console target transport_target_ref.",
    });
    await act(async () => {
      await result.current.loadTargets();
    });
    expect(result.current.liveConsoleTargets).toMatchObject({ state: "ready", data: [{ id: 19, connector_kind: "docker" }], error: null });
  });

  it("aborts a native credential read on unmount without applying its delayed result", async () => {
    const pending = deferred();
    apiGet.mockReturnValueOnce(pending.promise);
    const { result, unmount } = renderHook(() => useGatewayResources({ pollIsCurrent: () => true, connectorKinds: ["ssh"] }));
    let load: Promise<unknown> | undefined;
    act(() => {
      load = result.current.loadCredentials(1);
    });
    const signal = apiGet.mock.calls[0][1]?.signal;
    expect(signal?.aborted).toBe(false);
    unmount();
    expect(signal?.aborted).toBe(true);
    await act(async () => {
      pending.resolve([{ id: 11, name: "Stale", key_type: "ed25519" }]);
      await load;
    });
    expect(result.current.credentials.data).toEqual([]);
  });

  it("keeps a verified target snapshot when a current response is malformed", async () => {
    apiGet.mockResolvedValueOnce({ items: [target(1)] }).mockResolvedValueOnce({ items: [{ id: 2 }] });
    const { result } = renderResources();
    await act(async () => result.current.loadTargets());
    await act(async () => result.current.loadTargets());
    expect(result.current.targets).toEqual({ state: "error", data: [target(1)], error: "Invalid gateway targets response." });
  });

  it("does not replace MCP state with a malformed mutation acknowledgement", async () => {
    apiGet.mockResolvedValueOnce({ enabled: false, start_enabled: false });
    apiPut.mockResolvedValueOnce({ enabled: "true", start_enabled: false });
    const { result } = renderResources();
    await act(async () => result.current.loadMCPRuntime());
    await act(async () => {
      await expect(result.current.setMCPRuntimeEnabled(true)).rejects.toThrow("Invalid MCP runtime");
    });
    expect(result.current.mcpRuntime.data).toEqual({ enabled: false, start_enabled: false });
  });
});
