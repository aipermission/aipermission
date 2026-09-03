import { scopedUICookieName } from "./ui-cookie.js";

const viteEnv = import.meta.env || {};

export const apiUrl = viteEnv.VITE_API_URL === undefined ? "http://localhost:8080" : normalizeApiUrl(viteEnv.VITE_API_URL);
export const mcpApiUrl = normalizeApiUrl(viteEnv.VITE_MCP_API_URL || browserOrigin());

const localActionRetryKeys = new Map();
const localActionRetryKeyTTL = 5 * 60 * 1000;
const maxLocalActionRetryKeys = 128;

export async function apiGet(path) {
  const response = await fetch(`${apiUrl}${path}`, { credentials: "include" });
  return readResponse(response);
}

export async function apiPost(path, body, options = {}) {
  const prepared = preparePostBody(path, body);
  const response = await fetch(`${apiUrl}${path}`, {
    method: "POST",
    headers: csrfHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(prepared.body),
    signal: options.signal,
    credentials: "include",
  });
  if (prepared.retrySignature && (response.ok || response.status < 500)) {
    localActionRetryKeys.delete(prepared.retrySignature);
  }
  return readResponse(response);
}

function preparePostBody(path, body) {
  if (path !== "/api/connector-actions/local-run" || body?.idempotency_key) return { body, retrySignature: "" };
  const retrySignature = stableRequestSignature(body || {});
  const now = Date.now();
  pruneLocalActionRetryKeys(now);
  let entry = localActionRetryKeys.get(retrySignature);
  if (!entry) {
    entry = {
      key: globalThis.crypto?.randomUUID?.() || `ui-${now}-${Math.random().toString(16).slice(2)}`,
      expiresAt: now + localActionRetryKeyTTL,
    };
    localActionRetryKeys.set(retrySignature, entry);
    trimLocalActionRetryKeys();
  }
  return { body: { ...body, idempotency_key: entry.key }, retrySignature };
}

function pruneLocalActionRetryKeys(now) {
  for (const [signature, entry] of localActionRetryKeys) {
    if (entry.expiresAt <= now) localActionRetryKeys.delete(signature);
  }
}

function trimLocalActionRetryKeys() {
  while (localActionRetryKeys.size > maxLocalActionRetryKeys) {
    localActionRetryKeys.delete(localActionRetryKeys.keys().next().value);
  }
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

async function readResponse(response) {
  const text = await response.text();
  const data = parseResponseBody(text);
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

function parseResponseBody(text) {
  if (!text) return null;
  if (looksLikeHTML(text)) {
    return { error: "Gateway is starting or temporarily unavailable. Please retry in a few seconds." };
  }
  try {
    return JSON.parse(text);
  } catch {
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
