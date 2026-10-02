import { connectorActionRequest } from "../../../test/connector-action-fixtures";
import { act, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api.ts";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import { completed, pods, browserRenderer, resetBrowserAPI, responseFor } from "../../../test/kubernetes-browser-fixtures";
import { useKubernetesBrowser } from "./use-kubernetes-browser";

vi.mock("../../../lib/api.ts", () => ({ apiPost: vi.fn() }));

beforeEach(resetBrowserAPI);
const renderBrowser = browserRenderer(useKubernetesBrowser);

it.each(["completed", "rejected", "failed", "approval_pending", "running"] as const)(
  "hides old namespace rows immediately and after a %s list response",
  async (outcome) => {
    const { result, props } = renderBrowser({
      selectedSessionLive: true,
      session: { name: "kubernetes:kubernetes:1:1:default:api-a", active: true },
    });
    act(() => result.current.switchTab("pods"));
    await waitFor(() => expect(result.current.activeResources).toEqual(pods));
    await act(async () => result.current.selectResource(pods[0]));
    act(() => result.current.openPodConsole());
    expect(result.current.selectedPodConsoleLive).toBe(true);
    let resolveList!: (_value: ConnectorActionResponse) => void;
    let rejectList!: (_error: Error) => void;
    vi.mocked(apiPost).mockImplementationOnce(
      () =>
        new Promise((resolve, reject) => {
          resolveList = resolve;
          rejectList = reject;
        }),
    );

    act(() => result.current.changeNamespace("staging"));

    expect(result.current.namespace).toBe("staging");
    expect(result.current.activeResources).toEqual([]);
    expect(result.current.filteredResources).toEqual([]);
    expect(result.current.selectedResource).toBeNull();
    expect(result.current.selectedKey).toBe("");
    expect(result.current.detail).toBeNull();
    expect(result.current.logs).toBe("");
    expect(result.current.selectedPodConsoleLive).toBe(false);
    expect(connectorActionRequest(vi.mocked(apiPost).mock.lastCall![1]).input).toEqual({ namespace: "staging" });
    vi.mocked(apiPost).mockClear();
    vi.mocked(props.onSelectLiveSessionName!).mockClear();
    await act(async () => {
      await result.current.readLogs();
      result.current.openPodConsole();
      await result.current.startPodConsole();
    });
    expect(apiPost).not.toHaveBeenCalled();
    expect(props.onNewLiveSession).not.toHaveBeenCalled();
    expect(props.onSelectLiveSessionName).not.toHaveBeenCalled();
    const stagingPods = [{ ...pods[0], namespace: "staging" }];
    await act(async () => {
      if (outcome === "rejected") rejectList(new Error("namespace list denied"));
      else resolveList({ ...completed("list_pods", { pods: stagingPods }), status: outcome });
    });
    expect(result.current.activeResources).toEqual(outcome === "completed" ? stagingPods : []);
    expect(result.current.selectedResource).toBeNull();
    if (outcome === "rejected" || outcome === "failed") expect(result.current.state.state).toBe("error");
    if (outcome === "approval_pending" || outcome === "running") expect(result.current.state.message).toContain("awaiting approval");
  },
);

it.each(["workloads", "services", "ingress", "events"] as const)(
  "owns the %s snapshot by namespace and replaces it without accumulating namespace caches",
  async (tab) => {
    const production = { namespace: "production", kind: "Deployment", name: "api" };
    const staging = { ...production, namespace: "staging" };
    vi.mocked(apiPost).mockImplementation((_path, payload: Record<string, unknown>) => {
      const request = connectorActionRequest(payload);
      return Promise.resolve(completed(request.action_name, { [tab]: [request.input.namespace === "staging" ? staging : production] }));
    });
    const { result } = renderBrowser();
    act(() => result.current.switchTab(tab));
    act(() => result.current.changeNamespace("production"));
    await waitFor(() => expect(result.current.activeResources).toEqual([production]));
    let resolveList!: (_value: ConnectorActionResponse) => void;
    const delayList = () =>
      new Promise<ConnectorActionResponse>((resolve) => {
        resolveList = resolve;
      });
    vi.mocked(apiPost).mockImplementationOnce(delayList);
    act(() => result.current.changeNamespace("staging"));
    expect(result.current.activeResources).toEqual([]);
    await act(async () => resolveList(completed(`list_${tab}`, { [tab]: [staging] })));
    expect(result.current.activeResources).toEqual([staging]);

    vi.mocked(apiPost).mockImplementationOnce(delayList);
    act(() => result.current.changeNamespace("staging"));
    expect(result.current.activeResources).toEqual([staging]);
    await act(async () => resolveList({ ...completed(`list_${tab}`, {}), status: "approval_pending" }));
    expect(result.current.activeResources).toEqual([staging]);

    vi.mocked(apiPost).mockImplementationOnce(delayList);
    act(() => result.current.changeNamespace("production"));
    expect(result.current.activeResources).toEqual([]);
    await act(async () => resolveList(completed(`list_${tab}`, { [tab]: [production] })));
    expect(result.current.activeResources).toEqual([production]);
  },
);

it("does not reuse old pods when namespace changes on the workloads tab", async () => {
  const { result } = renderBrowser();
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.activeResources).toEqual(pods));
  act(() => result.current.switchTab("workloads"));
  await waitFor(() => expect(result.current.state.state).toBe("idle"));
  act(() => result.current.changeNamespace("staging"));
  await waitFor(() => expect(result.current.state.state).toBe("idle"));
  let resolvePods!: (_value: ConnectorActionResponse) => void;
  vi.mocked(apiPost).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolvePods = resolve;
      }),
  );

  act(() => result.current.switchTab("pods"));

  expect(result.current.activeResources).toEqual([]);
  expect(result.current.filteredResources).toEqual([]);
  expect(connectorActionRequest(vi.mocked(apiPost).mock.lastCall![1]).input).toEqual({ namespace: "staging" });
  const stagingPods = [{ ...pods[1], namespace: "staging" }];
  await act(async () => resolvePods(completed("list_pods", { pods: stagingPods })));
  expect(result.current.activeResources).toEqual(stagingPods);
});

