const terminalBatchStatuses = new Set(["completed", "failed", "canceled", "declined", "stale", "error"]);
export type FileTransferListItem = {
  id: number; status: string; remote_path?: string; file_name?: string; size_bytes?: number;
};
export type FileTransferListBatch = {
  id: number; status: string; direction?: string; source?: string; target_name?: string; runtime_id?: string | number;
  completed_items?: number; canceled_items?: number; failed_items?: number; total_items?: number;
  size_bytes?: number; transferred_bytes?: number; bytes_per_second?: number; eta_seconds?: number; error?: string;
  items?: FileTransferListItem[];
  [field: string]: unknown;
};
export type FileTransferListState = { state: string; data: FileTransferListBatch[]; error: string | null };

export function fileTransferListBatchResponse(value: unknown): FileTransferListBatch {
  if (!validBatch(value)) {
    throw new Error("Invalid transfer list batch response from gateway.");
  }
  return { ...value, id: value.id, status: value.status };
}

function objectRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function optionalFields(value: Record<string, unknown>, strings: string[], numbers: string[]) {
  return strings.every((key) => value[key] === undefined || typeof value[key] === "string") &&
    numbers.every((key) => value[key] === undefined || (typeof value[key] === "number" && Number.isFinite(value[key])));
}

function validBatch(value: unknown): value is FileTransferListBatch {
  if (!objectRecord(value) || !validIdentity(value)) return false;
  if (!optionalFields(value, ["direction", "source", "target_name", "error"],
    ["completed_items", "canceled_items", "failed_items", "total_items", "size_bytes", "transferred_bytes", "bytes_per_second", "eta_seconds"])) return false;
  if (value.runtime_id !== undefined && typeof value.runtime_id !== "string" && typeof value.runtime_id !== "number") return false;
  return value.items === undefined || (Array.isArray(value.items) && value.items.every((item: unknown) =>
    objectRecord(item) && validIdentity(item) && optionalFields(item, ["remote_path", "file_name"], ["size_bytes"])));
}

function validIdentity(value: Record<string, unknown>) {
  return typeof value.id === "number" && Number.isSafeInteger(value.id) && value.id > 0 && typeof value.status === "string";
}

export function fileTransferListResponse(value: unknown): { items: FileTransferListBatch[] } {
  if (!value || typeof value !== "object" || Array.isArray(value) || !("items" in value) || !Array.isArray(value.items)) throw new Error("Invalid transfer list response from gateway.");
  return { items: value.items.map(fileTransferListBatchResponse) };
}

export function createFileTransferListState() {
  let generation = 0;
  return {
    beginRequest() {
      generation += 1;
      return generation;
    },
    isCurrent(requestGeneration: number) {
      return requestGeneration === generation;
    },
    applyBatch(current: FileTransferListState, batch: FileTransferListBatch): FileTransferListState {
      generation += 1;
      const data = [...current.data];
      const index = data.findIndex((item) => Number(item.id) === Number(batch.id));
      if (index === -1) data.unshift(batch);
      else if (!terminalBatchStatuses.has(data[index].status) || terminalBatchStatuses.has(batch.status)) {
        data[index] = { ...data[index], ...batch };
      }
      return { state: "ready", data, error: null };
    },
  };
}

export async function loadCurrentFileTransferBatches({
  request,
  pollGeneration,
  pollIsCurrent,
  listState,
  onItems,
  onError,
}: {
  request: () => Promise<{ items?: FileTransferListBatch[] } | null | undefined>;
  pollGeneration?: number;
  pollIsCurrent: (_generation: number | undefined) => boolean;
  listState: ReturnType<typeof createFileTransferListState>;
  onItems: (_items: FileTransferListBatch[]) => unknown;
  onError: (_error: unknown) => unknown;
}) {
  const listGeneration = listState.beginRequest();
  try {
    const data = await request();
    if (!pollIsCurrent(pollGeneration) || !listState.isCurrent(listGeneration)) return [];
    const items = data?.items || [];
    onItems(items);
    return items;
  } catch (error) {
    if (!pollIsCurrent(pollGeneration) || !listState.isCurrent(listGeneration)) return [];
    onError(error);
    return [];
  }
}
