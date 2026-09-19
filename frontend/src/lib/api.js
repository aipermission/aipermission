import {
  completeLocalActionRetry,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  preserveLocalActionRetryAttempt,
  releaseLocalActionRetryAttempt,
} from "./local-action-retry.js";
import { APIError } from "./errors.js";
import { assertConnectorActionResponse } from "./gateway-contracts/connector-action-contract.js";
import { scopedUICookieName } from "./ui-cookie.js";
import { readBufferedDownload } from "./downloads/download-buffer.js";

const viteEnv = import.meta.env || {};
const workspaceHeaderName = "X-AIPermission-Workspace";
const workspaceChangedHeaderName = "X-AIPermission-Workspace-Changed";
let workspaceBinding = "";
let workspaceBindingOwner = null;

export const apiUrl = viteEnv.VITE_API_URL === undefined ? "http://localhost:8080" : normalizeApiUrl(viteEnv.VITE_API_URL);
export const mcpApiUrl = normalizeApiUrl(viteEnv.VITE_MCP_API_URL || browserOrigin());

export async function apiGet(path, options = {}) {
  const request = boundedReadSignal(options.signal, options.timeoutMs);
  try {
    const response = await fetch(`${apiUrl}${path}`, { signal: request.signal, credentials: "include" });
    return await readResponse(response);
  } catch (error) {
    if (request.timedOut()) throw new Error(`Gateway read timed out after ${options.timeoutMs}ms.`, { cause: error });
    throw error;
  } finally {
    request.cleanup();
  }
}

export async function apiPost(path, body, options = {}) {
  const requestWorkspace = currentWorkspaceBinding();
  const prepared = await preparePostBody(path, body, requestWorkspace);
  let finalized = false;
  try {
    const response = await fetch(`${apiUrl}${path}`, {
      method: "POST",
      headers: mutationHeaders({ "Content-Type": "application/json" }, requestWorkspace),
      body: JSON.stringify(prepared.body),
      signal: options.signal,
      credentials: "include",
    });
    let data;
    try {
      data = await readResponse(response);
    } catch (error) {
      if (prepared.retry && error?.data?.status === "outcome_unknown") {
        await markLocalActionRetryOutcome(prepared.retry, error.data);
        finalized = true;
      } else if (prepared.retry && !prepared.retry.reused && response.status >= 400 && response.status < 500) {
        // A gateway 4xx is a definitive pre-dispatch rejection unless the
        // key predates this attempt. Another active attempt still keeps the
        // shared identity protected.
        await completeLocalActionRetry(prepared.retry);
        finalized = true;
      }
      throw error;
    }
    if (response.ok && prepared.acknowledged && !prepared.acknowledged(data)) {
      throw new Error(prepared.invalidResponseMessage);
    }
    if (prepared.retry && response.ok && data.status !== "outcome_unknown") {
      await completeLocalActionRetry(prepared.retry);
      finalized = true;
    }
    if (prepared.retry && response.ok && data.status === "outcome_unknown") {
      await markLocalActionRetryOutcome(prepared.retry, data);
      finalized = true;
    }
    return data;
  } catch (error) {
    finalized = await preserveRetryAfterFailure(prepared.retry, finalized);
    throw error;
  } finally {
    if (prepared.retry && !finalized) await releaseLocalActionRetryAttempt(prepared.retry);
  }
}

async function preserveRetryAfterFailure(retry, finalized) {
  if (!retry || finalized) return finalized;
  await preserveLocalActionRetryAttempt(retry);
  return true;
}

function isAcknowledgedLocalActionResponse(data, body) {
  try {
    assertConnectorActionResponse(data, { targetRef: body?.target_ref, actionName: body?.action_name });
    return true;
  } catch {
    return false;
  }
}

async function preparePostBody(path, body, workspaceID) {
  const policy = idempotentPostPolicy(path, body);
  if (!policy) return { body, retry: null, acknowledged: null, invalidResponseMessage: "" };
  if (body?.idempotency_key) return { body, retry: null, ...policy };
  const retry = await prepareLocalActionRetry({ path, body: body || {} }, { workspaceID });
  return { body: { ...body, idempotency_key: retry.idempotencyKey }, retry, ...policy };
}

function idempotentPostPolicy(path, body) {
  if (path === "/api/connector-actions/local-run") {
    return {
      acknowledged: (data) => isAcknowledgedLocalActionResponse(data, body),
      invalidResponseMessage: "Invalid connector action response from gateway.",
    };
  }
  if (path === "/api/console/bulk-exec") {
    return { acknowledged: isAcknowledgedBulkCommandResponse, invalidResponseMessage: "Invalid bulk command response from gateway." };
  }
  if (/^\/api\/backup\/providers\/\d+\/upload$/.test(path)) {
    return { acknowledged: isAcknowledgedBackupUploadResponse, invalidResponseMessage: "Invalid backup upload response from gateway." };
  }
  return null;
}

