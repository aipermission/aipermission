import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { filterDockerResources, useDockerBrowser } from "./use-docker-browser";
import { useDockerLifecycle } from "./use-docker-lifecycle";

vi.mock("../_shared/action-runner", () => ({ runGuardedConnectorAction: vi.fn() }));

const containers = [
  { id: "one", name: "api", image: "example/api", state: "running", status: "Up" },
  { id: "two", name: "worker", image: "example/worker", state: "running", status: "Up" },
];

beforeEach(() => {
  runGuardedConnectorAction.mockReset();
  runGuardedConnectorAction.mockImplementation(async ({ actionName, input, onCompleted }) => {
    const output = actionName === "list_containers" ? { containers } : { container: { name: input?.container } };
    const item = { action_name: actionName, output };
    onCompleted?.(item);
    return item;
  });
});

function renderBrowser(overrides = {}) {
  const props = {
    target: { ref: "docker:1:1" },
    approvals: { data: [] },
    session: null,
    selectedSessionLive: false,
    onNewLiveSession: vi.fn(),
    onSelectLiveSessionName: vi.fn(),
    onRefreshActivity: vi.fn(),
    ...overrides,
  };
  return { ...renderHook(() => useDockerBrowser(props)), props };
}

it("loads Docker resources and toggles the selected container", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.visibleCount).toBe(2));

  act(() => result.current.selectResource("containers", containers[0]));
  await waitFor(() => expect(result.current.selectedContainer?.name).toBe("api"));
  expect(runGuardedConnectorAction).toHaveBeenCalledWith(
    expect.objectContaining({ actionName: "container_logs", input: { container: "api", tail: 200 }, channel: "detail" }),
  );

  act(() => result.current.selectResource("containers", containers[0]));
  await waitFor(() => expect(result.current.selectedContainer).toBeNull());
});

it("preserves inspect mode while switching Docker containers", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.visibleCount).toBe(2));
  act(() => result.current.selectResource("containers", containers[0]));
  await waitFor(() => expect(result.current.selectedContainer?.name).toBe("api"));
  await act(async () => result.current.inspectContainer());
  expect(result.current.viewMode).toBe("inspect");

  act(() => result.current.selectResource("containers", containers[1]));
  await waitFor(() => expect(result.current.selectedContainer?.name).toBe("worker"));
  expect(runGuardedConnectorAction).toHaveBeenLastCalledWith(
    expect.objectContaining({ actionName: "inspect_container", input: { container: "worker" }, channel: "detail" }),
  );
});

it("ignores a detail response after the selected container is cleared", async () => {
  let resolveDetail;
  runGuardedConnectorAction.mockImplementation(async ({ actionName, input, onCompleted, requestGuard, channel }) => {
    const output = actionName === "list_containers" ? { containers } : { container: { name: input?.container } };
    const item = { action_name: actionName, output };
    if (channel !== "detail") {
      onCompleted?.(item);
      return item;
    }
    const request = requestGuard.begin(channel);
    await new Promise((resolve) => {
      resolveDetail = resolve;
    });
    if (!request.isCurrent()) return null;
    onCompleted?.(item);
    request.complete();
    return item;
  });

  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.visibleCount).toBe(2));
  act(() => result.current.selectResource("containers", containers[0]));
  await waitFor(() => expect(result.current.selectedContainer?.name).toBe("api"));

  act(() => result.current.selectResource("containers", containers[0]));
  await waitFor(() => expect(result.current.selectedContainer).toBeNull());
  await act(async () => resolveDetail());

  expect(result.current.result).toBeNull();
  expect(result.current.state.state).toBe("idle");
});

it("builds bounded Docker lifecycle payloads and refreshes after completion", async () => {
  const runAction = vi.fn().mockResolvedValue({ action_name: "stop_container" });
  const refreshContainers = vi.fn().mockResolvedValue(undefined);
  const { result } = renderHook(() => useDockerLifecycle({ selectedContainer: containers[0], runAction, refreshContainers }));

  act(() => result.current.openLifecycle("stop_container"));
  expect(result.current.confirmDialog.title).toBe("Stop Docker container");
  await act(async () => result.current.confirmLifecycle());

  expect(runAction).toHaveBeenCalledWith(
    expect.objectContaining({ actionName: "stop_container", input: { container: "api", timeout_seconds: 10 }, channel: "lifecycle" }),
  );
  expect(refreshContainers).toHaveBeenCalledOnce();
  expect(result.current.confirmDialog.open).toBe(false);
});

it("filters Docker resources through connector-owned searchable fields", () => {
  expect(filterDockerResources("containers", containers, "WORK")).toEqual([containers[1]]);
  expect(filterDockerResources("containers", containers, "  ")).toBe(containers);
});
