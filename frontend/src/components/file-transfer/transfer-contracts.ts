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
