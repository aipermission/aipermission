import {
  completeLocalActionRetry,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  preserveLocalActionRetryAttempt,
  releaseLocalActionRetryAttempt,
  retireLocalActionRetryAttempt,
} from "./local-action-retry.ts";
import { APIError } from "./errors.ts";
import { assertConnectorActionResponse, isPendingConnectorActionStatus } from "./gateway-contracts/connector-action-contract.ts";
import { scopedUICookieName } from "./ui-cookie.ts";
import { readBufferedDownload } from "./downloads/download-buffer.ts";
import { nativeSaveFilePicker, objectRecord } from "./api-types.ts";
import type {
  APIOptions,
  DownloadOptions,
  DownloadResult,
  NativeFileWriter,
  NativeSaveHandle,
  PostPolicy,
  PreparedPost,
} from "./api-types";
import type { PreparedRetry } from "./local-action-retry/records";
import { observeLocalActionRetryResponse } from "./local-action-retry/observations";

const viteEnv = import.meta.env || {};
const workspaceHeaderName = "X-AIPermission-Workspace";
const workspaceChangedHeaderName = "X-AIPermission-Workspace-Changed";
let workspaceBinding = "";
let workspaceBindingOwner: Window | null = null;

export const apiUrl = viteEnv.VITE_API_URL === undefined ? "http://localhost:8080" : normalizeApiUrl(viteEnv.VITE_API_URL);
export const mcpApiUrl = normalizeApiUrl(viteEnv.VITE_MCP_API_URL || browserOrigin());

export async function apiGet(path: string, options: APIOptions = {}): Promise<unknown> {
  const requestWorkspace = options.workspaceBinding || currentWorkspaceBinding();
  const request = boundedReadSignal(options.signal, options.timeoutMs);
  try {
    const response = await fetch(`${apiUrl}${path}`, {
      signal: request.signal,
      credentials: "include",
      ...(options.workspaceBinding ? { headers: workspaceHeaders({}, requestWorkspace) } : {}),
    });
    const data = await readResponse(response, { captureWorkspace: !options.workspaceBinding });
    if (options.workspaceBinding && response.headers.get(workspaceHeaderName) !== requestWorkspace)
      throw new Error("Gateway read workspace binding mismatch.");
    if (requestWorkspace && response.headers.get(workspaceHeaderName) === requestWorkspace)
      await observeLocalActionRetryResponse(path, data, requestWorkspace);
    return data;
  } catch (error) {
    if (request.timedOut()) throw new Error(`Gateway read timed out after ${options.timeoutMs}ms.`, { cause: error });
    throw error;
  } finally {
    request.cleanup();
  }
}

