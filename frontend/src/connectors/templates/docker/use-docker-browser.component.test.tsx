import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { filterDockerResources, useDockerBrowser } from "./use-docker-browser";
import { useDockerLifecycle } from "./use-docker-lifecycle";
import type { DockerBrowserProps } from "./use-docker-browser";
import { connectorActionFixture } from "../../../test/connector-action-fixtures";

vi.mock("../_shared/action-runner", () => ({ runGuardedConnectorAction: vi.fn() }));

const containers = [
  { id: "one", name: "api", image: "example/api", state: "running", status: "Up" },
  { id: "two", name: "worker", image: "example/worker", state: "running", status: "Up" },
];

beforeEach(() => {
  vi.mocked(runGuardedConnectorAction).mockReset();
  vi.mocked(runGuardedConnectorAction).mockImplementation(async ({ actionName, input, onCompleted, targetRef }) => {
    const output =
      actionName === "list_containers"
        ? { containers }
        : actionName === "list_images"
          ? { images: [{ id: "image-1", repository: "example/api", tag: "latest" }] }
          : { container: { name: input?.container } };
    const item = connectorActionFixture({ action_name: actionName, target_ref: targetRef, connector_kind: "docker", output });
    onCompleted?.(item);
    return item;
  });
});

function renderBrowser(overrides: Partial<DockerBrowserProps> = {}) {
  const props: DockerBrowserProps = {
    target: { ref: "docker:1:1" },
    approvals: { data: [] },
    session: null,
    selectedSessionLive: false,
    onNewLiveSession: vi.fn(),
    onSelectLiveSessionName: vi.fn(),
    onRefreshActivity: vi.fn(),
    ...overrides,
  };
  return { ...renderHook((next) => useDockerBrowser(next), { initialProps: props }), props };
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
  let resolveDetail: (() => void) | undefined;
  vi.mocked(runGuardedConnectorAction).mockImplementation(async ({ actionName, input, onCompleted, requestGuard, channel, targetRef }) => {
    const output = actionName === "list_containers" ? { containers } : { container: { name: input?.container } };
    const item = connectorActionFixture({ action_name: actionName, target_ref: targetRef, connector_kind: "docker", output });
    if (channel !== "detail") {
      onCompleted?.(item);
      return item;
    }
    const request = requestGuard.begin(channel);
    await new Promise<void>((resolve) => {
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
  await act(async () => resolveDetail?.());

  expect(result.current.result).toBeNull();
  expect(result.current.state.state).toBe("idle");
});

it("builds bounded Docker lifecycle payloads and refreshes after completion", async () => {
  const runAction = vi
    .fn()
    .mockResolvedValue(connectorActionFixture({ action_name: "stop_container", connector_kind: "docker", target_ref: "docker:1:1" }));
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

it("browses non-container resources and starts a named container console", async () => {
  const onNewLiveSession = vi.fn().mockResolvedValue(undefined);
  const onSelectLiveSessionName = vi.fn();
  const { result } = renderBrowser({ onNewLiveSession, onSelectLiveSessionName });
  await waitFor(() => expect(result.current.visibleCount).toBe(2));

  act(() => result.current.switchResourceView("images"));
  await waitFor(() => expect(result.current.visibleCount).toBe(1));
  act(() => result.current.selectResource("images", result.current.filteredItems[0]));
  expect(result.current.selectedResourceID).toBeTruthy();

  act(() => result.current.switchResourceView("containers"));
  act(() => result.current.selectResource("containers", containers[0]));
  await waitFor(() => expect(result.current.selectedContainer?.name).toBe("api"));
  act(() => result.current.openContainerConsole());
  expect(result.current.viewMode).toBe("console");
  await act(async () => result.current.startContainerConsole());
  expect(onSelectLiveSessionName).toHaveBeenCalled();
  expect(onNewLiveSession).toHaveBeenCalledWith(expect.objectContaining({ params: { container: "api" }, closeExisting: false }));
});

it("keeps a network selection when an older image list finishes", async () => {
  let finishImages: (() => void) | undefined;
  const original = vi.mocked(runGuardedConnectorAction).getMockImplementation()!;
  vi.mocked(runGuardedConnectorAction).mockImplementation(async (options) => {
    if (options.actionName === "list_images") {
      await new Promise<void>((resolve) => {
        finishImages = resolve;
      });
    }
    if (options.actionName === "list_networks") {
      return connectorActionFixture({
        action_name: options.actionName,
        connector_kind: "docker",
        target_ref: options.targetRef,
        output: { networks: [{ id: "network-1", name: "application" }] },
      });
    }
    return original(options);
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.visibleCount).toBe(2));
  act(() => result.current.switchResourceView("images"));
  await waitFor(() => expect(finishImages).toBeTypeOf("function"));
  act(() => result.current.switchResourceView("networks"));
  await waitFor(() => expect(result.current.visibleCount).toBe(1));
  act(() => result.current.selectResource("networks", result.current.filteredItems[0]));
  const selected = result.current.selectedResourceID;
  await act(async () => finishImages?.());
  expect(result.current.selectedResourceID).toBe(selected);
  expect(result.current.selectedResource?.name).toBe("application");
});

it("does not clear a new console pending state when an older start fails", async () => {
  let rejectFirst: ((_error: Error) => void) | undefined;
  let finishSecond: (() => void) | undefined;
  const onNewLiveSession = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise<void>((_resolve, reject) => {
          rejectFirst = reject;
        }),
    )
    .mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          finishSecond = resolve;
        }),
    );
  const { result } = renderBrowser({ onNewLiveSession });
  await waitFor(() => expect(result.current.visibleCount).toBe(2));
  act(() => result.current.selectResource("containers", containers[0]));
  let first: Promise<void> | undefined;
  act(() => {
    first = result.current.startContainerConsole();
  });
  act(() => result.current.selectResource("containers", containers[1]));
  let second: Promise<void> | undefined;
  act(() => {
    second = result.current.startContainerConsole();
  });
  expect(result.current.consolePending).toBe(true);
  await act(async () => {
    rejectFirst?.(new Error("old start failed"));
    await first;
  });
  expect(result.current.consolePending).toBe(true);
  await act(async () => {
    finishSecond?.();
    await second;
  });
});

it.each(["null", "error", "success"])("does not update a replacement lifecycle dialog after an older %s response", async (outcome) => {
  let finish: (() => void) | undefined;
  const runAction = vi.fn(async () => {
    await new Promise<void>((resolve) => {
      finish = resolve;
    });
    if (outcome === "error") throw new Error("old lifecycle failure");
    if (outcome === "success")
      return connectorActionFixture({ action_name: "stop_container", connector_kind: "docker", target_ref: "docker:1:1" });
    return null;
  });
  const refreshContainers = vi.fn().mockResolvedValue(undefined);
  const { result } = renderHook(() => useDockerLifecycle({ selectedContainer: containers[0], runAction, refreshContainers }));
  act(() => result.current.openLifecycle("stop_container"));
  let old: Promise<void> | undefined;
  act(() => {
    old = result.current.confirmLifecycle();
  });
  const finishOld = finish;
  act(() => result.current.openLifecycle("restart_container"));
  let replacement: Promise<void> | undefined;
  act(() => {
    replacement = result.current.confirmLifecycle();
  });
  const finishReplacement = finish;
  expect(result.current.confirmDialog.pending).toBe(true);
  await act(async () => {
    finishOld?.();
    await old;
  });
  expect(result.current.confirmDialog.pending).toBe(true);
  expect(result.current.confirmDialog.actionName).toBe("restart_container");
  expect(refreshContainers).not.toHaveBeenCalled();
  await act(async () => {
    finishReplacement?.();
    await replacement;
  });
});

it("executes the container shown in the confirmation and rejects duplicate submits", async () => {
  let finish: (() => void) | undefined;
  const runAction = vi.fn(async () => {
    await new Promise<void>((resolve) => {
      finish = resolve;
    });
    return null;
  });
  const refreshContainers = vi.fn().mockResolvedValue(undefined);
  const { result, rerender } = renderHook(
    ({ selectedContainer }) => useDockerLifecycle({ selectedContainer, runAction, refreshContainers }),
    { initialProps: { selectedContainer: containers[0] } },
  );
  act(() => result.current.openLifecycle("stop_container"));
  rerender({ selectedContainer: containers[1] });
  let request: Promise<void> | undefined;
  act(() => {
    request = result.current.confirmLifecycle();
  });
  await act(async () => result.current.confirmLifecycle());
  expect(runAction).toHaveBeenCalledOnce();
  expect(runAction).toHaveBeenCalledWith(expect.objectContaining({ input: { container: "api", timeout_seconds: 10 } }));
  await act(async () => {
    finish?.();
    await request;
  });
  expect(result.current.confirmDialog.pending).toBe(false);
  let retry: Promise<void> | undefined;
  act(() => {
    retry = result.current.confirmLifecycle();
  });
  expect(runAction).toHaveBeenCalledTimes(2);
  await act(async () => {
    finish?.();
    await retry;
  });
});

it.each(["target", "unmount"])("ignores a console failure after %s disposal", async (disposal) => {
  let rejectStart: ((_error: Error) => void) | undefined;
  const onNewLiveSession = vi.fn(
    () =>
      new Promise<void>((_resolve, reject) => {
        rejectStart = reject;
      }),
  );
  const { result, rerender, unmount, props } = renderBrowser({ onNewLiveSession });
  await waitFor(() => expect(result.current.visibleCount).toBe(2));
  act(() => result.current.selectResource("containers", containers[0]));
  let request: Promise<void> | undefined;
  act(() => {
    request = result.current.startContainerConsole();
  });
  if (disposal === "target") rerender({ ...props, target: { ref: "docker:2:2" } });
  else unmount();
  await act(async () => {
    rejectStart?.(new Error("disposed start"));
    await request;
  });
  if (disposal === "target") expect(result.current.consolePending).toBe(false);
});