it("keeps namespace scope ABA completions from overwriting current rows or selection", async () => {
  const { result } = renderBrowser();
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.activeResources).toEqual(pods));
  const pending: { namespace: unknown; resolve: (_value: ConnectorActionResponse) => void; signal?: AbortSignal }[] = [];
  vi.mocked(apiPost).mockImplementation((_path, payload: Record<string, unknown>, options) => {
    const request = connectorActionRequest(payload);
    if (request.action_name === "list_pods")
      return new Promise((resolve) => {
        pending.push({ namespace: request.input.namespace, resolve, signal: options?.signal });
      });
    return Promise.resolve(completed(request.action_name, responseFor(request.action_name, request.input)));
  });
  act(() => result.current.changeNamespace("default"));
  act(() => result.current.changeNamespace("staging"));
  act(() => result.current.changeNamespace("default"));
  expect(pending.map((request) => request.namespace)).toEqual(["default", "staging", "default"]);
  expect(pending[0].signal?.aborted).toBe(true);
  expect(pending[1].signal?.aborted).toBe(true);
  await act(async () => pending[2].resolve(completed("list_pods", { pods })));
  await act(async () => result.current.selectResource(pods[1]));
  const detail = result.current.detail;
  await act(async () => {
    pending[0].resolve(completed("list_pods", { pods: [{ ...pods[0], name: "obsolete-a" }] }));
    pending[1].resolve(completed("list_pods", { pods: [{ ...pods[0], namespace: "staging" }] }));
  });
  expect(result.current.namespace).toBe("default");
  expect(result.current.activeResources).toEqual(pods);
  expect(result.current.selectedResource).toEqual(pods[1]);
  expect(result.current.detail).toBe(detail);
  expect(result.current.logs).toBe("logs for api-b");
});

it.each(["describe_resource", "get_logs"])("discards delayed %s after a namespace change", async (actionName) => {
  let resolveRead!: (_value: ConnectorActionResponse) => void;
  vi.mocked(apiPost).mockImplementation((_path, payload: Record<string, unknown>) => {
    const request = connectorActionRequest(payload);
    if (request.action_name === actionName)
      return new Promise((resolve) => {
        resolveRead = resolve;
      });
    const output =
      request.action_name === "list_pods" && request.input.namespace === "staging"
        ? { pods: [] }
        : responseFor(request.action_name, request.input);
    return Promise.resolve(completed(request.action_name, output));
  });
  const { result } = renderBrowser();
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.activeResources).toEqual(pods));
  act(() => void result.current.selectResource(pods[0]));
  await waitFor(() => expect(resolveRead).toBeTypeOf("function"));
  act(() => result.current.changeNamespace("staging"));
  await act(async () => resolveRead(completed(actionName, { resource: pods[0], logs: "obsolete logs" })));

  expect(result.current.activeResources).toEqual([]);
  expect(result.current.selectedResource).toBeNull();
  expect(result.current.detail).toBeNull();
  expect(result.current.logs).toBe("");
  expect(result.current.state.state).toBe("idle");
  if (actionName === "describe_resource")
    expect(vi.mocked(apiPost).mock.calls.some(([, payload]) => connectorActionRequest(payload).action_name === "get_logs")).toBe(false);
});