export async function apiPost(path: string, body: Record<string, unknown>, options: APIOptions = {}): Promise<unknown> {
  const requestWorkspace = currentWorkspaceBinding();
  const prepared = await preparePostBody(path, body, requestWorkspace, options.exclusiveMutationActions);
  let finalized = false;
  try {
    const response = await fetch(`${apiUrl}${path}`, {
      method: "POST",
      headers: mutationHeaders({ "Content-Type": "application/json" }, requestWorkspace),
      body: JSON.stringify(prepared.body),
      signal: options.signal,
      credentials: "include",
    });
    let data: unknown;
    try {
      data = await readResponse(response);
    } catch (error) {
      finalized = await finalizePostError(prepared, error, response);
      throw error;
    }
    if (response.ok && prepared.acknowledged && !prepared.acknowledged(data)) {
      throw new Error(prepared.invalidResponseMessage);
    }
    if (prepared.retry && response.ok && prepared.pending?.(data)) {
      await preserveLocalActionRetryAttempt(prepared.retry, data);
      finalized = true;
    } else if (prepared.retry && response.ok && objectRecord(data)?.status !== "outcome_unknown") {
      const requestID = objectRecord(data)?.request_id;
      await completeLocalActionRetry(prepared.retry, true, typeof requestID === "number" ? requestID : undefined);
      finalized = true;
    }
    if (prepared.retry && response.ok && objectRecord(data)?.status === "outcome_unknown") {
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

async function finalizePostError(prepared: PreparedPost, error: unknown, response: Response) {
  const retry = prepared.retry;
  if (!retry) return false;
  if (prepared.retireOnError?.(error)) {
    await retireLocalActionRetryAttempt(retry);
    return true;
  }
  if (error instanceof APIError && objectRecord(error.data)?.status === "outcome_unknown") {
    await markLocalActionRetryOutcome(retry, error.data);
    return true;
  }
  if (!retry.reused && response.status >= 400 && response.status < 500) {
    // A fresh gateway 4xx is definitive unless another attempt already owns the identity.
    await completeLocalActionRetry(retry);
    return true;
  }
  return false;
}

async function preserveRetryAfterFailure(retry: PreparedRetry | null, finalized: boolean) {
  if (!retry || finalized) return finalized;
  await preserveLocalActionRetryAttempt(retry);
  return true;
}

function isAcknowledgedLocalActionResponse(data: unknown, body: unknown) {
  try {
    const request = objectRecord(body);
    assertConnectorActionResponse(data, {
      targetRef: typeof request?.target_ref === "string" ? request.target_ref : "",
      actionName: typeof request?.action_name === "string" ? request.action_name : "",
    });
    return true;
  } catch {
    return false;
  }
}

async function preparePostBody(
  path: string,
  body: unknown,
  workspaceID: string,
  exclusiveMutationActions?: readonly string[],
): Promise<PreparedPost> {
  const policy = idempotentPostPolicy(path, body);
  if (!policy) return { body, retry: null, invalidResponseMessage: "" };
  if (objectRecord(body)?.idempotency_key) return { body, retry: null, ...policy };
  const retry = await prepareLocalActionRetry({ path, body: body || {} }, { workspaceID, exclusiveMutationActions });
  return { body: { ...objectRecord(body), idempotency_key: retry.idempotencyKey }, retry, ...policy };
}

function idempotentPostPolicy(path: string, body: unknown): PostPolicy | null {
  if (path === "/api/connector-actions/local-run") {
    return {
      acknowledged: (data) => isAcknowledgedLocalActionResponse(data, body),
      pending: (data) => isPendingConnectorActionStatus(objectRecord(data)?.status),
      invalidResponseMessage: "Invalid connector action response from gateway.",
    };
  }
  if (path === "/api/console/bulk-exec") {
    return { acknowledged: isAcknowledgedBulkCommandResponse, invalidResponseMessage: "Invalid bulk command response from gateway." };
  }
  if (/^\/api\/backup\/providers\/\d+\/upload$/.test(path)) {
    return {
      acknowledged: isAcknowledgedBackupUploadResponse,
      retireOnError: (error) => error instanceof APIError && error.status === 410 && error.code === "operation_expired",
      invalidResponseMessage: "Invalid backup upload response from gateway.",
    };
  }
  return null;
}

function isAcknowledgedBackupUploadResponse(value: unknown) {
  const data = objectRecord(value);
  return (
    data !== null &&
    typeof data === "object" &&
    typeof data.id === "number" &&
    Number.isSafeInteger(data.id) &&
    data.id > 0 &&
    typeof data.provider_file_id === "string" &&
    data.provider_file_id.length > 0
  );
}

function isAcknowledgedBulkCommandResponse(value: unknown) {
  const data = objectRecord(value);
  return (
    data !== null &&
    typeof data === "object" &&
    typeof data.parallelism === "number" &&
    Number.isSafeInteger(data.parallelism) &&
    data.parallelism > 0 &&
    Array.isArray(data.items) &&
    data.items.length > 0 &&
    data.items.every((item: unknown) => {
      const request = objectRecord(item);
      return typeof request?.request_id === "number" && Number.isSafeInteger(request.request_id) && request.request_id > 0;
    })
  );
}

export async function apiPostForm(path: string, formData: FormData, options: APIOptions = {}): Promise<unknown> {
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

export async function apiPut(path: string, body: unknown, options: APIOptions = {}): Promise<unknown> {
  const response = await fetch(`${apiUrl}${path}`, {
    method: "PUT",
    headers: mutationHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(body),
    signal: options.signal,
    credentials: "include",
  });
  return readResponse(response);
}

export async function apiDelete(path: string, options: APIOptions = {}): Promise<unknown> {
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

export async function apiDownload(path: string, filename: string, options: DownloadOptions = {}): Promise<DownloadResult> {
  const requestWorkspace = currentWorkspaceBinding();
  const safeFilename = filename.replaceAll(":", "-");
  let saveHandle: NativeSaveHandle | null = null;
  const picker = nativeSaveFilePicker();
  const pickerAvailable = picker !== null;
  if (options.requireStreaming && !pickerAvailable) {
    throw new Error("This download requires a browser with a streaming Save dialog.");
  }
  if ((options.picker || options.requireStreaming) && pickerAvailable) {
    try {
      saveHandle = await picker({ suggestedName: safeFilename });
    } catch (error) {
      if (objectRecord(error)?.name === "AbortError") {
        return { saved: false, canceled: true, method: "picker" };
      }
      throw error;
    }
  }
  const response = await fetch(`${apiUrl}${path}`, {
    headers: workspaceHeaders({}, requestWorkspace),
    signal: options.signal,
    credentials: "include",
  });
  if (!response.ok) {
    await readResponse(response, { captureWorkspace: false });
    throw new Error("Download failed.");
  }
  captureWorkspaceBinding(response);
  if (saveHandle && response.body && typeof response.body.pipeTo === "function") {
    let writable: NativeFileWriter | null = null;
    try {
      writable = await saveHandle.createWritable();
      await response.body.pipeTo(writable, { signal: options.signal });
      return { saved: true, method: "picker" };
    } catch (error) {
      await abortDownloadResources(response.body, writable, error);
      throw error;
    }
  }
  if (options.requireStreaming) {
    try {
      await response.body?.cancel?.();
    } catch {
      // Preserve the actionable compatibility error when response cancellation fails.
    }
    throw new Error("This browser cannot stream this download to the selected file. Try a current Chromium-based browser.");
  }
  const blob = await readBufferedDownload(response);
  if (saveHandle) {
    let writable: NativeFileWriter | null = null;
    try {
      writable = await saveHandle.createWritable();
      await writable.write(blob);
      await writable.close();
      return { saved: true, method: "picker" };
    } catch (error) {
      await abortDownloadResources(null, writable, error);
      throw error;
    }
  }
  return saveBlob(blob, safeFilename, { ...options, picker: false });
}

async function abortDownloadResources(body: ReadableStream<Uint8Array> | null, writable: NativeFileWriter | null, reason: unknown) {
  try {
    await writable?.abort?.(reason);
  } catch {
    // Keep the original download failure.
  }
  try {
    await body?.cancel?.(reason);
  } catch {
    // The stream may already be closed or locked by pipeTo.
  }
}

async function readResponse(response: Response, options: { captureWorkspace?: boolean } = {}): Promise<unknown> {
  const text = await response.text();
  if (!response.ok) {
    let data: unknown = null;
    let parseError: unknown = null;
    try {
      data = parseResponseBody(text);
    } catch (error) {
      parseError = error;
    }
    const failure = objectRecord(data);
    if (response.status === 401 && failure?.error === "ui session required" && typeof window !== "undefined") {
      window.dispatchEvent(new CustomEvent("aipermission:ui-session-required"));
    }
    throw new APIError(
      typeof failure?.error === "string" && failure.error
        ? failure.error
        : parseError instanceof Error
          ? parseError.message
          : `Request failed with ${response.status}`,
      {
        status: response.status,
        code: typeof failure?.code === "string" ? failure.code : typeof failure?.status === "string" ? failure.status : "",
        details: failure?.details || null,
        data,
      },
    );
  }
  const data = parseResponseBody(text);
  if (options.captureWorkspace !== false) captureWorkspaceBinding(response);
  return data;
}

function parseResponseBody(text: string): unknown {
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

function looksLikeHTML(text: string) {
  return text.trimStart().startsWith("<");
}

function normalizeApiUrl(value: unknown) {
  const trimmed = String(value || "").replace(/\/+$/, "");
  return trimmed;
}

function browserOrigin() {
  if (typeof window !== "undefined" && window.location?.origin) {
    return window.location.origin;
  }
  return "http://localhost:3210";
}

function boundedReadSignal(parent: AbortSignal | undefined, timeoutMs: number | undefined) {
  if (typeof timeoutMs !== "number" || !Number.isFinite(timeoutMs) || timeoutMs <= 0)
    return { signal: parent, timedOut: () => false, cleanup: () => {} };
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

function csrfHeaders(base: Record<string, string> = {}) {
  const token = readCookie(scopedUICookieName("aipermission_csrf"));
  if (!token) return base;
  return { ...base, "X-AIPermission-CSRF": token };
}

function mutationHeaders(base: Record<string, string> = {}, requestWorkspace = currentWorkspaceBinding()) {
  return workspaceHeaders(csrfHeaders(base), requestWorkspace);
}

function workspaceHeaders(base: Record<string, string> = {}, requestWorkspace = currentWorkspaceBinding()) {
  const headers = base;
  if (!requestWorkspace) return headers;
  return { ...headers, [workspaceHeaderName]: requestWorkspace };
}

export function currentWorkspaceBinding() {
  synchronizeWorkspaceBindingOwner();
  if (!workspaceBinding) workspaceBinding = readCookie(scopedUICookieName("aipermission_workspace"));
  return workspaceBinding;
}

function captureWorkspaceBinding(response: Response) {
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

function readCookie(name: string) {
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

export function downloadBlob(blob: Blob, filename: string): DownloadResult {
  const safeFilename = filename.replaceAll(":", "-");
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = safeFilename;
  link.click();
  URL.revokeObjectURL(url);
  return { saved: true, method: "anchor" };
}

export async function saveBlob(blob: Blob, filename: string, options: DownloadOptions = {}): Promise<DownloadResult> {
  const safeFilename = filename.replaceAll(":", "-");
  const picker = nativeSaveFilePicker();
  if (options.picker && picker) {
    try {
      const handle = await picker({ suggestedName: safeFilename });
      const writable = await handle.createWritable();
      await writable.write(blob);
      await writable.close();
      return { saved: true, method: "picker" };
    } catch (error) {
      if (objectRecord(error)?.name === "AbortError") {
        return { saved: false, canceled: true, method: "picker" };
      }
      throw error;
    }
  }
  return downloadBlob(blob, safeFilename);
}

export function downloadJSON(value: unknown, filename: string) {
  const blob = new Blob([JSON.stringify(value, null, 2)], { type: "application/json" });
  downloadBlob(blob, filename);
}
