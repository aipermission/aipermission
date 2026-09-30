import { connectorApproval, connectorApprovals } from "../gateway-contracts/security-contracts";
import { isDefinitiveConnectorActionStatus } from "../gateway-contracts/connector-action-contract";
import { allEntries, deleteEntryIfMatching, updateEntryIfMatching } from "./entries";
import { currentRetryScope, notifyChanged } from "./runtime";
import type { ConnectorApproval } from "../gateway-contracts/security-contracts";
import type { RetryEntry, RetryScope } from "./records";

export async function observeLocalActionRetryResponse(path: string, value: unknown, workspaceID: string) {
  const route = /^\/api\/connector-action-approvals(?:\/([1-9]\d*))?(?:\?.*)?$/.exec(path);
  if (!route || !workspaceID) return;
  const scope = currentRetryScope(workspaceID);
  let entries: RetryEntry[];
  try {
    entries = (await allEntries(scope)).filter(observableEntry);
  } catch {
    reportObservationFailure();
    return;
  }
  if (!entries.length) return;
  const items = route[1] ? [connectorApproval(value)] : connectorApprovals(value);
  if (route[1] && items[0].id !== Number(route[1])) throw new Error("Invalid connector request observation identity.");
  try {
    await settleObservations(scope, entries, items);
  } catch {
    reportObservationFailure();
  }
}

async function settleObservations(scope: RetryScope, entries: RetryEntry[], items: ConnectorApproval[]) {
  for (const entry of entries) {
    const item = items.find((candidate) => matchesRequest(entry, candidate));
    if (!item) continue;
    if (item.status === "outcome_unknown") {
      await updateEntryIfMatching(
        { scope, signature: entry.signature, idempotencyKey: entry.key, revision: entry.revision },
        (current) => ({
          ...current,
          state: "outcome_unknown",
          revision: current.revision + 1,
          assistant_hint: String(item.assistant_hint || "").slice(0, 1024),
          updated_at: new Date().toISOString(),
        }),
      );
    } else if (isDefinitiveConnectorActionStatus(item.status)) {
      await deleteEntryIfMatching(scope, entry.signature, entry.key, entry.revision);
    }
  }
}

function reportObservationFailure() {
  notifyChanged();
  console.warn("Local retry reconciliation could not be persisted. Protected actions must be reconciled in Settings before retrying.");
}

function observableEntry(entry: RetryEntry) {
  return (
    entry.state === "pending" &&
    typeof entry.request_id === "number" &&
    Number.isSafeInteger(entry.request_id) &&
    entry.request_id > 0 &&
    typeof entry.target_ref === "string" &&
    entry.target_ref.length > 0 &&
    typeof entry.action_name === "string" &&
    entry.action_name.length > 0
  );
}

function matchesRequest(entry: RetryEntry, item: ConnectorApproval) {
  return item.id === entry.request_id && item.target_ref === entry.target_ref && item.action_name === entry.action_name;
}
