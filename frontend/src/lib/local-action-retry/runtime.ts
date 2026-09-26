import { scopedUICookieName } from "../ui-cookie.ts";
import {
  legacyStoragePrefix,
  localActionReconciliationEvent,
  localActionRetryLedgerChangedEvent,
  workspaceCookieName,
} from "./constants.ts";
import { storageError } from "./errors.ts";
import type { RetryScope } from "./records.ts";

export type ReconciliationDetail = {
  requestID?: number | null;
  operationRef?: string;
  assistantHint?: string;
  createdAt?: string;
  resolve: (_confirmed: boolean) => void;
};

type LegacyScope = RetryScope & { legacyKey: string };
type ReconciliationEntry = {
  request_id?: unknown;
  operation_ref?: unknown;
  assistant_hint?: unknown;
  created_at: string;
};

export function currentRetryScope(explicitWorkspaceID = "") {
  const workspaceID = String(explicitWorkspaceID || readCookie(scopedUICookieName(workspaceCookieName))).trim();
  if (!workspaceID) throw new Error("Database retry identity is unavailable; the connector action was not sent.");
  return { key: workspaceID, legacyKey: `${legacyStoragePrefix}${workspaceID}` };
}

export function assertNoLegacyLedger(scope: LegacyScope) {
  if (readLegacyLedger(scope)) {
    throw new Error("An earlier retry ledger requires manual reconciliation in Settings before connector actions can run.");
  }
}

export function readLegacyLedger(scope: LegacyScope) {
  try {
    return globalThis.window?.localStorage?.getItem(scope.legacyKey) || "";
  } catch {
    throw storageError();
  }
}

export function removeLegacyLedger(scope: LegacyScope) {
  try {
    globalThis.window?.localStorage?.removeItem(scope.legacyKey);
  } catch {
    throw storageError();
  }
}

export function isBrowserRuntime() {
  return typeof window !== "undefined";
}

export function usesIndexedDB() {
  if (!isBrowserRuntime()) return false;
  requireBrowserIndexedDB();
  return true;
}

export function requireBrowserIndexedDB() {
  if (!globalThis.indexedDB) throw storageError();
}

export function notifyChanged() {
  if (typeof window !== "undefined" && typeof window.dispatchEvent === "function" && typeof CustomEvent === "function") {
    window.dispatchEvent(new CustomEvent(localActionRetryLedgerChangedEvent));
  }
}

export function requestReconciliation(entry: ReconciliationEntry): Promise<boolean> {
  if (typeof window === "undefined") return Promise.resolve(false);
  return new Promise<boolean>((resolve) => {
    let settled = false;
    const finish = (value: boolean) => {
      if (settled) return;
      settled = true;
      resolve(Boolean(value));
    };
    const event = new CustomEvent<ReconciliationDetail>(localActionReconciliationEvent, {
      cancelable: true,
      detail: {
        requestID:
          typeof entry.request_id === "number" && Number.isSafeInteger(entry.request_id) && entry.request_id > 0 ? entry.request_id : null,
        operationRef: typeof entry.operation_ref === "string" ? entry.operation_ref : "",
        assistantHint: typeof entry.assistant_hint === "string" ? entry.assistant_hint : "",
        createdAt: entry.created_at,
        resolve: finish,
      },
    });
    if (window.dispatchEvent(event)) finish(false);
  });
}

function readCookie(name: string) {
  if (typeof document === "undefined") return "non-browser";
  const prefix = `${name}=`;
  return (
    document.cookie
      .split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(prefix))
      ?.slice(prefix.length) || ""
  );
}
