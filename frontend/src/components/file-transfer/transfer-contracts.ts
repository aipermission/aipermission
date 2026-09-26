export type TransferDirection = "upload" | "download";
export type TransferStatus = "pending" | "pending_approval" | "running" | "paused" | "completed" | "failed" | "canceled";
type TransferFailure = { failure_kind?: string; error?: string | null };
export type TransferBatchItem = {
  id: number;
  status: TransferStatus;
  file_name?: string;
  [field: string]: unknown;
};
export type TransferBatch = TransferFailure & {
  id: number;
  status: TransferStatus;
  direction: TransferDirection;
  items?: TransferBatchItem[];
  archive_name?: string;
  size_bytes?: number;
  transferred_bytes?: number;
  [field: string]: unknown;
};
export type TransferBatchState = { state: string; item: TransferBatch | null; error: string | null };
export type RemoteEntry = { type: "file" | "directory" | "other"; path: string; name: string; size?: number; modified_at?: string };
export type RemoteBrowserData = { entries: RemoteEntry[]; path?: string; parent?: string; has_more?: boolean; next_cursor?: string };
export type RemoteBrowserState = { open: boolean; purpose: TransferDirection; path: string; state: string; data: RemoteBrowserData | null; error: string | null };
export type RemoteBrowserOptions = { append?: boolean; cursor?: string; fallbackToDefault?: boolean };

const statuses: ReadonlySet<string> = new Set(["pending", "pending_approval", "running", "paused", "completed", "failed", "canceled"]);

function validTransferFailure(value: Record<string, unknown>) {
  return (value.failure_kind === undefined || typeof value.failure_kind === "string") &&
    (value.error === undefined || value.error === null || typeof value.error === "string");
}

export function transferBatchResponse(value: unknown): TransferBatch {
  const invalid = () => new Error("Invalid transfer batch response from gateway.");
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  const batch = value as Record<string, unknown>;
  if (
    !Number.isSafeInteger(batch.id) ||
    Number(batch.id) <= 0 ||
    !statuses.has(String(batch.status)) ||
    (batch.direction !== "upload" && batch.direction !== "download") ||
    (batch.archive_name !== undefined && typeof batch.archive_name !== "string") ||
    (batch.items !== undefined && !Array.isArray(batch.items)) || !validTransferFailure(batch)
  )
    throw invalid();
  if (Array.isArray(batch.items)) {
    for (const entry of batch.items) {
      if (!entry || typeof entry !== "object" || Array.isArray(entry)) throw invalid();
      const item = entry as Record<string, unknown>;
      if (
        !Number.isSafeInteger(item.id) ||
        Number(item.id) <= 0 ||
        !statuses.has(String(item.status)) ||
        (item.file_name !== undefined && typeof item.file_name !== "string")
      )
        throw invalid();
    }
  }
  return batch as TransferBatch;
}

function remoteEntries(value: unknown, allowOther: boolean): RemoteEntry[] {
  if (!value || typeof value !== "object" || Array.isArray(value) || !("entries" in value) || !Array.isArray(value.entries)) {
    throw new Error("Invalid remote folder response from gateway.");
  }
  const entries = value.entries as unknown[];
  for (const entry of entries) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) throw new Error("Invalid remote folder entry from gateway.");
    const item = entry as Record<string, unknown>;
    if (
      (item.type !== "file" && item.type !== "directory" && !(allowOther && item.type === "other")) ||
      typeof item.path !== "string" ||
      !item.path.startsWith("/") ||
      typeof item.name !== "string" ||
      (item.size !== undefined && (typeof item.size !== "number" || !Number.isSafeInteger(item.size) || item.size < 0)) ||
      (item.modified_at !== undefined && typeof item.modified_at !== "string")
    ) {
      throw new Error("Invalid remote folder entry from gateway.");
    }
  }
  return entries as RemoteEntry[];
}

export function remoteExpansionEntries(value: unknown): RemoteEntry[] {
  return remoteEntries(value, false);
}

export function remoteBrowserResponse(value: unknown): RemoteBrowserData {
  const entries = remoteEntries(value, true);
  const data = value as Record<string, unknown>;
  for (const field of ["path", "parent", "next_cursor"] as const) {
    if (field in data && typeof data[field] !== "string") throw new Error("Invalid remote browser response from gateway.");
  }
  if ("has_more" in data && typeof data.has_more !== "boolean") throw new Error("Invalid remote browser response from gateway.");
  return { ...data, entries };
}
