import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPut } from "../../lib/api";
import { useConnectorTokenPermissionState } from "../../components/console/use-connector-token-permission-state";
import type { ConnectorTokenPermissionOptions } from "../../components/console/use-connector-token-permission-state";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPut: vi.fn() }));
const get = vi.mocked(apiGet);
const put = vi.mocked(apiPut);
const target = { connector_kind: "fixture", target_id: 7, profile_id: 11, project_id: 3 };
const token = { id: 5, name: "Agent" };
const snapshot = (enabled = true, revision = "r1") => ({
  items: [{ project_id: 3, project_name: "My Project", project_slug: "my-project", enabled }],
  revision,
});

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: Error) => void;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

function setup(overrides: Partial<ConnectorTokenPermissionOptions> = {}) {
  const options: ConnectorTokenPermissionOptions = {
    selectedTarget: target,
    targets: { data: [target] },
    tokens: { data: [token] },
    loadAllConnectorPermissions: vi.fn().mockResolvedValue({}),
    loadConnectorActions: vi.fn().mockResolvedValue([]),
    replaceTokenConnectorPermissions: vi.fn().mockResolvedValue([]),
    ...overrides,
  };
  const view = renderHook(({ current }) => useConnectorTokenPermissionState(current), { initialProps: { current: options } });
  return { ...view, options };
}

beforeEach(() => {
  get.mockReset().mockResolvedValue(snapshot());
  put.mockReset();
  window.localStorage.clear();
});

it("keeps a pending save through profile inventory refresh and blocks competing mutations before paint", async () => {
  const mutation = deferred();
  const refresh = deferred();
  const load = vi
    .fn()
    .mockResolvedValue({})
    .mockResolvedValueOnce({})
    .mockResolvedValueOnce({})
    .mockImplementationOnce(() => refresh.promise);
  put.mockReturnValue(mutation.promise);
  const { result, options, rerender } = setup({ loadAllConnectorPermissions: load });
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  let pending!: Promise<void>;
  act(() => {
    pending = result.current.setProjectVisibility(token, false);
    void result.current.setProjectVisibility(token, true);
    void result.current.setConnectorRule(token, 11, { name: "read" }, "always_run");
    void result.current.setProfileLifetime(token, 11, "2030-01-01");
  });
  expect(put).toHaveBeenCalledOnce();
  expect(options.replaceTokenConnectorPermissions).not.toHaveBeenCalled();
  expect(result.current.projectScopeReadyForToken(5)).toBe(false);
  rerender({ current: { ...options, targets: { data: [target, { ...target, profile_id: 12 }] } } });
  await act(async () => {
    await Promise.resolve();
  });
  expect(get).toHaveBeenCalledOnce();
  expect(put.mock.calls[0][2]?.signal?.aborted).toBe(false);
  await act(async () => {
    mutation.resolve(snapshot(false, "r2"));
    await Promise.resolve();
  });
  expect(result.current.savingKey).toBe("5:project:3");
  await act(async () => {
    refresh.resolve({});
    await pending;
  });
  expect(result.current.savingKey).toBe("");
  expect(result.current.projectEnabledForToken(5)).toBe(false);
});

it("does not allow a project mutation during an owned connector mutation", async () => {
  const mutation = deferred();
  const { result } = setup({ replaceTokenConnectorPermissions: vi.fn().mockImplementation(() => mutation.promise) });
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  let pending!: Promise<void>;
  act(() => {
    pending = result.current.setConnectorRule(token, 11, { name: "read" }, "always_run");
    void result.current.setProjectVisibility(token, false);
  });
  expect(put).not.toHaveBeenCalled();
  await act(async () => {
    mutation.resolve([]);
    await pending;
  });
});

it("retires an old save across target A-B-A without erasing a newer save", async () => {
  const first = deferred();
  const second = deferred();
  put.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  const { result, rerender, options } = setup();
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  const retained = result.current.setProjectVisibility;
  let oldPending!: Promise<void>;
  act(() => {
    oldPending = retained(token, false);
  });
  rerender({ current: { ...options, selectedTarget: { ...target, target_id: 8 } } });
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  expect(result.current.savingKey).toBe("");
  expect(put.mock.calls[0][2]?.signal?.aborted).toBe(true);
  rerender({ current: options });
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  act(() => {
    void retained(token, false);
  });
  expect(put).toHaveBeenCalledOnce();
  let pending!: Promise<void>;
  act(() => {
    pending = result.current.setProjectVisibility(token, false);
  });
  await act(async () => {
    first.resolve(snapshot(false, "old"));
    await oldPending;
  });
  expect(result.current.savingKey).toBe("5:project:3");
  expect(result.current.projectEnabledForToken(5)).toBe(true);
  await act(async () => {
    second.resolve(snapshot(false, "new"));
    await pending;
  });
  expect(result.current.savingKey).toBe("");
  expect(options.loadAllConnectorPermissions).toHaveBeenCalledTimes(4);
});

