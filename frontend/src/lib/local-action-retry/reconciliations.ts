import { maxReconciliations, reconciliationsStore } from "./constants.ts";
import { storageError } from "./errors.ts";
import { isDefinitiveConnectorActionStatus } from "../gateway-contracts/connector-action-contract.ts";
import { connectorApproval } from "../gateway-contracts/security-contracts.ts";
import type { ConnectorApproval } from "../gateway-contracts/security-contracts.ts";
import type { RetryEntry, RetryScope } from "./records.ts";
import { notifyChanged, usesIndexedDB } from "./runtime.ts";
import { memoryReconciliations, openRetryDatabase, requestPromise, transactionPromise, withMemoryTransaction } from "./storage.ts";

export type ReconciledRequest = {
  id: string;
  scope: string;
  request_id: number;
  target_ref: string;
  action_name: string;
};

function recordID(scope: string, requestID: number, targetRef: string, actionName: string) {
  return JSON.stringify([scope, requestID, targetRef, actionName]);
}

type ReconciliationSubject = Pick<RetryEntry, "scope" | "request_id" | "target_ref" | "action_name" | "request_kind">;

function reconciliationRecord(entry: ReconciliationSubject): ReconciledRequest | null {
  if (entry.request_kind === "console_batch" || !entry.request_id || !entry.target_ref || !entry.action_name) return null;
  const record = {
    id: recordID(entry.scope, entry.request_id, entry.target_ref, entry.action_name),
    scope: entry.scope,
    request_id: entry.request_id,
    target_ref: entry.target_ref,
    action_name: entry.action_name,
  };
  if (!validRecord(record, entry.scope)) throw storageError();
  return record;
}

function validRecord(value: unknown, scope: string): value is ReconciledRequest {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as ReconciledRequest;
  return (
    record.scope === scope &&
    Number.isSafeInteger(record.request_id) &&
    record.request_id > 0 &&
    typeof record.target_ref === "string" &&
    /^[a-z][a-z0-9_-]*:[1-9]\d*:[1-9]\d*$/.test(record.target_ref) &&
    typeof record.action_name === "string" &&
    /^[a-z][a-z0-9_]*$/.test(record.action_name) &&
    record.id === recordID(scope, record.request_id, record.target_ref, record.action_name)
  );
}

function fullError() {
  return new Error("Local reconciliation storage is full. Inspect History and external state before resetting retry storage in Settings.");
}

// Called inside the same entry/attempt transaction as the explicit operator CAS.
export function recordMemoryReconciliation(entry: ReconciliationSubject) {
  const record = reconciliationRecord(entry);
  if (!record) return;
  const existing = memoryReconciliations.get(record.id);
  if (existing !== undefined && !validRecord(existing, entry.scope)) throw storageError();
  if (!memoryReconciliations.has(record.id) && memoryReconciliations.size >= maxReconciliations) throw fullError();
  memoryReconciliations.set(record.id, record);
}

export async function recordStoredReconciliation(store: IDBObjectStore, entry: ReconciliationSubject) {
  const record = reconciliationRecord(entry);
  if (!record) return;
  const existing: unknown = await requestPromise(store.get(record.id));
  if (existing !== undefined && !validRecord(existing, entry.scope)) throw storageError();
  if (existing === undefined && (await requestPromise(store.count())) >= maxReconciliations) throw fullError();
  await requestPromise(store.put(record));
}

export async function reconcileVerifiedServerRequest(scope: RetryScope, value: unknown) {
  const item = connectorApproval(value);
  if (item.status !== "outcome_unknown") throw new Error("Only a verified unknown-outcome request can be manually reconciled.");
  const subject = { scope: scope.key, request_id: item.id, target_ref: item.target_ref, action_name: item.action_name };
  if (!usesIndexedDB()) await withMemoryTransaction(() => recordMemoryReconciliation(subject));
  else {
    const database = await openRetryDatabase();
    await transactionPromise(database, reconciliationsStore, "readwrite", (store) => recordStoredReconciliation(store, subject));
  }
  notifyChanged();
}

export async function reconciledRequests(scope: RetryScope) {
  const records: unknown[] = usesIndexedDB()
    ? await requestPromise(
        (await openRetryDatabase()).transaction(reconciliationsStore).objectStore(reconciliationsStore).index("scope").getAll(scope.key),
      )
    : Array.from(memoryReconciliations.values()).filter((record) => record.scope === scope.key);
  if (!records.every((record): record is ReconciledRequest => validRecord(record, scope.key))) throw storageError();
  return records;
}

export function requestWasReconciled(records: readonly ReconciledRequest[], item: { id: number; target_ref: string; action_name: string }) {
  return records.some(
    (record) => record.request_id === item.id && record.target_ref === item.target_ref && record.action_name === item.action_name,
  );
}

export async function forgetDefinitiveReconciliations(scope: RetryScope, items: readonly ConnectorApproval[]) {
  const ids = items
    .filter((item) => isDefinitiveConnectorActionStatus(item.status))
    .map((item) => recordID(scope.key, item.id, item.target_ref, item.action_name));
  if (!ids.length) return;
  if (!usesIndexedDB()) {
    await withMemoryTransaction(() => {
      for (const id of ids) memoryReconciliations.delete(id);
    });
    return;
  }
  const database = await openRetryDatabase();
  await transactionPromise(database, reconciliationsStore, "readwrite", async (store) => {
    for (const id of ids) await requestPromise(store.delete(id));
  });
}
