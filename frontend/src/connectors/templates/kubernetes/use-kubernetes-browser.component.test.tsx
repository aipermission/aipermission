import { connectorActionRequest } from "../../../test/connector-action-fixtures";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api.ts";
import { useRolloutRestart } from "./use-rollout-restart";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import { kubernetesOutputField } from "./resource-output";
import { completed, pods, browserRenderer, resetBrowserAPI, responseFor } from "../../../test/kubernetes-browser-fixtures";
import { useKubernetesBrowser } from "./use-kubernetes-browser";

vi.mock("../../../lib/api.ts", () => ({ apiPost: vi.fn() }));

beforeEach(resetBrowserAPI);
const renderBrowser = browserRenderer(useKubernetesBrowser);

it("loads resources and ignores detail from a superseded pod selection", async () => {
  const pending = new Map<string, (_value: ConnectorActionResponse) => void>();
  vi.mocked(apiPost).mockImplementation((_path, payload: Record<string, unknown>) => {
    if (connectorActionRequest(payload).action_name !== "describe_resource") {
      return Promise.resolve(
        completed(
          connectorActionRequest(payload).action_name,
          responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
        ),
      );
    }
    return new Promise((resolve) => pending.set(String(connectorActionRequest(payload).input.name), resolve));
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.activeResources).toEqual([]));
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.activeResources).toEqual(pods));

  act(() => void result.current.selectResource(pods[0]));
  await waitFor(() => expect(pending.has("api-a")).toBe(true));
  act(() => void result.current.selectResource(pods[1]));
  await waitFor(() => expect(pending.has("api-b")).toBe(true));

  await act(async () => pending.get("api-a")?.(completed("describe_resource", { resource: pods[0] })));
  expect(result.current.detail).toBeNull();
  await act(async () => pending.get("api-b")?.(completed("describe_resource", { resource: pods[1] })));
  await waitFor(() => expect(kubernetesOutputField(result.current.detail?.output, "resource")).toEqual(pods[1]));
  expect(result.current.logs).toBe("logs for api-b");
});

it("keeps pod console identity and params bound to the selected pod", async () => {
  const onNewLiveSession = vi.fn().mockResolvedValue(undefined);
  const onSelectLiveSessionName = vi.fn();
  const { result } = renderBrowser({ onNewLiveSession, onSelectLiveSessionName });
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.activeResources).toEqual(pods));
  await act(async () => result.current.selectResource(pods[0]));

  await act(async () => result.current.startPodConsole());

  expect(onSelectLiveSessionName).toHaveBeenLastCalledWith("kubernetes:kubernetes:1:1:default:api-a");
  expect(onNewLiveSession).toHaveBeenCalledWith({
    name: "kubernetes:kubernetes:1:1:default:api-a",
    params: { namespace: "default", pod: "api-a" },
    closeExisting: false,
  });
});

it("discards resource lists that arrive after the connector target changes", async () => {
  const pending = new Map<string, (_value: ConnectorActionResponse) => void>();
  vi.mocked(apiPost).mockImplementation((_path, payload: Record<string, unknown>) => {
    if (connectorActionRequest(payload).action_name !== "list_workloads") {
      return Promise.resolve(
        completed(
          connectorActionRequest(payload).action_name,
          responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
        ),
      );
    }
    return new Promise((resolve) => pending.set(connectorActionRequest(payload).target_ref, resolve));
  });
  const { result, rerender, props } = renderBrowser();
  await waitFor(() => expect(pending.has("kubernetes:1:1")).toBe(true));
  rerender({ ...props, target: { ref: "kubernetes:2:2" } });
  await waitFor(() => expect(pending.has("kubernetes:2:2")).toBe(true));

  await act(async () =>
    pending.get("kubernetes:1:1")?.(completed("list_workloads", { workloads: [{ kind: "Deployment", namespace: "old", name: "stale" }] })),
  );
  expect(result.current.activeResources).toEqual([]);
  await act(async () => pending.get("kubernetes:2:2")?.(completed("list_workloads", { workloads: [] }, "kubernetes:2:2")));
  expect(result.current.activeResources).toEqual([]);
});

it("does not let an older tab list clear the current pod selection", async () => {
  let resolveWorkloads: ((_value: ConnectorActionResponse) => void) | undefined;
  vi.mocked(apiPost).mockImplementation((_path, payload: Record<string, unknown>) => {
    if (connectorActionRequest(payload).action_name === "list_workloads") {
      return new Promise((resolve) => {
        resolveWorkloads = resolve;
      });
    }
    return Promise.resolve(
      completed(
        connectorActionRequest(payload).action_name,
        responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
      ),
    );
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(resolveWorkloads).toBeTypeOf("function"));
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.activeResources).toEqual(pods));
  await act(async () => result.current.selectResource(pods[0]));

  await act(async () => resolveWorkloads?.(completed("list_workloads", { workloads: [] })));

  expect(result.current.tab).toBe("pods");
  expect(result.current.selectedResource).toEqual(pods[0]);
});

