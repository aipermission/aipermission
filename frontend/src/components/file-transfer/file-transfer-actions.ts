type BatchAction = "approve" | "cancel" | "decline" | "pause" | "resume";
type BatchActionOptions<T> = {
  post: (_path: string, _body: Record<string, unknown>) => Promise<T>;
  applyResult: (_result: T) => void;
  refresh: (_options: { keepData: true }) => Promise<unknown> | unknown;
};

const batchActionPaths: Record<string, (_batchID: number) => string> = {
  approve: (batchID) => `/api/file-transfer-batches/${batchID}/approve`,
  cancel: (batchID) => `/api/file-transfer-batches/${batchID}/cancel`,
  decline: (batchID) => `/api/file-transfer-batches/${batchID}/decline`,
  pause: (batchID) => `/api/file-transfer-batches/${batchID}/pause`,
  resume: (batchID) => `/api/file-transfer-batches/${batchID}/resume`,
};

export async function runFileTransferBatchAction<T>({
  action,
  batchID,
  body = {},
  post,
  applyResult,
  refresh,
}: BatchActionOptions<T> & { action: string; batchID: number; body?: Record<string, unknown> }): Promise<T> {
  const pathForAction = batchActionPaths[action];
  if (!pathForAction) throw new Error(`unsupported file transfer batch action: ${action}`);

  const result = await post(pathForAction(batchID), body);
  applyResult(result);
  await refresh({ keepData: true });
  return result;
}

export function createFileTransferBatchActions<T>({ post, applyResult, refresh }: BatchActionOptions<T>) {
  const run = (action: BatchAction, batchID: number, body?: Record<string, unknown>) =>
    runFileTransferBatchAction({ action, batchID, body, post, applyResult, refresh });
  return {
    approve: (batchID: number, itemIDs: number[], note = "") => run("approve", batchID, { item_ids: itemIDs, note }),
    cancel: (batchID: number) => run("cancel", batchID),
    decline: (batchID: number, note = "") => run("decline", batchID, { note }),
    pause: (batchID: number) => run("pause", batchID),
    resume: (batchID: number) => run("resume", batchID),
  };
}
