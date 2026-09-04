import { scopedUICookieName } from "./ui-cookie.js";

export const localActionReconciliationEvent = "aipermission:local-action-reconciliation-required";
export const localActionRetryLedgerChangedEvent = "aipermission:local-action-retry-ledger-changed";

const storagePrefix = "aipermission.local-action-retry.v2.";
const workspaceCookieName = "aipermission_workspace";
const maxEntries = 128;
const memoryLedgers = new Map();

export async function prepareLocalActionRetry(body) {
  const scope = currentRetryScope();
  const signature = await sha256Hex(stableRequestSignature(body || {}));
  const ledger = readLedger(scope);
  const existing = ledger.entries[signature];
  if (existing?.state === "outcome_unknown") {
    const confirmed = await requestReconciliation(existing);
    if (!confirmed) {
      const error = new Error("A new external attempt was canceled. The unresolved request remains protected.");
      error.code = "local_action_reconciliation_canceled";
      throw error;
    }
    delete ledger.entries[signature];
  }
  let entry = ledger.entries[signature];
  if (!entry) {
    if (Object.keys(ledger.entries).length >= maxEntries) {
      throw new Error("The local action retry ledger is full. Reconcile unresolved requests in Settings before starting another action.");
    }
    const now = new Date().toISOString();
    entry = { key: newIdempotencyKey(), state: "pending", created_at: now, updated_at: now };
    ledger.entries[signature] = entry;
    writeLedger(scope, ledger);
  }
  return { scope, signature, idempotencyKey: entry.key };
}

export function markLocalActionRetryOutcome(prepared, data) {
  if (!prepared?.scope || !prepared.signature) return;
  const ledger = readLedger(prepared.scope);
  const entry = ledger.entries[prepared.signature];
  if (!entry || entry.key !== prepared.idempotencyKey) return;
  entry.state = "outcome_unknown";
  entry.request_id = Number.isSafeInteger(data?.request_id) ? data.request_id : null;
  entry.assistant_hint = String(data?.assistant_hint || "").slice(0, 1024);
  entry.updated_at = new Date().toISOString();
  writeLedger(prepared.scope, ledger);
}

export function completeLocalActionRetry(prepared) {
  if (!prepared?.scope || !prepared.signature) return;
  const ledger = readLedger(prepared.scope);
  if (ledger.entries[prepared.signature]?.key !== prepared.idempotencyKey) return;
  delete ledger.entries[prepared.signature];
  writeLedger(prepared.scope, ledger);
}

export function listLocalActionRetryEntries() {
  const scope = currentRetryScope();
  const ledger = readLedger(scope);
  return Object.entries(ledger.entries)
    .map(([signature, entry]) => ({ signature, ...entry }))
    .sort((left, right) => String(right.updated_at).localeCompare(String(left.updated_at)));
}

export function resolveLocalActionRetryEntry(signature) {
  const scope = currentRetryScope();
  const ledger = readLedger(scope);
  if (!ledger.entries[signature]) return false;
  delete ledger.entries[signature];
  writeLedger(scope, ledger);
  return true;
}

export function resetLocalActionRetryLedger() {
  const scope = currentRetryScope();
  writeLedger(scope, { version: 2, entries: {} });
}

function currentRetryScope() {
  const storage = browserLocalStorage();
  const workspaceID = storage ? readCookie(scopedUICookieName(workspaceCookieName)) : "non-browser";
  if (!workspaceID) throw new Error("Database retry identity is unavailable; the connector action was not sent.");
  return { storage, key: `${storagePrefix}${workspaceID}` };
}

function readLedger(scope) {
  const raw = scope.storage ? scope.storage.getItem(scope.key) : memoryLedgers.get(scope.key);
  if (!raw) return { version: 2, entries: {} };
  try {
    const parsed = typeof raw === "string" ? JSON.parse(raw) : raw;
    if (parsed?.version !== 2 || !parsed.entries || typeof parsed.entries !== "object" || Array.isArray(parsed.entries)) {
      throw new Error("invalid ledger shape");
    }
    return parsed;
  } catch {
    throw new Error("The local action retry ledger is invalid. Resolve it from Settings before running connector actions.");
  }
}

function writeLedger(scope, ledger) {
  try {
    if (scope.storage) {
      if (Object.keys(ledger.entries).length === 0) scope.storage.removeItem(scope.key);
      else scope.storage.setItem(scope.key, JSON.stringify(ledger));
    } else if (Object.keys(ledger.entries).length === 0) {
      memoryLedgers.delete(scope.key);
    } else {
      memoryLedgers.set(scope.key, ledger);
    }
  } catch {
    throw new Error("Secure retry storage is unavailable; the connector action was not sent.");
  }
  if (typeof window !== "undefined" && typeof window.dispatchEvent === "function" && typeof CustomEvent === "function") {
    window.dispatchEvent(new CustomEvent(localActionRetryLedgerChangedEvent));
  }
}

function requestReconciliation(entry) {
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

function browserLocalStorage() {
  if (typeof window === "undefined") return null;
  try {
    if (!window.localStorage) throw new Error("storage unavailable");
    return window.localStorage;
  } catch {
    throw new Error("Secure retry storage is unavailable; the connector action was not sent.");
  }
}

function readCookie(name) {
  if (typeof document === "undefined") return "";
  const prefix = `${name}=`;
  return (
    document.cookie
      .split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(prefix))
      ?.slice(prefix.length) || ""
  );
}

function newIdempotencyKey() {
  return globalThis.crypto?.randomUUID?.() || `ui-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

async function sha256Hex(value) {
  if (!globalThis.crypto?.subtle || typeof TextEncoder === "undefined") {
    throw new Error("Secure request hashing is unavailable; the connector action was not sent.");
  }
  const digest = await globalThis.crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function stableRequestSignature(value) {
  if (Array.isArray(value)) return `[${value.map(stableRequestSignature).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableRequestSignature(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}
