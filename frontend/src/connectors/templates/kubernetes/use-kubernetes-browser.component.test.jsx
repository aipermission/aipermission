import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { useKubernetesBrowser } from "./use-kubernetes-browser";
import { useRolloutRestart } from "./use-rollout-restart";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));

const pods = [
  { namespace: "default", name: "api-a", node: "worker-1", phase: "Running" },
  { namespace: "default", name: "api-b", node: "worker-2", phase: "Running" },
];

beforeEach(() => {
  apiPost.mockReset();
  apiPost.mockImplementation(async (_path, payload) => completed(payload.action_name, responseFor(payload.action_name, payload.input)));
});

function renderBrowser(overrides = {}) {
  const props = {
    target: { ref: "kubernetes:1:1" },
    approvals: { data: [] },
    session: { name: "", active: false },
    selectedSessionLive: false,
    onNewLiveSession: vi.fn(),
    onSelectLiveSessionName: vi.fn(),
    onRefreshActivity: vi.fn(),
    ...overrides,
  };
  return { ...renderHook((next) => useKubernetesBrowser(next), { initialProps: props }), props };
}

it("loads resources and ignores detail from a superseded pod selection", async () => {
  const pending = new Map();
  apiPost.mockImplementation((_path, payload) => {
    if (payload.action_name !== "describe_resource") {
      return Promise.resolve(completed(payload.action_name, responseFor(payload.action_name, payload.input)));
    }
    return new Promise((resolve) => pending.set(payload.input.name, resolve));
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.activeResources).toEqual([]));
  act(() => result.current.switchTab("pods"));
  await waitFor(() => expect(result.current.activeResources).toEqual(pods));

  act(() => void result.current.selectResource(pods[0]));
  await waitFor(() => expect(pending.has("api-a")).toBe(true));
  act(() => void result.current.selectResource(pods[1]));
  await waitFor(() => expect(pending.has("api-b")).toBe(true));

  await act(async () => pending.get("api-a")(completed("describe_resource", { resource: pods[0] })));
  expect(result.current.detail).toBeNull();
  await act(async () => pending.get("api-b")(completed("describe_resource", { resource: pods[1] })));
  await waitFor(() => expect(result.current.detail?.output?.resource?.name).toBe("api-b"));
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
  const pending = new Map();
  apiPost.mockImplementation((_path, payload) => {
    if (payload.action_name !== "list_workloads") {
      return Promise.resolve(completed(payload.action_name, responseFor(payload.action_name, payload.input)));
    }
    return new Promise((resolve) => pending.set(payload.target_ref, resolve));
  });
  const { result, rerender, props } = renderBrowser();
  await waitFor(() => expect(pending.has("kubernetes:1:1")).toBe(true));
  rerender({ ...props, target: { ref: "kubernetes:2:2" } });
  await waitFor(() => expect(pending.has("kubernetes:2:2")).toBe(true));

  await act(async () =>
    pending.get("kubernetes:1:1")(completed("list_workloads", { workloads: [{ kind: "Deployment", namespace: "old", name: "stale" }] })),
  );
  expect(result.current.activeResources).toEqual([]);
  await act(async () => pending.get("kubernetes:2:2")(completed("list_workloads", { workloads: [] })));
  expect(result.current.activeResources).toEqual([]);
});

it("restarts the workload captured by the confirmation dialog", async () => {
  const first = { kind: "Deployment", namespace: "default", name: "api-a" };
  const second = { kind: "Deployment", namespace: "default", name: "api-b" };
  const runAction = vi.fn().mockResolvedValue({ id: 1 });
  const refreshResource = vi.fn().mockResolvedValue(undefined);
  const { result, rerender } = renderHook((props) => useRolloutRestart(props), {
    initialProps: { targetRef: "kubernetes:1:1", tab: "workloads", selectedResource: first, runAction, refreshResource },
  });
  act(() => result.current.open());
  rerender({ targetRef: "kubernetes:1:1", tab: "workloads", selectedResource: second, runAction, refreshResource });

  await act(async () => result.current.confirm());

  expect(runAction).toHaveBeenCalledWith(
    expect.objectContaining({ input: { namespace: "default", deployment: "api-a" }, actionName: "rollout_restart" }),
  );
  expect(refreshResource).toHaveBeenCalledWith("workloads");
});

function completed(actionName, output) {
  return { id: 1, status: "completed", action_name: actionName, output };
}

function responseFor(actionName, input) {
  const outputs = {
    list_namespaces: { namespaces: [{ name: "default" }] },
    list_workloads: { workloads: [] },
    list_pods: { pods },
    get_logs: { logs: `logs for ${input.pod}` },
  };
  return outputs[actionName] || {};
}
