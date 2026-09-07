import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { apiGet, apiPost, apiPostForm } from "../../lib/api";
import { pendingBatchItemIDs, suggestedArchiveName, transferProgress } from "../../lib/file-transfer-utils";

export const emptyBatchState = { state: "idle", item: null, error: null };

export function useTransferBatch({ open, runtimeTarget, mode, remoteDir, uploadQueue, downloadQueue, queue, onNotice, onUploadCompleted }) {
  const [batch, setBatch] = useState(emptyBatchState);
  const [overwritePrompt, setOverwritePrompt] = useState(null);
  const completedUploadRef = useRef(0);
  const refreshRequestRef = useRef(0);
  const progress = useMemo(() => transferProgress(batch.item), [batch.item]);
  const activeBatch = batch.item && ["pending", "running", "paused"].includes(batch.item.status);
  const canStart = runtimeTarget && queue.length > 0 && !batch.item && batch.state !== "starting";
  const batchID = batch.item?.id;
  const batchStatus = batch.item?.status;
  const refreshBatchForEffect = useEffectEvent((id, options) => refreshBatch(id, options));
  const publishUploadCompletion = useEffectEvent(() => {
    if (batch.item?.status !== "completed" || batch.item.direction !== "upload") return;
    onNotice({ tone: "good", message: "Upload queue completed. Review the summary, then clear when ready." });
    if (completedUploadRef.current !== batch.item.id) {
      completedUploadRef.current = batch.item.id;
      void onUploadCompleted?.();
    }
  });

  useEffect(() => {
    if (!open || batch.state !== "ready" || !batchID || !["pending", "running", "paused"].includes(batchStatus)) return undefined;
    let stopped = false;
    let timer = null;
    async function poll() {
      await refreshBatchForEffect(batchID, { silent: true });
      if (!stopped) timer = window.setTimeout(poll, 900);
    }
    timer = window.setTimeout(poll, 900);
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, [open, batchID, batchStatus, batch.state]);

  useEffect(
    () => () => {
      refreshRequestRef.current += 1;
    },
    [],
  );

  useEffect(() => {
    publishUploadCompletion();
  }, [batch.item?.id, batch.item?.status, batch.item?.direction]);

  function resetBatch() {
    refreshRequestRef.current += 1;
    setBatch(emptyBatchState);
    setOverwritePrompt(null);
    completedUploadRef.current = 0;
  }

  function clearBatch() {
    setBatch(emptyBatchState);
    setOverwritePrompt(null);
  }

  async function refreshBatch(id = batch.item?.id, options = {}) {
    if (!id) return;
    const requestID = ++refreshRequestRef.current;
    if (!options.silent) setBatch((current) => ({ ...current, state: "loading", error: null }));
    try {
      const item = await apiGet(`/api/file-transfer-batches/${id}`);
      if (requestID !== refreshRequestRef.current) return;
      setBatch((current) => {
        if (options.silent && !["idle", "loading", "ready"].includes(current.state)) return current;
        return { state: "ready", item, error: null };
      });
    } catch (error) {
      if (requestID !== refreshRequestRef.current) return;
      setBatch((current) => {
        if (options.silent && !["idle", "loading", "ready"].includes(current.state)) return current;
        return { ...current, state: "error", error: error.message };
      });
    }
  }

  async function updatePausedBatchQueue(itemIDs) {
    if (!batch.item) return;
    setBatch((current) => ({ ...current, state: "updating", error: null }));
    try {
      const item = await apiPost(`/api/file-transfer-batches/${batch.item.id}/queue`, { item_ids: itemIDs });
      setBatch({ state: "ready", item, error: null });
    } catch (error) {
      setBatch((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  function pausedQueueWithout(id) {
    return pendingBatchItemIDs(batch.item).filter((itemID) => itemID !== Number(id));
  }

  function movePausedQueueItem(id, direction) {
    const ids = pendingBatchItemIDs(batch.item);
    const index = ids.indexOf(Number(id));
    const nextIndex = index + direction;
    if (index < 0 || nextIndex < 0 || nextIndex >= ids.length) return null;
    const next = [...ids];
    [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
    return next;
  }

  async function startQueue(options = {}) {
    if (!runtimeTarget || queue.length === 0) return;
    if (mode === "upload") {
      await startUploadBatch(options);
      return;
    }
    await startDownloadBatch();
  }

  async function startUploadBatch(options = {}) {
    const formData = new FormData();
    formData.append("runtime_id", String(runtimeTarget.id));
    formData.append("remote_dir", remoteDir);
    formData.append("overwrite", options.overwrite ? "true" : "false");
    uploadQueue.forEach((item) => formData.append("files", item.file, item.name));
    formData.append("relative_paths", JSON.stringify(uploadQueue.map((item) => item.relative_path || item.name)));
    onNotice(null);
    setOverwritePrompt(null);
    setBatch({ state: "starting", item: null, error: null });
    try {
      const item = await apiPostForm("/api/file-transfers/upload-batch", formData);
      setBatch({ state: "ready", item, error: null });
    } catch (error) {
      if (error.status === 409 && error.data?.code === "remote_files_exist") {
        setBatch(emptyBatchState);
        setOverwritePrompt(error.data.conflicts || []);
        return;
      }
      setBatch({ state: "error", item: null, error: error.message });
    }
  }

  async function startDownloadBatch() {
    onNotice(null);
    setBatch({ state: "starting", item: null, error: null });
    try {
      const item = await apiPost("/api/file-transfers/download-batch", {
        runtime_id: Number(runtimeTarget.id),
        remote_paths: downloadQueue.map((item) => item.path),
        archive_name: downloadQueue.length > 1 ? suggestedArchiveName() : "",
      });
      setBatch({ state: "ready", item, error: null });
    } catch (error) {
      setBatch({ state: "error", item: null, error: error.message });
    }
  }

  async function transitionBatch(nextState, action, successNotice = null) {
    if (!batch.item) return;
    refreshRequestRef.current += 1;
    setBatch((current) => ({ ...current, state: nextState, error: null }));
    try {
      const item = await apiPost(`/api/file-transfer-batches/${batch.item.id}/${action}`, {});
      setBatch({ state: "ready", item, error: null });
      if (successNotice) onNotice(successNotice);
    } catch (error) {
      setBatch((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  return {
    batch,
    setBatch,
    activeBatch,
    progress,
    canStart,
    overwritePrompt,
    setOverwritePrompt,
    resetBatch,
    clearBatch,
    refreshBatch,
    updatePausedBatchQueue,
    pausedQueueWithout,
    movePausedQueueItem,
    startQueue,
    pauseBatch: () => transitionBatch("pausing", "pause"),
    resumeBatch: () => transitionBatch("resuming", "resume"),
    cancelBatch: () => transitionBatch("canceling", "cancel", { tone: "warn", message: "Transfer queue canceled." }),
  };
}
