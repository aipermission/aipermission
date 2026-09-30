import {
  consoleCommandBatch,
  isDefinitiveConsoleCommandStatus,
  isUnknownConsoleCommandStatus,
} from "../gateway-contracts/console-command-contract.ts";
import { retryIdentityChangedError } from "./errors.ts";
import type { CommandBatchIdentity } from "../gateway-contracts/console-command-contract.ts";
import type { RetryEntry } from "./records.ts";

export function acknowledgedCommandBatch(entry: RetryEntry, data: Record<string, unknown> | null) {
  if (entry.request_kind !== "console_batch" || data?.items === undefined) return {};
  const incoming = consoleCommandBatch(data).items;
  const previous = entry.batch_requests;
  if (previous && !sameCommandBatchIdentity(previous, incoming)) throw retryIdentityChangedError();
  const batch_requests = incoming.map(({ request_id, target_id, status }) => {
    const old = previous?.find((item) => item.request_id === request_id);
    const nextStatus = old?.observed && isDefinitiveConsoleCommandStatus(old.status) ? old.status : retainedStatus(old?.status, status);
    return {
      request_id,
      target_id,
      status: nextStatus,
      ...(old?.observed && nextStatus === old.status ? { observed: true } : {}),
    };
  });
  return {
    batch_requests,
    ...(batch_requests.some((item) => isUnknownConsoleCommandStatus(item.status)) ? { state: "outcome_unknown" as const } : {}),
  };
}

export function sameCommandBatchIdentity(left: readonly CommandBatchIdentity[], right: readonly CommandBatchIdentity[]) {
  return (
    left.length === right.length &&
    left.every((old) => right.some((item) => old.request_id === item.request_id && old.target_id === item.target_id))
  );
}

export function completedCommandBatch(entry: RetryEntry | undefined) {
  const requests = entry?.batch_requests;
  return (
    entry?.state === "pending" &&
    entry.request_kind === "console_batch" &&
    requests !== undefined &&
    requests.length > 0 &&
    requests.every((item) => item.observed === true && isDefinitiveConsoleCommandStatus(item.status))
  );
}

export function retainedStatus(previous: CommandBatchIdentity["status"] | undefined, incoming: CommandBatchIdentity["status"]) {
  if (previous && isUnknownConsoleCommandStatus(previous)) return previous;
  if (isUnknownConsoleCommandStatus(incoming)) return incoming;
  return previous && isDefinitiveConsoleCommandStatus(previous) ? previous : incoming;
}
