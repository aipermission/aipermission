import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../../lib/api";
import {
  listLocalActionRetryEntries,
  prepareLocalActionRetry,
  preserveLocalActionRetryAttempt,
  releaseLocalActionRetryAttempt,
} from "../../../lib/local-action-retry";
import { connectorActionFixture, connectorApprovalFixture } from "../../../test/connector-action-fixtures";
import { mutationTestWorkspace, mutationObservationQueue, setupMutationRetryStorage } from "../../../test/connector-mutation-test-state";
import { useConnectorMutationOwnership as useOwner } from "./use-connector-mutation-ownership";
import type { MutationLookup } from "./mutation-observation";
import type { ConnectorApproval } from "../../../lib/gateway-contracts/security-contracts";

const target = "example:1:1";
const actions = ["mutate"] as const;
const noActiveRequests = async () => [];
const observations = mutationObservationQueue();
const advanceObservation = observations.advance;
function useConnectorMutationOwnership(ref: string, names: readonly string[], state: string, get: MutationLookup = apiGet) {
  return useOwner(ref, names, state, get, observations.schedule);
}
setupMutationRetryStorage();
beforeEach(async () => {
  observations.clear();
  vi.stubGlobal("fetch", async () => response({}));
  await apiGet("/api/status");
});

function approval(overrides: Partial<ConnectorApproval> = {}) {
  return connectorApprovalFixture({ id: 41, target_ref: target, action_name: actions[0], ...overrides });
}
function response(value: unknown) {
  return new Response(JSON.stringify(value), {
    headers: {
      "Content-Type": "application/json",
      "X-AIPermission-Workspace": mutationTestWorkspace,
      "X-AIPermission-Workspace-Changed": "true",
    },
  });
}

it.each([{}, [null], [{ id: 0 }], [approval({ target_ref: "example:2:2" })]])(
  "fails closed and retries malformed or foreign discovery %j",
  async (malformed) => {
    const get = vi.fn().mockResolvedValueOnce(malformed).mockResolvedValue([]);
    const rendered = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", get));
    await waitFor(() => expect(observations.pending).toBeGreaterThan(0));
    expect(rendered.result.current.locked).toBe(true);
    await advanceObservation();
    await waitFor(() => expect(rendered.result.current.locked).toBe(false));
    expect(get).toHaveBeenCalledTimes(2);
  },
);

it("checks the target scope with bounded reads and never dispatches during discovery", async () => {
  const get = vi.fn().mockResolvedValue([]);
  const post = vi.fn();
  const { result } = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", get));
  await act(async () => {
    await result.current.run(post);
  });
  expect(post).not.toHaveBeenCalled();
  await waitFor(() => expect(result.current.locked).toBe(false));
  expect(get).toHaveBeenCalledWith(
    "/api/connector-action-approvals?target_ref=example%3A1%3A1&action_name=mutate&active=true",
    expect.objectContaining({ signal: expect.any(AbortSignal), timeoutMs: 10000, workspaceBinding: mutationTestWorkspace }),
  );
});

it("discovers every mutation action independently of a noisy read activity feed", async () => {
  const names = ["write_first", "write_second"];
  const get = vi.fn(async (path: string) => (path.includes("action_name=write_second") ? [approval({ action_name: names[1] })] : []));
  const { result } = renderHook(() => useConnectorMutationOwnership(target, names, "ready", get));
  await waitFor(() => expect(result.current.ownerRef.current?.requestID).toBe(41));
  expect(result.current.locked).toBe(true);
  expect(get.mock.calls.map(([path]) => path)).toEqual(
    names.map((action) => `/api/connector-action-approvals?target_ref=example%3A1%3A1&action_name=${action}&active=true`),
  );
});

it("retains ownership when an action-filtered response contains another action", async () => {
  const get = vi.fn().mockResolvedValue([approval({ action_name: "read_value" })]);
  const { result } = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", get));
  await waitFor(() => expect(observations.pending).toBeGreaterThan(0));
  expect(result.current.locked).toBe(true);
  expect(result.current.ownerRef.current).toBeNull();
});