it("does not let old pod detail replace a synchronously selected event", async () => {
  let resolvePodDetail: ((_value: ConnectorActionResponse) => void) | undefined;
  const event = { namespace: "default", object: "pod/api-b", reason: "Scheduled", last_timestamp: "now", message: "placed" };
  vi.mocked(apiPost).mockImplementation((_path, payload: Record<string, unknown>) => {
    if (
      connectorActionRequest(payload).action_name === "describe_resource" &&
      connectorActionRequest(payload).input.resource_type === "pod"
    ) {
      return new Promise((resolve) => {
        resolvePodDetail = resolve;
      });
    }
    if (connectorActionRequest(payload).action_name === "list_events")
      return Promise.resolve(completed("list_events", { events: [event] }));
    return Promise.resolve(
      completed(
        connectorActionRequest(payload).action_name,
        responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
      ),
    );
  });
  const { result } = renderBrowser();
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.activeResources).toEqual(pods));
  act(() => void result.current.selectResource(pods[0]));
  await waitFor(() => expect(resolvePodDetail).toBeTypeOf("function"));
  act(() => result.current.switchTab("events"));
  await waitFor(() => expect(result.current.activeResources).toEqual([event]));
  await act(async () => result.current.selectResource(event));

  await act(async () => resolvePodDetail?.(completed("describe_resource", { resource: pods[0] })));

  expect(result.current.tab).toBe("events");
  expect(kubernetesOutputField(result.current.detail?.output, "resource")).toEqual(event);
});

it("restarts the workload captured by the confirmation dialog", async () => {
  const first = { kind: "Deployment", namespace: "default", name: "api-a" };
  const second = { kind: "Deployment", namespace: "default", name: "api-b" };
  const runAction = vi.fn().mockResolvedValue(completed("rollout_restart", {}));
  const refreshResource = vi.fn().mockResolvedValue(undefined);
  const { result, rerender } = renderHook((props) => useRolloutRestart(props), {
    initialProps: { targetRef: "kubernetes:1:1", tab: "workloads" as const, selectedResource: first, runAction, refreshResource },
  });
  act(() => result.current.open());
  rerender({ targetRef: "kubernetes:1:1", tab: "workloads", selectedResource: second, runAction, refreshResource });

  await act(async () => result.current.confirm());

  expect(runAction).toHaveBeenCalledWith(
    expect.objectContaining({ input: { namespace: "default", deployment: "api-a" }, actionName: "rollout_restart" }),
  );
  expect(refreshResource).toHaveBeenCalledWith("workloads");
});

it("does not replace an opened console with delayed pod logs", async () => {
  let resolveLogs: ((_value: ConnectorActionResponse) => void) | undefined;
  vi.mocked(apiPost).mockImplementation((_path, payload: Record<string, unknown>) => {
    if (connectorActionRequest(payload).action_name === "get_logs")
      return new Promise((resolve) => {
        resolveLogs = resolve;
      });
    return Promise.resolve(
      completed(
        connectorActionRequest(payload).action_name,
        responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
      ),
    );
  });
  const { result } = renderBrowser();
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.activeResources).toEqual(pods));
  act(() => void result.current.selectResource(pods[0]));
  await waitFor(() => expect(resolveLogs).toBeTypeOf("function"));
  act(() => result.current.openPodConsole());
  await act(async () => resolveLogs?.(completed("get_logs", { logs: "old" })));
  expect(result.current.viewMode).toBe("console");
  expect(result.current.logs).toBe("");
  expect(result.current.state.state).toBe("idle");
});

it("keeps a newer pod console pending when an older start rejects", async () => {
  const pending = new Map<string, { resolve: () => void; reject: (_error: Error) => void }>();
  const onNewLiveSession = vi.fn(
    (_options: { name: string }) =>
      new Promise<void>((resolve, reject) => {
        pending.set(_options.name, { resolve, reject });
      }),
  );
  const { result } = renderBrowser({ onNewLiveSession });
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.activeResources).toEqual(pods));
  await act(async () => result.current.selectResource(pods[0]));
  let first: Promise<void> | undefined;
  act(() => {
    first = result.current.startPodConsole();
  });
  await act(async () => result.current.selectResource(pods[1]));
  let second: Promise<void> | undefined;
  act(() => {
    second = result.current.startPodConsole();
  });
  expect(result.current.consolePending).toBe(true);
  await act(async () => {
    pending.get("kubernetes:kubernetes:1:1:default:api-a")?.reject(new Error("old start failed"));
    await first;
  });
  expect(result.current.consolePending).toBe(true);
  await act(async () => {
    pending.get("kubernetes:kubernetes:1:1:default:api-b")?.resolve();
    await second;
  });
});

it("does not carry the previous target namespace into initial workload refresh", async () => {
  const { result, rerender, props } = renderBrowser();
  await waitFor(() => expect(result.current.namespaces).toEqual([{ name: "default" }]));
  act(() => result.current.changeNamespace("default"));
  await waitFor(() =>
    expect(
      vi
        .mocked(apiPost)
        .mock.calls.some(
          ([, payload]) =>
            connectorActionRequest(payload).target_ref === "kubernetes:1:1" &&
            connectorActionRequest(payload).input.namespace === "default",
        ),
    ).toBe(true),
  );
  rerender({ ...props, target: { ref: "kubernetes:2:2" } });
  await waitFor(() =>
    expect(
      vi
        .mocked(apiPost)
        .mock.calls.some(
          ([, payload]) =>
            connectorActionRequest(payload).target_ref === "kubernetes:2:2" &&
            connectorActionRequest(payload).action_name === "list_workloads",
        ),
    ).toBe(true),
  );
  const newRequests = vi
    .mocked(apiPost)
    .mock.calls.filter(
      ([, payload]) =>
        connectorActionRequest(payload).target_ref === "kubernetes:2:2" && connectorActionRequest(payload).action_name === "list_workloads",
    );
  expect(newRequests).toHaveLength(1);
  expect(newRequests[0][1].input).toEqual({});
});
