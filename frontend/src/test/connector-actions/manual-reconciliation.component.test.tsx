import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import {
  listLocalActionRetryEntries,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  resolveLocalActionRetryEntry,
  resetLocalActionRetryLedger,
} from "../../lib/local-action-retry";
import { useConnectorMutationOwnership } from "../../connectors/templates/_shared/use-connector-mutation-ownership";
import { connectorApprovalFixture } from "../connector-action-fixtures";
import { mutationObservationQueue, mutationTestWorkspace, setupMutationRetryStorage } from "../connector-mutation-test-state";

const targetRef = "example:1:1";
const actions = ["mutate"];
const observations = mutationObservationQueue();
setupMutationRetryStorage();
beforeEach(async () => {
  observations.clear();
  vi.stubGlobal(
    "fetch",
    async () =>
      new Response("{}", {
        headers: {
          "Content-Type": "application/json",
          "X-AIPermission-Workspace": mutationTestWorkspace,
          "X-AIPermission-Workspace-Changed": "true",
        },
      }),
  );
  await apiGet("/api/status");
});

async function unknownRequest() {
  const prepared = await prepareLocalActionRetry(
    {
      path: "/api/connector-actions/local-run",
      body: { target_ref: targetRef, action_name: actions[0], input: {}, reason: "reconciliation control" },
    },
    { workspaceID: mutationTestWorkspace, exclusiveMutationActions: actions },
  );
  await markLocalActionRetryOutcome(prepared, { request_id: 71, status: "outcome_unknown" });
  const [entry] = await listLocalActionRetryEntries();
  return entry;
}

it("keeps explicit reconciliation across polling, a second tab and remount without inventing completion", async () => {
  const entry = await unknownRequest();
  const remote = connectorApprovalFixture({ id: 71, target_ref: targetRef, action_name: actions[0], status: "outcome_unknown" });
  const get = vi.fn(async (_path: string) => [remote]);
  const useOwner = () => useConnectorMutationOwnership(targetRef, actions, "ready", get, observations.schedule);
  const first = renderHook(useOwner);
  const second = renderHook(useOwner);
  await waitFor(() => expect(first.result.current.ownerRef.current?.requestID).toBe(71));
  await waitFor(() => expect(second.result.current.ownerRef.current?.requestID).toBe(71));
  await act(async () => expect(await resolveLocalActionRetryEntry(entry)).toBe(true));
  expect(await listLocalActionRetryEntries()).toEqual([]);
  await observations.advance();
  await observations.advance();
  await waitFor(() => expect(first.result.current.locked).toBe(false));
  await waitFor(() => expect(second.result.current.locked).toBe(false));
  first.unmount();
  second.unmount();
  const remounted = renderHook(useOwner);
  await waitFor(() => expect(remounted.result.current.locked).toBe(false));
  expect(remote.status).toBe("outcome_unknown");
  expect(get.mock.calls.every((args) => !String(args[0]).includes("local-run"))).toBe(true);
});

it.each(["running", "approval_pending", "another-request"])("does not use reconciliation to suppress %s", async (status) => {
  const entry = await unknownRequest();
  expect(await resolveLocalActionRetryEntry(entry)).toBe(true);
  const remote = connectorApprovalFixture({
    id: status === "another-request" ? 72 : 71,
    target_ref: targetRef,
    action_name: actions[0],
    status: status === "another-request" ? "outcome_unknown" : (status as "running" | "approval_pending"),
  });
  const get = vi.fn(async () => [remote]);
  const { result } = renderHook(() => useConnectorMutationOwnership(targetRef, actions, "ready", get, observations.schedule));
  await waitFor(() => expect(result.current.ownerRef.current?.requestID).toBe(remote.id));
  expect(result.current.locked).toBe(true);
  const dispatch = vi.fn();
  await act(async () => {
    await result.current.run(dispatch);
  });
  expect(dispatch).not.toHaveBeenCalled();
});

it("rediscovery is conservative after an explicit storage reset removes reconciliation evidence", async () => {
  const entry = await unknownRequest();
  expect(await resolveLocalActionRetryEntry(entry)).toBe(true);
  await resetLocalActionRetryLedger();
  const remote = connectorApprovalFixture({ id: 71, target_ref: targetRef, action_name: actions[0], status: "outcome_unknown" });
  const get = vi.fn(async () => [remote]);
  const { result } = renderHook(() => useConnectorMutationOwnership(targetRef, actions, "ready", get, observations.schedule));
  await waitFor(() => expect(result.current.ownerRef.current?.requestID).toBe(71));
  expect(result.current.locked).toBe(true);
});
