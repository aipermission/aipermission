import { connectorActionResponse, connectorApproval, connectorApprovals } from "../../../lib/gateway-contracts/security-contracts";
import {
  isDefinitiveConnectorActionStatus,
  isPendingConnectorActionStatus,
} from "../../../lib/gateway-contracts/connector-action-contract";
import { allEntries } from "../../../lib/local-action-retry/entries";
import { retryEntryBlocksMutation } from "../../../lib/local-action-retry/records";
import { assertNoLegacyLedger, currentRetryScope } from "../../../lib/local-action-retry/runtime";
import type { ConnectorApproval } from "../../../lib/gateway-contracts/security-contracts";

export type MutationLookup = (
  _path: string,
  _options: { signal: AbortSignal; timeoutMs: number; workspaceBinding: string },
) => Promise<unknown>;
type ObservationInput = {
  targetRef: string;
  actions: readonly string[];
  workspaceID: string;
  requestID: number | null;
  get: MutationLookup;
  signal: AbortSignal;
};

export function definitiveMutationFailure(error: unknown, targetRef: string, actions: readonly string[]) {
  if (!error || typeof error !== "object" || !("actionItem" in error)) return false;
  try {
    const raw = error.actionItem;
    if (
      !raw ||
      typeof raw !== "object" ||
      !("action_name" in raw) ||
      typeof raw.action_name !== "string" ||
      !actions.includes(raw.action_name)
    )
      return false;
    const item = connectorActionResponse(raw, { targetRef, actionName: raw.action_name });
    return isDefinitiveConnectorActionStatus(item.status);
  } catch {
    return false;
  }
}

export async function observeMutationRequests(input: ObservationInput) {
  const scope = currentRetryScope(input.workspaceID);
  assertNoLegacyLedger(scope);
  const items = await readActiveMutations(input);
  const ledger = await allEntries(scope);
  const candidates = new Map<number, string | undefined>();
  if (input.requestID) candidates.set(input.requestID, undefined);
  for (const entry of ledger) {
    if (entry.state === "pending" && entry.request_id && retryEntryBlocksMutation(entry, input.targetRef, input.actions))
      candidates.set(entry.request_id, entry.action_name);
  }
  const terminalIDs = new Set<number>();
  // Local storage is bounded; cap exact reads per observation and retain all unobserved entries.
  for (const [id, actionName] of Array.from(candidates)
    .filter(([id]) => !items.some((item) => item.id === id))
    .slice(0, 8)) {
    const exact = connectorApproval(
      await input.get(`/api/connector-action-approvals/${id}`, {
        signal: input.signal,
        timeoutMs: 10000,
        workspaceBinding: input.workspaceID,
      }),
    );
    input.signal.throwIfAborted();
    if (
      exact.id !== id ||
      exact.target_ref !== input.targetRef ||
      (actionName ? exact.action_name !== actionName : !input.actions.includes(exact.action_name))
    )
      throw new Error("Mutation observation identity mismatch.");
    if (isDefinitiveConnectorActionStatus(exact.status)) terminalIDs.add(id);
  }
  const remaining = await allEntries(scope);
  input.signal.throwIfAborted();
  return {
    unresolved: items.find((item) => isPendingConnectorActionStatus(item.status) || item.status === "outcome_unknown") || null,
    retained: remaining.some((entry) => retryEntryBlocksMutation(entry, input.targetRef, input.actions)),
    terminalIDs,
  };
}

async function readActiveMutations(input: ObservationInput) {
  const items: ConnectorApproval[] = [];
  for (const actionName of input.actions) {
    const query = new URLSearchParams({ target_ref: input.targetRef, action_name: actionName, active: "true" });
    const next = connectorApprovals(
      await input.get(`/api/connector-action-approvals?${query}`, {
        signal: input.signal,
        timeoutMs: 10000,
        workspaceBinding: input.workspaceID,
      }),
    );
    input.signal.throwIfAborted();
    if (next.some((item) => item.target_ref !== input.targetRef || item.action_name !== actionName))
      throw new Error("Mutation observation target or action mismatch.");
    items.push(...next);
  }
  return items;
}
