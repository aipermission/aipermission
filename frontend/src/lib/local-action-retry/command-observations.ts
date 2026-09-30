import { consoleCommandDetail, isUnknownConsoleCommandStatus } from "../gateway-contracts/console-command-contract.ts";
import { allEntries, deleteEntryIfMatching, updateEntryIfMatching } from "./entries.ts";
import { completedCommandBatch, retainedStatus, sameCommandBatchIdentity } from "./command-batches.ts";
import { reportObservationFailure } from "./observation-errors.ts";
import { currentRetryScope } from "./runtime.ts";
import type { RetryEntry } from "./records.ts";

export async function observeCommandBatchResponse(path: string, value: unknown, workspaceID: string) {
  const route = /^\/api\/console\/command-requests\/([1-9]\d*)(?:\?.*)?$/.exec(path);
  if (!route || !workspaceID) return false;
  const scope = currentRetryScope(workspaceID);
  let entries: RetryEntry[];
  try {
    entries = (await allEntries(scope)).filter(
      (entry) => entry.state === "pending" && entry.request_kind === "console_batch" && entry.batch_requests?.length,
    );
  } catch {
    reportObservationFailure();
    return true;
  }
  if (!entries.length) return true;
  const detail = consoleCommandDetail(value, Number(route[1]));
  try {
    for (const entry of entries) {
      const requests = entry.batch_requests;
      const match = requests?.find((item) => item.request_id === detail.id && item.target_id === detail.runtime_id);
      if (!requests || !match) continue;
      const projection = { entry: null as RetryEntry | null };
      const changed = await updateEntryIfMatching(
        { scope, signature: entry.signature, idempotencyKey: entry.key, revision: entry.revision },
        (current) => {
          if (current.state !== "pending" || !current.batch_requests || !sameCommandBatchIdentity(current.batch_requests, requests))
            return current;
          const batch_requests = current.batch_requests.map((item) =>
            item.request_id === detail.id
              ? { ...item, status: item.observed ? retainedStatus(item.status, detail.status) : detail.status, observed: true }
              : item,
          );
          projection.entry = {
            ...current,
            batch_requests,
            state: batch_requests.some((item) => isUnknownConsoleCommandStatus(item.status)) ? "outcome_unknown" : "pending",
            revision: current.revision + 1,
            updated_at: new Date().toISOString(),
          };
          return projection.entry;
        },
        true,
      );
      if (changed && projection.entry && completedCommandBatch(projection.entry))
        await deleteEntryIfMatching(scope, entry.signature, entry.key, projection.entry.revision);
    }
  } catch {
    reportObservationFailure();
  }
  return true;
}