it("does not restore an obsolete cache when the target scope returns from A to B to A", async () => {
  const pending: { targetRef: string; resolve: (_value: ConnectorActionResponse) => void }[] = [];
  vi.mocked(apiPost).mockImplementation((_path, payload: Record<string, unknown>) => {
    const request = connectorActionRequest(payload);
    if (request.action_name === "list_workloads")
      return new Promise((resolve) => {
        pending.push({ targetRef: request.target_ref, resolve });
      });
    return Promise.resolve(completed(request.action_name, responseFor(request.action_name, request.input), request.target_ref));
  });
  const { result, rerender, props } = renderBrowser();
  rerender({ ...props, target: { ref: "kubernetes:2:2" } });
  rerender(props);
  expect(pending.map((request) => request.targetRef)).toEqual(["kubernetes:1:1", "kubernetes:2:2", "kubernetes:1:1"]);
  const current = { namespace: "default", kind: "Deployment", name: "current" };
  await act(async () => pending[2].resolve(completed("list_workloads", { workloads: [current] })));
  await act(async () => result.current.selectResource(current));
  const detail = result.current.detail;
  await act(async () => {
    pending[0].resolve(completed("list_workloads", { workloads: [{ ...current, name: "obsolete-a" }] }));
    pending[1].resolve(completed("list_workloads", { workloads: [{ ...current, name: "obsolete-b" }] }, "kubernetes:2:2"));
  });
  expect(result.current.activeResources).toEqual([current]);
  expect(result.current.selectedResource).toEqual(current);
  expect(result.current.detail).toBe(detail);
});

it("reuses cluster-scoped nodes across namespace changes and tab switches", async () => {
  const nodes = [{ name: "worker-1", ready: "True" }];
  vi.mocked(apiPost).mockImplementation((_path, payload: Record<string, unknown>) => {
    const request = connectorActionRequest(payload);
    return Promise.resolve(
      completed(
        request.action_name,
        request.action_name === "list_nodes" ? { nodes } : responseFor(request.action_name, request.input),
        request.target_ref,
      ),
    );
  });
  const { result, rerender, props } = renderBrowser();
  act(() => result.current.switchTab("nodes"));
  await waitFor(() => expect(result.current.activeResources).toEqual(nodes));
  await act(async () => result.current.selectResource(nodes[0]));
  expect(connectorActionRequest(vi.mocked(apiPost).mock.lastCall![1]).input).toEqual({ resource_type: "node", name: "worker-1" });
  let resolveNodes!: (_value: ConnectorActionResponse) => void;
  vi.mocked(apiPost).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveNodes = resolve;
      }),
  );
  act(() => result.current.changeNamespace("staging"));
  expect(result.current.activeResources).toEqual(nodes);
  expect(result.current.selectedResource).toBeNull();
  expect(connectorActionRequest(vi.mocked(apiPost).mock.lastCall![1]).input).toEqual({});
  await act(async () => resolveNodes({ ...completed("list_nodes", {}), status: "approval_pending" }));
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.state.state).toBe("idle"));
  vi.mocked(apiPost).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveNodes = resolve;
      }),
  );
  act(() => result.current.switchTab("nodes"));
  expect(result.current.activeResources).toEqual(nodes);
  expect(connectorActionRequest(vi.mocked(apiPost).mock.lastCall![1]).input).toEqual({});
  await act(async () => resolveNodes(completed("list_nodes", { nodes })));
  expect(result.current.activeResources).toEqual(nodes);

  rerender({ ...props, target: { ref: "kubernetes:2:2" } });
  await waitFor(() => expect(result.current.state.state).toBe("idle"));
  expect(result.current.namespace).toBe("");
  vi.mocked(apiPost).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveNodes = resolve;
      }),
  );
  act(() => result.current.switchTab("nodes"));
  expect(result.current.activeResources).toEqual([]);
  const newNodes = [{ name: "worker-2", ready: "True" }];
  await act(async () => resolveNodes(completed("list_nodes", { nodes: newNodes }, "kubernetes:2:2")));
  expect(result.current.activeResources).toEqual(newNodes);
});
