const batchActionPaths = {
  approve: (batchID) => `/api/file-transfer-batches/${batchID}/approve`,
  cancel: (batchID) => `/api/file-transfer-batches/${batchID}/cancel`,
  decline: (batchID) => `/api/file-transfer-batches/${batchID}/decline`,
  pause: (batchID) => `/api/file-transfer-batches/${batchID}/pause`,
  resume: (batchID) => `/api/file-transfer-batches/${batchID}/resume`,
};

export async function runFileTransferBatchAction({ action, batchID, body = {}, post, applyResult, refresh }) {
  const pathForAction = batchActionPaths[action];
  if (!pathForAction) throw new Error(`unsupported file transfer batch action: ${action}`);

  const result = await post(pathForAction(batchID), body);
  applyResult(result);
  await refresh({ keepData: true });
  return result;
}

export function createFileTransferBatchActions({ post, applyResult, refresh }) {
  const run = (action, batchID, body) => runFileTransferBatchAction({ action, batchID, body, post, applyResult, refresh });
  return {
    approve: (batchID, itemIDs, note = "") => run("approve", batchID, { item_ids: itemIDs, note }),
    cancel: (batchID) => run("cancel", batchID),
    decline: (batchID, note = "") => run("decline", batchID, { note }),
    pause: (batchID) => run("pause", batchID),
    resume: (batchID) => run("resume", batchID),
  };
}
