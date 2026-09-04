import { completeLocalActionRetry, markLocalActionRetryOutcome, prepareLocalActionRetry } from "./local-action-retry.js";
import { scopedUICookieName } from "./ui-cookie.js";

const viteEnv = import.meta.env || {};

export const apiUrl = viteEnv.VITE_API_URL === undefined ? "http://localhost:8080" : normalizeApiUrl(viteEnv.VITE_API_URL);
export const mcpApiUrl = normalizeApiUrl(viteEnv.VITE_MCP_API_URL || browserOrigin());

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
  let data;
  try {
    data = await readResponse(response, { requireJSON: Boolean(prepared.retry) });
  } catch (error) {
    if (prepared.retry && error?.data?.status === "outcome_unknown") {
      await markLocalActionRetryOutcome(prepared.retry, error.data);
    } else if (prepared.retry && !prepared.retry.reused && response.status >= 400 && response.status < 500) {
      // A gateway 4xx is a definitive pre-dispatch rejection unless the
      // key predates this attempt. A carried key may represent an external
      // side effect whose response was lost, so pre-handler auth/lock errors
      // cannot retire it.
      await completeLocalActionRetry(prepared.retry);
    }
    throw error;
  }
  if (prepared.retry && response.ok && isAcknowledgedLocalActionResponse(data) && data.status !== "outcome_unknown") {
    await completeLocalActionRetry(prepared.retry);
  }
  if (prepared.retry && response.ok && isAcknowledgedLocalActionResponse(data) && data.status === "outcome_unknown") {
    await markLocalActionRetryOutcome(prepared.retry, data);
  }
  if (prepared.retry && response.ok && !isAcknowledgedLocalActionResponse(data)) {
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
  if (path !== "/api/connector-actions/local-run" || body?.idempotency_key) return { body, retry: null };
  const retry = await prepareLocalActionRetry(body || {});
  return { body: { ...body, idempotency_key: retry.idempotencyKey }, retry };
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
