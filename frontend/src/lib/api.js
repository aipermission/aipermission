import { scopedUICookieName } from "./ui-cookie.js";

const viteEnv = import.meta.env || {};

export const apiUrl = viteEnv.VITE_API_URL === undefined ? "http://localhost:8080" : normalizeApiUrl(viteEnv.VITE_API_URL);
export const mcpApiUrl = normalizeApiUrl(viteEnv.VITE_MCP_API_URL || browserOrigin());

const localActionRetryKeys = new Map();
const localActionRetryStoragePrefix = "aipermission.local-action-retry.v1.";

export async function apiGet(path) {
  const response = await fetch(`${apiUrl}${path}`, { credentials: "include" });
  return readResponse(response);
}

export async function apiPost(path, body, options = {}) {
  const prepared = await preparePostBody(path, body);
  const response = await fetch(`${apiUrl}${path}`, {
    method: "POST",
    headers: csrfHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(prepared.body),
    signal: options.signal,
    credentials: "include",
  });
  const data = await readResponse(response, { requireJSON: Boolean(prepared.retrySignature) });
  if (prepared.retrySignature && response.ok && isAcknowledgedLocalActionResponse(data)) {
    clearLocalActionRetryKey(prepared.retrySignature, prepared.idempotencyKey);
  }
  if (prepared.retrySignature && response.ok && !isAcknowledgedLocalActionResponse(data)) {
    throw new Error("Invalid connector action response from gateway.");
  }
  return data;
}

const acknowledgedLocalActionStatuses = new Set([
  "completed",
  "failed",
  "canceled",
  "running",
  "approval_pending",
  "blocked",
  "stale",
  "declined",
  "error",
  "outcome_unknown",
]);

function isAcknowledgedLocalActionResponse(data) {
  return (
    data !== null &&
    typeof data === "object" &&
    Number.isSafeInteger(data.request_id) &&
    data.request_id > 0 &&
    acknowledgedLocalActionStatuses.has(data.status)
  );
}

async function preparePostBody(path, body) {
  if (path !== "/api/connector-actions/local-run" || body?.idempotency_key) return { body, retrySignature: "" };
  const signature = stableRequestSignature(body || {});
  const storage = browserLocalStorage();
  const retrySignature = storage ? `${localActionRetryStoragePrefix}${await sha256Hex(signature)}` : signature;
  let idempotencyKey = storage ? readPersistedRetryKey(storage, retrySignature) : localActionRetryKeys.get(retrySignature);
  if (!idempotencyKey) {
    idempotencyKey = globalThis.crypto?.randomUUID?.() || `ui-${Date.now()}-${Math.random().toString(16).slice(2)}`;
    if (storage) storage.setItem(retrySignature, idempotencyKey);
    else localActionRetryKeys.set(retrySignature, idempotencyKey);
  }
  return { body: { ...body, idempotency_key: idempotencyKey }, retrySignature, idempotencyKey };
}

