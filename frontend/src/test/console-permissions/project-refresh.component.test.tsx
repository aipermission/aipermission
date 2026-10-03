import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { useRef } from "react";
import { apiGet, apiPut } from "../../lib/api";
import { useConnectorTokenPermissionState } from "../../components/console/use-connector-token-permission-state";
import type { ConnectorTokenPermissionOptions } from "../../components/console/use-connector-token-permission-state";
import { useConsoleProjectScopes } from "../../components/console/use-console-project-scopes";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPut: vi.fn() }));
const target = { connector_kind: "fixture", target_id: 7, profile_id: 11, project_id: 3 };
const token = { id: 5, name: "Agent" };
const snapshot = (enabled: boolean, revision: string) => ({
  items: [{ project_id: 3, project_name: "My Project", project_slug: "my-project", enabled }],
  revision,
});

function setup() {
  let finish!: () => void;
  const refresh = new Promise<void>((resolve) => {
    finish = resolve;
  });
  const options: ConnectorTokenPermissionOptions = {
    selectedTarget: target,
    targets: { data: [target] },
    tokens: { data: [token] },
    loadAllConnectorPermissions: vi.fn().mockResolvedValue({}),
    loadConnectorActions: vi.fn().mockResolvedValue([]),
    onRefresh: vi.fn(() => refresh),
  };
  return {
    ...renderHook(({ current }) => useConnectorTokenPermissionState(current), { initialProps: { current: options } }),
    options,
    finish,
  };
}

beforeEach(() => {
  vi.mocked(apiGet).mockReset().mockResolvedValueOnce(snapshot(true, "r1")).mockResolvedValue(snapshot(false, "r2"));
  vi.mocked(apiPut).mockReset().mockResolvedValue(snapshot(true, "r3"));
  window.localStorage.clear();
});

it.each([false, true])("reconciles visibility and revision after parent refresh, rerender=%s", async (rerenderParent) => {
  const { result, options, rerender, finish } = setup();
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  let pending!: Promise<void>;
  act(() => {
    pending = result.current.refreshPanel();
  });
  if (rerenderParent) rerender({ current: { ...options, selectedTarget: { ...target }, tokens: { data: [{ ...token }] } } });
  await act(async () => {
    finish();
    await pending;
  });
  expect(apiGet).toHaveBeenCalledTimes(2);
  expect(result.current.projectEnabledForToken(5)).toBe(false);
  await act(async () => {
    await result.current.setProjectVisibility(token, true);
  });
  expect(apiPut).toHaveBeenCalledWith(
    "/api/tokens/5/project-scopes",
    { enabled_project_ids: [3], expected_revision: "r2" },
    expect.anything(),
  );
});

it.each(["target", "aba", "project", "tokens", "unmount"])("retires a pending parent refresh across %s", async (transition) => {
  const { result, options, rerender, unmount, finish } = setup();
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  let pending!: Promise<void>;
  act(() => {
    pending = result.current.refreshPanel();
  });
  if (transition === "unmount") unmount();
  else {
    const next =
      transition === "tokens"
        ? { ...options, tokens: { data: [] } }
        : { ...options, selectedTarget: { ...target, ...(transition === "project" ? { project_id: 4 } : { target_id: 8 }) } };
    rerender({ current: next });
    if (transition === "aba") rerender({ current: options });
    await act(async () => {
      await Promise.resolve();
    });
  }
  const before = vi.mocked(apiGet).mock.calls.length;
  const actionReads = vi.mocked(options.loadConnectorActions!).mock.calls.length;
  await act(async () => {
    finish();
    await pending;
  });
  expect(apiGet).toHaveBeenCalledTimes(before);
  expect(options.loadConnectorActions).toHaveBeenCalledTimes(actionReads);
});

it("rejects a retained refresh handler after a committed render", async () => {
  const { result, options, rerender } = setup();
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  const retained = result.current.refreshPanel;
  rerender({ current: { ...options } });
  await act(async () => {
    await retained();
  });
  expect(options.onRefresh).not.toHaveBeenCalled();
  expect(apiGet).toHaveBeenCalledOnce();
});

it("allows only the newest pending refresh to reconcile", async () => {
  const { result, options, finish } = setup();
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  let first!: Promise<void>;
  let second!: Promise<void>;
  act(() => {
    first = result.current.refreshPanel();
    second = result.current.refreshPanel();
  });
  expect(options.onRefresh).toHaveBeenCalledTimes(2);
  await act(async () => {
    finish();
    await Promise.all([first, second]);
  });
  expect(apiGet).toHaveBeenCalledTimes(2);
  expect(result.current.projectEnabledForToken(5)).toBe(false);
});

it("can retry after parent refresh rejects without starting an unauthorized read", async () => {
  const { result, options, finish } = setup();
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  vi.mocked(options.onRefresh!).mockRejectedValueOnce(new Error("parent refresh failed"));
  await act(async () => {
    await expect(result.current.refreshPanel()).rejects.toThrow("parent refresh failed");
  });
  expect(apiGet).toHaveBeenCalledOnce();
  let pending!: Promise<void>;
  act(() => {
    pending = result.current.refreshPanel();
  });
  await act(async () => {
    finish();
    await pending;
  });
  expect(apiGet).toHaveBeenCalledTimes(2);
});

it.each([3, undefined])("admits only current scope loaders with a selected project: %s", async (projectID) => {
  const { result, rerender, unmount } = renderHook(
    ({ scope }) => {
      const permissionMutationActiveRef = useRef(false);
      return useConsoleProjectScopes({ scope, projectID, tokens: [token], permissionMutationActiveRef });
    },
    { initialProps: { scope: "first" } },
  );
  const retained = result.current.load;
  rerender({ scope: "second" });
  await act(async () => {
    await retained();
  });
  expect(apiGet).not.toHaveBeenCalled();
  await act(async () => {
    await result.current.load();
  });
  expect(apiGet).toHaveBeenCalledTimes(projectID ? 1 : 0);
  const afterUnmount = result.current.load;
  unmount();
  await act(async () => {
    await afterUnmount();
  });
  expect(apiGet).toHaveBeenCalledTimes(projectID ? 1 : 0);
});