function isAcknowledgedBackupUploadResponse(data) {
  return (
    data !== null &&
    typeof data === "object" &&
    Number.isSafeInteger(data.id) &&
    data.id > 0 &&
    typeof data.provider_file_id === "string" &&
    data.provider_file_id.length > 0
  );
}

function isAcknowledgedBulkCommandResponse(data) {
  return (
    data !== null &&
    typeof data === "object" &&
    Number.isSafeInteger(data.parallelism) &&
    data.parallelism > 0 &&
    Array.isArray(data.items) &&
    data.items.length > 0 &&
    data.items.every((item) => Number.isSafeInteger(item?.request_id) && item.request_id > 0)
  );
}

export async function apiPostForm(path, formData, options = {}) {
  const requestWorkspace = options.workspaceBinding || currentWorkspaceBinding();
  const response = await fetch(`${apiUrl}${path}`, {
    method: "POST",
    headers: mutationHeaders({}, requestWorkspace),
    body: formData,
    signal: options.signal,
    credentials: "include",
  });
  return readResponse(response);
}

export async function apiPut(path, body, options = {}) {
  const response = await fetch(`${apiUrl}${path}`, {
    method: "PUT",
    headers: mutationHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(body),
    signal: options.signal,
    credentials: "include",
  });
  return readResponse(response);
}

export async function apiDelete(path, options = {}) {
  const response = await fetch(`${apiUrl}${path}`, {
    method: "DELETE",
    headers: mutationHeaders(),
    signal: options.signal,
    credentials: "include",
  });
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
  const response = await fetch(`${apiUrl}${path}`, { signal: options.signal, credentials: "include" });
  if (!response.ok) {
    return readResponse(response);
  }
  if (saveHandle && response.body && typeof response.body.pipeTo === "function") {
    const writable = await saveHandle.createWritable();
    await response.body.pipeTo(writable, { signal: options.signal });
    return { saved: true, method: "picker" };
  }
  const blob = await readBufferedDownload(response);
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
  if (!response.ok) {
    let data = null;
    let parseError = null;
    try {
      data = parseResponseBody(text, options);
    } catch (error) {
      parseError = error;
    }
    if (response.status === 401 && data?.error === "ui session required" && typeof window !== "undefined") {
      window.dispatchEvent(new CustomEvent("aipermission:ui-session-required"));
    }
    throw new APIError(data?.error || parseError?.message || `Request failed with ${response.status}`, {
      status: response.status,
      code: data?.code || data?.status || "",
      details: data?.details || null,
      data,
    });
  }
  const data = parseResponseBody(text, options);
  if (options.captureWorkspace !== false) captureWorkspaceBinding(response);
  return data;
}

function parseResponseBody(text) {
  if (!text) {
    throw new Error("Empty JSON response from gateway.");
  }
  try {
    return JSON.parse(text);
  } catch {
    if (looksLikeHTML(text)) throw new Error("Gateway returned HTML instead of JSON.");
    throw new Error("Invalid JSON response from gateway.");
  }
}

function looksLikeHTML(text) {
  return text.trimStart().startsWith("<");
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

function boundedReadSignal(parent, timeoutMs) {
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) return { signal: parent, timedOut: () => false, cleanup: () => {} };
  const controller = new AbortController();
  let timeoutReached = false;
  const abortFromParent = () => controller.abort(parent?.reason);
  if (parent?.aborted) abortFromParent();
  else parent?.addEventListener("abort", abortFromParent, { once: true });
  const timer = setTimeout(() => {
    timeoutReached = true;
    controller.abort();
  }, timeoutMs);
  return {
    signal: controller.signal,
    timedOut: () => timeoutReached,
    cleanup: () => {
      clearTimeout(timer);
      parent?.removeEventListener("abort", abortFromParent);
    },
  };
}

function csrfHeaders(base = {}) {
  const token = readCookie(scopedUICookieName("aipermission_csrf"));
  if (!token) return base;
  return { ...base, "X-AIPermission-CSRF": token };
}

function mutationHeaders(base = {}, requestWorkspace = currentWorkspaceBinding()) {
  const headers = csrfHeaders(base);
  if (!requestWorkspace) return headers;
  return { ...headers, [workspaceHeaderName]: requestWorkspace };
}

export function currentWorkspaceBinding() {
  synchronizeWorkspaceBindingOwner();
  if (!workspaceBinding) workspaceBinding = readCookie(scopedUICookieName("aipermission_workspace"));
  return workspaceBinding;
}

function captureWorkspaceBinding(response) {
  synchronizeWorkspaceBindingOwner();
  if (!response?.headers?.has?.(workspaceHeaderName)) return;
  const changed = response.headers.get(workspaceChangedHeaderName) === "true";
  if (!workspaceBinding || changed) {
    workspaceBinding = String(response.headers.get(workspaceHeaderName) || "").trim();
  }
}

function synchronizeWorkspaceBindingOwner() {
  const owner = typeof window === "undefined" ? null : window;
  if (owner === workspaceBindingOwner) return;
  workspaceBindingOwner = owner;
  workspaceBinding = "";
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
