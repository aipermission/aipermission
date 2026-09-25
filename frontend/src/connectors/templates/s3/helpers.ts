export function fileToBase64(file: Blob, options: { signal?: AbortSignal } = {}): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    const signal = options.signal;
    const abort = () => {
      if (reader.readyState === FileReader.LOADING) reader.abort();
      reject(new DOMException("File read aborted.", "AbortError"));
    };
    if (signal?.aborted) {
      abort();
      return;
    }
    signal?.addEventListener("abort", abort, { once: true });
    const finish = (callback: () => void) => {
      signal?.removeEventListener("abort", abort);
      callback();
    };
    reader.onload = () => {
      const result = String(reader.result || "");
      finish(() => resolve(result.includes(",") ? (result.split(",").pop() ?? "") : result));
    };
    reader.onerror = () => finish(() => reject(reader.error || new Error("Failed to read file.")));
    reader.onabort = () => finish(() => reject(new DOMException("File read aborted.", "AbortError")));
    reader.readAsDataURL(file);
  });
}

export function base64Blob(value: string, contentType?: string): Blob {
  const binary = atob(value || "");
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return new Blob([bytes], { type: contentType || "application/octet-stream" });
}

export function filenameFromKey(key: string | null | undefined): string {
  const parts = String(key || "s3-object")
    .split("/")
    .filter(Boolean);
  return parts[parts.length - 1] || "s3-object";
}

export function safeDownloadName(value: string | null | undefined): string {
  return String(value || "s3-object").replaceAll(":", "-");
}

export function approvalsForTarget<Item extends { target_ref: string }>(items: Item[] | null | undefined, targetRef: string): Item[] {
  return (items || []).filter((item) => item.target_ref === targetRef);
}

export function visibleObjectBytes(objects: { size?: number | string | null }[] | null | undefined): number {
  return (objects || []).reduce((total, object) => total + Number(object.size || 0), 0);
}

export function normalizeObjectKey(value: string | null | undefined): string {
  return String(value ?? "");
}

export function joinObjectKey(prefix: string, name: string): string {
  const cleanPrefix = normalizeObjectKey(prefix);
  const cleanName = normalizeObjectKey(name);
  if (!cleanPrefix) return cleanName;
  return `${cleanPrefix}${cleanPrefix.endsWith("/") ? "" : "/"}${cleanName}`;
}

export function parentPrefix(value: string): string {
  const clean = normalizeObjectKey(value).replace(/\/$/, "");
  const index = clean.lastIndexOf("/");
  if (index < 0) return "";
  return `${clean.slice(0, index)}/`;
}

export function shortDate(value: string | null | undefined): string {
  if (!value) return "unknown";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

export function restoreDestinationGuard(
  metadata: { output?: { etag?: string | null } } | null,
  error?: unknown,
): { expected_current_etag: string } | { expected_current_absent: true } {
  const etag = String(metadata?.output?.etag || "").trim();
  if (etag) return { expected_current_etag: etag };
  if (isNotFoundActionError(error)) return { expected_current_absent: true };
  if (error) throw error;
  throw new Error("Current object ETag could not be read; restore was not started.");
}

function isNotFoundActionError(error: unknown): boolean {
  if (!error || typeof error !== "object" || !("actionItem" in error)) return false;
  const item = error.actionItem;
  if (!item || typeof item !== "object" || !("output" in item)) return false;
  const output = item.output;
  return Boolean(output && typeof output === "object" && "code" in output && output.code === "not_found");
}
