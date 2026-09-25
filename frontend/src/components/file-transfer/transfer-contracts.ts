export type TransferDirection = "upload" | "download";
export type TransferStatus = "pending" | "pending_approval" | "running" | "paused" | "completed" | "failed" | "canceled";
export type TransferBatchItem = {
  id: number;
  status: TransferStatus;
  file_name?: string;
  [field: string]: unknown;
};
export type TransferBatch = {
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
export type RemoteEntry = { type: "file" | "directory"; path: string; name: string; size?: number };

const statuses: ReadonlySet<string> = new Set(["pending", "pending_approval", "running", "paused", "completed", "failed", "canceled"]);

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
    (batch.items !== undefined && !Array.isArray(batch.items))
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

export function remoteExpansionEntries(value: unknown): RemoteEntry[] {
  if (!value || typeof value !== "object" || Array.isArray(value) || !("entries" in value) || !Array.isArray(value.entries)) {
    throw new Error("Invalid remote folder response from gateway.");
  }
  const entries = value.entries as unknown[];
  for (const entry of entries) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) throw new Error("Invalid remote folder entry from gateway.");
    const item = entry as Record<string, unknown>;
    if (
      (item.type !== "file" && item.type !== "directory") ||
      typeof item.path !== "string" ||
      !item.path.startsWith("/") ||
      typeof item.name !== "string" ||
      (item.size !== undefined && (typeof item.size !== "number" || !Number.isSafeInteger(item.size) || item.size < 0))
    ) {
      throw new Error("Invalid remote folder entry from gateway.");
    }
  }
  return entries as RemoteEntry[];
}
