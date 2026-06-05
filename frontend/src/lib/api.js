export const apiUrl =
  import.meta.env.VITE_API_URL === undefined
    ? "http://localhost:8080"
    : normalizeApiUrl(import.meta.env.VITE_API_URL);
export const mcpApiUrl =
  import.meta.env.VITE_MCP_API_URL === undefined
    ? "http://localhost:3210"
    : normalizeApiUrl(import.meta.env.VITE_MCP_API_URL || browserOrigin());

export async function apiGet(path) {
  const response = await fetch(`${apiUrl}${path}`, { credentials: "include" });
  return readResponse(response);
}

export async function apiPost(path, body, options = {}) {
  const response = await fetch(`${apiUrl}${path}`, {
    method: "POST",
    headers: csrfHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(body),
    signal: options.signal,
    credentials: "include",
  });
  return readResponse(response);
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
  const response = await fetch(`${apiUrl}${path}`, { credentials: "include" });
  if (!response.ok) {
    return readResponse(response);
  }
  const blob = await response.blob();
  return saveBlob(blob, filename, options);
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
  const token = readCookie("aipermission_csrf");
  if (!token) return base;
  return { ...base, "X-AIPermission-CSRF": token };
}

function readCookie(name) {
  if (typeof document === "undefined") return "";
  const prefix = `${name}=`;
  return document.cookie
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(prefix))
    ?.slice(prefix.length) || "";
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