it("settles an exact terminal ledger request on a cold remount even when absent from the active listing", async () => {
  const prepared = await prepareLocalActionRetry(
    {
      path: "/api/connector-actions/local-run",
      body: {
        target_ref: target,
        action_name: actions[0],
        input: {},
        reason: "cold remount control",
      },
    },
    { workspaceID: mutationTestWorkspace, exclusiveMutationActions: actions },
  );
  await preserveLocalActionRetryAttempt(prepared, { request_id: 41, target_ref: target, action_name: actions[0] });
  const read = vi.fn(async (url: string) => response(url.endsWith("/41") ? approval({ status: "completed" }) : []));
  vi.stubGlobal("fetch", read);
  const { result } = renderHook(() => useConnectorMutationOwnership(target, actions, "ready"));
  await waitFor(() => expect(result.current.locked).toBe(false));
  expect(read).toHaveBeenCalledWith(expect.stringContaining("/api/connector-action-approvals/41"), expect.any(Object));
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it("does not guess that an unattributed pending identity belongs to another target", async () => {
  const prepared = await prepareLocalActionRetry({ old_shape: "unattributed" }, { workspaceID: mutationTestWorkspace });
  await releaseLocalActionRetryAttempt(prepared);
  const { result } = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", noActiveRequests));
  await waitFor(() => expect(observations.pending).toBeGreaterThan(0));
  expect(result.current.locked).toBe(true);
  const post = vi.fn();
  await act(async () => {
    await result.current.run(post);
  });
  expect(post).not.toHaveBeenCalled();
});

it("aborts an in-flight observation and cancels its next read when unmounted", async () => {
  let signal: AbortSignal | undefined;
  const get: MutationLookup = async (_path, options) => {
    signal = options.signal;
    return [];
  };
  const rendered = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", get));
  await waitFor(() => expect(rendered.result.current.locked).toBe(false));
  expect(observations.pending).toBe(1);
  rendered.unmount();
  expect(signal?.aborted).toBe(true);
  expect(observations.pending).toBe(0);
});

it("atomically rejects a second begin and ignores stale attempt updates and releases", async () => {
  const { result } = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", noActiveRequests));
  await waitFor(() => expect(result.current.locked).toBe(false));
  let id = 0;
  act(() => {
    id = result.current.begin();
    expect(result.current.begin()).toBe(0);
    expect(result.current.update(id + 1, { requestID: 42, observed: true })).toBe(false);
    expect(result.current.release(id + 1)).toBe(false);
  });
  expect(result.current.ownerRef.current).toMatchObject({ attemptID: id, requestID: null, inFlight: true });
  act(() => expect(result.current.update(id, { requestID: 41, observed: true })).toBe(true));
  expect(result.current.ownerRef.current).toMatchObject({ requestID: 41, inFlight: false });
  act(() => expect(result.current.release(id)).toBe(true));
});

it("does not discover or authorize mutation while approval state is unavailable", async () => {
  const get = vi.fn().mockResolvedValue([]);
  const { result, rerender } = renderHook(({ state }) => useConnectorMutationOwnership(target, actions, state, get), {
    initialProps: { state: "loading" },
  });
  expect(result.current.locked).toBe(true);
  expect(get).not.toHaveBeenCalled();
  rerender({ state: "ready" });
  await waitFor(() => expect(result.current.locked).toBe(false));
});

it("recovers a pending mutation outside the bounded activity feed after remount", async () => {
  const get = vi.fn().mockResolvedValue([approval()]);
  const first = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", get));
  await waitFor(() => expect(first.result.current.ownerRef.current?.requestID).toBe(41));
  first.unmount();
  const next = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", get));
  await waitFor(() => expect(next.result.current.ownerRef.current?.requestID).toBe(41));
  expect(next.result.current.locked).toBe(true);
});

it.each(["completed", "failed", "canceled", "blocked", "stale", "declined", "error"] as const)(
  "releases an exact %s terminal request, including after it leaves the active listing",
  async (status) => {
    const get = vi
      .fn()
      .mockResolvedValueOnce([approval()])
      .mockImplementation(async (path: string) => (path.endsWith("/41") ? approval({ status }) : []));
    const { result } = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", get));
    await waitFor(() => expect(result.current.ownerRef.current?.requestID).toBe(41));
    expect(result.current.locked).toBe(true);
    await advanceObservation();
    await waitFor(() => expect(result.current.locked).toBe(false));
    expect(get).toHaveBeenCalledWith("/api/connector-action-approvals/41", expect.any(Object));
  },
);

it.each([{ id: 42 }, { target_ref: "example:2:2" }, { action_name: "unrelated_action" }, { status: "outcome_unknown" as const }])(
  "retains ownership for a mismatched or unknown exact response %j",
  async (override) => {
    const get = vi
      .fn()
      .mockResolvedValueOnce([approval()])
      .mockImplementation(async (path: string) => (path.endsWith("/41") ? approval({ status: "completed", ...override }) : []));
    const { result } = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", get));
    await waitFor(() => expect(observations.pending).toBeGreaterThan(0));
    await advanceObservation();
    expect(result.current.locked).toBe(true);
    expect(result.current.ownerRef.current?.requestID).toBe(41);
  },
);

it("retains ownership and retries when exact request observation fails", async () => {
  let failure = true;
  const get = vi
    .fn()
    .mockResolvedValueOnce([approval()])
    .mockImplementation(async (path: string) => {
      if (!path.endsWith("/41")) return [];
      if (failure) throw new Error("read failed");
      return approval({ status: "completed" });
    });
  const { result } = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", get));
  await waitFor(() => expect(observations.pending).toBeGreaterThan(0));
  await advanceObservation();
  expect(result.current.locked).toBe(true);
  failure = false;
  await advanceObservation();
  expect(result.current.locked).toBe(false);
});

it("does not let a late old-scope discovery own the newly selected target", async () => {
  let finish!: (_value: unknown) => void;
  const get = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValue([]);
  const { result, rerender } = renderHook(({ ref }) => useConnectorMutationOwnership(ref, actions, "ready", get), {
    initialProps: { ref: target },
  });
  rerender({ ref: "example:2:2" });
  await waitFor(() => expect(result.current.locked).toBe(false));
  await act(async () => finish([approval()]));
  expect(result.current.ownerRef.current).toBeNull();
  expect(result.current.locked).toBe(false);
});

it("discards a late successful mutation from a previous workspace even with the same target", async () => {
  let finish!: (_item: ReturnType<typeof connectorActionFixture>) => void;
  const operation = new Promise<ReturnType<typeof connectorActionFixture>>((resolve) => {
    finish = resolve;
  });
  const rendered = renderHook(() => useConnectorMutationOwnership(target, actions, "ready", noActiveRequests));
  await waitFor(() => expect(rendered.result.current.locked).toBe(false));
  let running!: ReturnType<typeof rendered.result.current.run>;
  act(() => {
    running = rendered.result.current.run(() => operation);
  });
  vi.stubGlobal(
    "fetch",
    async () =>
      new Response("{}", {
        headers: {
          "Content-Type": "application/json",
          "X-AIPermission-Workspace": "next-mutation-workspace",
          "X-AIPermission-Workspace-Changed": "true",
        },
      }),
  );
  await apiGet("/api/status");
  rendered.rerender();
  await waitFor(() => expect(rendered.result.current.locked).toBe(false));
  await act(async () => {
    finish(connectorActionFixture({ target_ref: target, action_name: actions[0] }));
    expect(await running).toBeNull();
  });
  expect(rendered.result.current.ownerRef.current).toBeNull();
});

it.each(["running", "approval_pending", "outcome_unknown"] as const)(
  "retains real retry identity through lost reply, remount, %s replay and draft edits",
  async (status) => {
    const keys: string[] = [];
    let remoteStatus: ConnectorApproval["status"] | null = null;
    const body = {
      target_ref: target,
      action_name: actions[0],
      input: { value: "private-input-canary" },
      reason: "owner integration control",
    };
    vi.stubGlobal("fetch", async (url: string, options?: RequestInit) => {
      if (options?.method === "POST") {
        keys.push(JSON.parse(String(options.body)).idempotency_key);
        remoteStatus = status;
        if (keys.length === 1) throw new TypeError("reply lost after dispatch");
        return response(connectorActionFixture({ request_id: 41, target_ref: target, action_name: actions[0], status }));
      }
      if (url.endsWith("/41")) return response(approval({ status: remoteStatus || status }));
      return response(remoteStatus && remoteStatus !== "completed" ? [approval({ status: remoteStatus })] : []);
    });
    const first = renderHook(() => useConnectorMutationOwnership(target, actions, "ready"));
    await waitFor(() => expect(first.result.current.locked).toBe(false));
    await act(async () => {
      await expect(first.result.current.run(() => apiPost("/api/connector-actions/local-run", body) as Promise<null>)).rejects.toThrow(
        "reply lost",
      );
    });
    expect(JSON.stringify(await listLocalActionRetryEntries())).not.toContain("private-input-canary");
    expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ target_ref: target, action_name: actions[0] })]);
    first.unmount();
    const next = renderHook(() => useConnectorMutationOwnership(target, actions, "ready"));
    await waitFor(() => expect(next.result.current.ownerRef.current?.requestID).toBe(41));
    expect(next.result.current.locked).toBe(true);
    const bypass = vi.fn();
    await act(async () => {
      await next.result.current.run(bypass);
    });
    expect(bypass).not.toHaveBeenCalled();
    expect(keys).toHaveLength(1);
    if (status !== "outcome_unknown") {
      await apiPost("/api/connector-actions/local-run", body);
      expect(keys[1]).toBe(keys[0]);
      remoteStatus = "completed";
      await advanceObservation();
      expect(await listLocalActionRetryEntries()).toEqual([]);
      await waitFor(() => expect(next.result.current.locked).toBe(false));
    } else {
      await apiPost("/api/connector-actions/local-run", body);
      remoteStatus = "completed";
      await advanceObservation();
      expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "outcome_unknown" })]);
      expect(next.result.current.locked).toBe(true);
    }
  },
);
