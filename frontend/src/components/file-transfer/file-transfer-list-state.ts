const terminalBatchStatuses = new Set(["completed", "failed", "canceled", "declined", "stale", "error"]);
export type FileTransferListBatch = { id: number; status: string; [field: string]: unknown };
export type FileTransferListState = { state: string; data: FileTransferListBatch[]; error: string | null };

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
  pollGeneration: number;
  pollIsCurrent: (_generation: number) => boolean;
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