it("rejects retained handlers and late responses after token removal or unmount", async () => {
  const mutation = deferred();
  put.mockReturnValue(mutation.promise);
  const { result, options, rerender, unmount } = setup();
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  const retained = result.current.setProjectVisibility;
  let pending!: Promise<void>;
  act(() => {
    pending = retained(token, false);
  });
  rerender({ current: { ...options, tokens: { data: [] } } });
  expect(result.current.savingKey).toBe("");
  expect(result.current.projectScopeReadyForToken(5)).toBe(false);
  expect(put.mock.calls[0][2]?.signal?.aborted).toBe(true);
  unmount();
  await act(async () => {
    mutation.resolve(snapshot(false));
    await pending;
    await retained(token, true);
  });
  expect(put).toHaveBeenCalledOnce();
  expect(options.loadAllConnectorPermissions).toHaveBeenCalledTimes(2);
});

it("clears busy state on failure and uses the saved revision even if refreshing connector permissions fails", async () => {
  const { result, options } = setup();
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  put.mockRejectedValueOnce(new Error("visibility write failed"));
  await act(async () => {
    await result.current.setProjectVisibility(token, false);
  });
  expect(result.current.projectScopeError).toBe("visibility write failed");
  expect(result.current.savingKey).toBe("");
  vi.mocked(options.loadAllConnectorPermissions!).mockRejectedValueOnce(new Error("refresh failed"));
  put.mockResolvedValueOnce(snapshot(false, "r2"));
  await act(async () => {
    await result.current.setProjectVisibility(token, false);
  });
  expect(result.current.projectScopeError).toContain("Project visibility saved, but refreshing");
  expect(result.current.projectEnabledForToken(5)).toBe(false);
  put.mockResolvedValueOnce(snapshot(true, "r3"));
  await act(async () => {
    await result.current.setProjectVisibility(token, true);
  });
  expect(put.mock.calls[2][1]).toEqual({ enabled_project_ids: [3], expected_revision: "r2" });
  expect(result.current.projectScopeError).toBe("");
});

it("does not load or mutate project scopes without a selected project", async () => {
  const { result } = setup({ selectedTarget: null, targets: { data: [] } });
  await act(async () => {
    await result.current.setProjectVisibility(token, true);
    await result.current.refreshPanel();
  });
  expect(get).not.toHaveBeenCalled();
  expect(put).not.toHaveBeenCalled();
  expect(result.current.projectScopeReadyForToken(5)).toBe(false);
});

it("defers a refresh continuation while a same-tick write owns the token", async () => {
  const mutation = deferred();
  const { result } = setup();
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  put.mockReturnValueOnce(mutation.promise);
  let refreshing!: Promise<void>;
  let saving!: Promise<void>;
  act(() => {
    refreshing = result.current.refreshPanel();
    saving = result.current.setProjectVisibility(token, false);
  });
  await act(async () => {
    await Promise.resolve();
  });
  // The refresh continuation must not start a read while the write owns this token.
  expect(get).toHaveBeenCalledOnce();
  await act(async () => {
    mutation.resolve(snapshot(false, "saved"));
    await saving;
    await refreshing;
  });
  expect(result.current.projectEnabledForToken(5)).toBe(false);
});

it("does not publish a read from an old target when it rejects after a scope change", async () => {
  const read = deferred();
  get.mockReturnValueOnce(read.promise);
  const { result, options, rerender } = setup();
  await waitFor(() => expect(get).toHaveBeenCalledOnce());
  rerender({ current: { ...options, selectedTarget: { ...target, target_id: 8 } } });
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  await act(async () => {
    read.reject(new Error("old target failed"));
  });
  expect(result.current.projectScopeError).toBe("");
  expect(result.current.projectEnabledForToken(5)).toBe(true);
});
