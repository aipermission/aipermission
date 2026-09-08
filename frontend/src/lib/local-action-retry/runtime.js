import { scopedUICookieName } from "../ui-cookie.js";
import {
  legacyStoragePrefix,
  localActionReconciliationEvent,
  localActionRetryLedgerChangedEvent,
  workspaceCookieName,
} from "./constants.js";
import { storageError } from "./errors.js";

export function currentRetryScope() {
  const workspaceID = readCookie(scopedUICookieName(workspaceCookieName));
  if (!workspaceID) throw new Error("Database retry identity is unavailable; the connector action was not sent.");
  return { key: workspaceID, legacyKey: `${legacyStoragePrefix}${workspaceID}` };
}

export function assertNoLegacyLedger(scope) {
  if (readLegacyLedger(scope)) {
    throw new Error("An earlier retry ledger requires manual reconciliation in Settings before connector actions can run.");
  }
}

export function readLegacyLedger(scope) {
  try {
    return globalThis.window?.localStorage?.getItem(scope.legacyKey) || "";
  } catch {
    throw storageError();
  }
}

export function removeLegacyLedger(scope) {
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

export function requestReconciliation(entry) {
  if (typeof window === "undefined") return Promise.resolve(false);
  return new Promise((resolve) => {
    let settled = false;
    const finish = (value) => {
      if (settled) return;
      settled = true;
      resolve(Boolean(value));
    };
    const event = new CustomEvent(localActionReconciliationEvent, {
      cancelable: true,
      detail: {
        requestID: entry.request_id || null,
        assistantHint: entry.assistant_hint || "",
        createdAt: entry.created_at,
        resolve: finish,
      },
    });
    if (window.dispatchEvent(event)) finish(false);
  });
}

function readCookie(name) {
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