function clearLocalActionRetryKey(retrySignature, idempotencyKey) {
  const storage = browserLocalStorage();
  if (storage) {
    if (storage.getItem(retrySignature) === idempotencyKey) storage.removeItem(retrySignature);
    return;
  }
  if (localActionRetryKeys.get(retrySignature) === idempotencyKey) localActionRetryKeys.delete(retrySignature);
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

function readPersistedRetryKey(storage, key) {
  const value = storage.getItem(key);
  return typeof value === "string" && value.length > 0 ? value : "";
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

export async function apiPostForm(path, formData, options = {}) {
  const response = await fetch(`${apiUrl}${path}`, {
    method: "POST",
    headers: csrfHeaders(),
    body: formData,
    signal: options.signal,
    credentials: "include",
  });
  return readResponse(response);
}

export async function apiPut(path, body) {
  const response = await fetch(`${apiUrl}${path}`, {
    method: "PUT",
    headers: csrfHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(body),
    credentials: "include",
  });
  return readResponse(response);
}

export async function apiDelete(path) {
  const response = await fetch(`${apiUrl}${path}`, { method: "DELETE", headers: csrfHeaders(), credentials: "include" });
  if (response.status === 204) {
    return null;
  }
  return readResponse(response);
}

export async function apiDownload(path, filename, options = {}) {
  const safeFilename = filename.replaceAll(":", "-");
  let saveHandle = null;
  if (options.picker && typeof window !== "undefined" && typeof window.showSaveFilePicker === "function") {
    try {
      saveHandle = await window.showSaveFilePicker({ suggestedName: safeFilename });
    } catch (error) {
      if (error?.name === "AbortError") {
        return { saved: false, canceled: true, method: "picker" };
      }
      throw error;
    }
  }
  const response = await fetch(`${apiUrl}${path}`, { credentials: "include" });
  if (!response.ok) {
    return readResponse(response);
  }
  if (saveHandle && response.body && typeof response.body.pipeTo === "function") {
    const writable = await saveHandle.createWritable();
    await response.body.pipeTo(writable);
    return { saved: true, method: "picker" };
  }
  const blob = await response.blob();
  if (saveHandle) {
    const writable = await saveHandle.createWritable();
    await writable.write(blob);
    await writable.close();
    return { saved: true, method: "picker" };
  }
  return saveBlob(blob, safeFilename, { ...options, picker: false });
}

async function readResponse(response, options = {}) {
  const text = await response.text();
  const data = parseResponseBody(text, options);
  if (!response.ok) {
    if (response.status === 401 && data?.error === "ui session required" && typeof window !== "undefined") {
      window.dispatchEvent(new CustomEvent("aipermission:ui-session-required"));
    }
    const error = new Error(data?.error || `Request failed with ${response.status}`);
    error.status = response.status;
    error.data = data;
    throw error;
  }
  return data;
}

function parseResponseBody(text, options = {}) {
  if (!text) {
    if (options.requireJSON) throw new Error("Empty JSON response from gateway.");
    return null;
  }
  if (looksLikeHTML(text)) {
    if (options.requireJSON) throw new Error("Gateway returned HTML instead of JSON.");
    return { error: "Gateway is starting or temporarily unavailable. Please retry in a few seconds." };
  }
  try {
    return JSON.parse(text);
  } catch {
    if (options.requireJSON) throw new Error("Invalid JSON response from gateway.");
    return { error: text.trim() || "Invalid non-JSON response from gateway." };
  }
}

function looksLikeHTML(text) {
  const trimmed = text.trimStart().toLowerCase();
  return trimmed.startsWith("<!doctype html") || trimmed.startsWith("<html") || trimmed.includes("<body");
}

function normalizeApiUrl(value) {
  const trimmed = String(value || "").replace(/\/+$/, "");
  return trimmed;
}

function browserOrigin() {
  if (typeof window !== "undefined" && window.location?.origin) {
    return window.location.origin;
  }
  return "http://localhost:3210";
}

function csrfHeaders(base = {}) {
  const token = readCookie(scopedUICookieName("aipermission_csrf"));
  if (!token) return base;
  return { ...base, "X-AIPermission-CSRF": token };
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

export function downloadBlob(blob, filename) {
  const safeFilename = filename.replaceAll(":", "-");
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = safeFilename;
  link.click();
  URL.revokeObjectURL(url);
  return { saved: true, method: "anchor" };
}

export async function saveBlob(blob, filename, options = {}) {
  const safeFilename = filename.replaceAll(":", "-");
  if (options.picker && typeof window !== "undefined" && typeof window.showSaveFilePicker === "function") {
    try {
      const handle = await window.showSaveFilePicker({ suggestedName: safeFilename });
      const writable = await handle.createWritable();
      await writable.write(blob);
      await writable.close();
      return { saved: true, method: "picker" };
    } catch (error) {
      if (error?.name === "AbortError") {
        return { saved: false, canceled: true, method: "picker" };
      }
      throw error;
    }
  }
  return downloadBlob(blob, safeFilename);
}

export function downloadJSON(value, filename) {
  const blob = new Blob([JSON.stringify(value, null, 2)], { type: "application/json" });
  downloadBlob(blob, filename);
}
