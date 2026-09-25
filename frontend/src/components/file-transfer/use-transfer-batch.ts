import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import type { RefObject } from "react";
import { apiGet, apiPost, apiPostForm } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { pendingBatchItemIDs, suggestedArchiveName, transferProgress } from "../../lib/file-transfer-utils";
import { useRequestGuard } from "../../lib/request-guard";
import { transferBatchResponse } from "./transfer-contracts";
import type { TransferBatchState, TransferDirection } from "./transfer-contracts";

type UploadQueueItem = { id: string; name: string; relative_path?: string; file: File };
type DownloadQueueItem = { path: string };
type Notice = { tone: "good" | "warn"; message: string };
type StartAttempt = { signature: string; key: string; archiveName: string };
type Props = {
  open: boolean;
  runtimeTarget: { id: number } | null;
  mode: TransferDirection;
  remoteDir: string;
  uploadQueue: readonly UploadQueueItem[];
  downloadQueue: readonly DownloadQueueItem[];
  queue: readonly unknown[];
  onNotice: (_notice: Notice | null) => void;
  onUploadCompleted?: () => void | Promise<void>;
};

export const emptyBatchState: TransferBatchState = { state: "idle", item: null, error: null };

export function useTransferBatch({
  open,
  runtimeTarget,
  mode,
  remoteDir,
  uploadQueue,
  downloadQueue,
  queue,
  onNotice,
  onUploadCompleted,
}: Props) {
  const [batch, setBatch] = useState<TransferBatchState>(emptyBatchState);
  const [overwritePrompt, setOverwritePrompt] = useState<{ remote_path: string }[] | null>(null);
  const completedUploadRef = useRef(0);
  const startAttemptRef = useRef<StartAttempt | null>(null);
  const requests = useRequestGuard(`transfer-batch:${open ? "open" : "closed"}:${runtimeTarget?.id || "none"}`);
  const progress = useMemo(() => transferProgress(batch.item), [batch.item]);
  const activeBatch = batch.item && ["pending", "running", "paused"].includes(batch.item.status);
  const canStart = runtimeTarget && queue.length > 0 && !batch.item && batch.state !== "starting";
  const batchID = batch.item?.id;
  const batchStatus = batch.item?.status;
  const refreshBatchForEffect = useEffectEvent((id: number, options: { silent?: boolean }) => refreshBatch(id, options));
  const resetBatchForEffect = useEffectEvent(() => resetBatch());
  const publishUploadCompletion = useEffectEvent(() => {
    if (batch.item?.status !== "completed" || batch.item.direction !== "upload") return;
    onNotice({ tone: "good", message: "Upload queue completed. Review the summary, then clear when ready." });
    if (completedUploadRef.current !== batch.item.id) {
      completedUploadRef.current = batch.item.id;
      void onUploadCompleted?.();
    }
  });

  useEffect(() => {
    if (!open || batch.state !== "ready" || !batchID || !batchStatus || !["pending", "running", "paused"].includes(batchStatus))
      return undefined;
    const id = batchID;
    let stopped = false;
    let timer: number | null = null;
    async function poll() {
      await refreshBatchForEffect(id, { silent: true });
      if (!stopped) timer = window.setTimeout(poll, 900);
    }
    timer = window.setTimeout(poll, 900);
    return () => {
      stopped = true;
      if (timer !== null) window.clearTimeout(timer);
    };
  }, [open, batchID, batchStatus, batch.state]);

  useEffect(() => {
    resetBatchForEffect();
  }, [open, runtimeTarget?.id]);

  useEffect(() => {
    publishUploadCompletion();
  }, [batch.item?.id, batch.item?.status, batch.item?.direction]);

  function resetBatch() {
    requests.invalidate("refresh");
    requests.invalidate("mutation");
    setBatch(emptyBatchState);
    setOverwritePrompt(null);
    completedUploadRef.current = 0;
    startAttemptRef.current = null;
  }

  function clearBatch() {
    resetBatch();
  }

  async function refreshBatch(id: number | undefined = batch.item?.id, options: { silent?: boolean } = {}) {
    if (!id) return;
    const request = requests.begin("refresh");
    if (!options.silent) setBatch((current) => ({ ...current, state: "loading", error: null }));
    try {
      const response = await apiGet(`/api/file-transfer-batches/${id}`, { signal: request.signal });
      if (!request.isCurrent()) return;
      const item = transferBatchResponse(response);
      setBatch((current) => {
        if (options.silent && !["idle", "loading", "ready"].includes(current.state)) return current;
        return { state: "ready", item, error: null };
      });
    } catch (error) {
      if (!request.isCurrent()) return;
      setBatch((current) => {
        if (options.silent && !["idle", "loading", "ready"].includes(current.state)) return current;
        return { ...current, state: "error", error: errorMessage(error, "Transfer refresh failed.") };
      });
    } finally {
      request.complete();
    }
  }

  async function updatePausedBatchQueue(itemIDs: number[]) {
    if (!batch.item) return;
    const request = requests.begin("mutation");
    requests.invalidate("refresh");
    const batchID = batch.item.id;
    setBatch((current) => ({ ...current, state: "updating", error: null }));
    try {
      const response = await apiPost(`/api/file-transfer-batches/${batchID}/queue`, { item_ids: itemIDs }, { signal: request.signal });
      if (!request.isCurrent()) return;
      const item = transferBatchResponse(response);
      setBatch({ state: "ready", item, error: null });
    } catch (error) {
      if (!request.isCurrent()) return;
      setBatch((current) => ({ ...current, state: "error", error: errorMessage(error, "Transfer queue update failed.") }));
    } finally {
      request.complete();
    }
  }

  function pausedQueueWithout(id: number | string): number[] {
    const ids: number[] = pendingBatchItemIDs(batch.item);
    return ids.filter((itemID) => itemID !== Number(id));
  }

  function movePausedQueueItem(id: number | string, direction: number): number[] | null {
    const ids: number[] = pendingBatchItemIDs(batch.item);
    const index = ids.indexOf(Number(id));
    const nextIndex = index + direction;
    if (index < 0 || nextIndex < 0 || nextIndex >= ids.length) return null;
    const next = [...ids];
    [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
    return next;
  }

  async function startQueue(options: { overwrite?: boolean } = {}) {
    if (!runtimeTarget || queue.length === 0) return;
    const startMode = mode;
    if (startMode === "upload") {
      await startUploadBatch(options);
      return;
    }
    await startDownloadBatch();
  }

  async function startUploadBatch(options: { overwrite?: boolean } = {}) {
    if (!runtimeTarget) return;
    const request = requests.begin("mutation");
    requests.invalidate("refresh");
    const formData = new FormData();
    formData.append("runtime_id", String(runtimeTarget.id));
    formData.append("remote_dir", remoteDir);
    formData.append("overwrite", options.overwrite ? "true" : "false");
    const attempt = startAttempt(
      startAttemptRef,
      JSON.stringify({
        mode: "upload",
        runtimeID: runtimeTarget.id,
        remoteDir,
        overwrite: Boolean(options.overwrite),
        files: uploadQueue.map((item) => ({
          id: item.id,
          name: item.name,
          relativePath: item.relative_path || item.name,
          size: item.file.size,
        })),
      }),
    );
    formData.append("idempotency_key", attempt.key);
    uploadQueue.forEach((item) => formData.append("files", item.file, item.name));
    formData.append("relative_paths", JSON.stringify(uploadQueue.map((item) => item.relative_path || item.name)));
    onNotice(null);
    setOverwritePrompt(null);
    setBatch({ state: "starting", item: null, error: null });
    try {
      const response = await apiPostForm("/api/file-transfers/upload-batch", formData, { signal: request.signal });
      if (!request.isCurrent()) return;
      const item = transferBatchResponse(response);
      setBatch({ state: "ready", item, error: null });
    } catch (error) {
      if (!request.isCurrent()) return;
      if (isOverwriteConflict(error)) {
        setBatch(emptyBatchState);
        setOverwritePrompt(error.data.conflicts);
        return;
      }
      setBatch({ state: "error", item: null, error: errorMessage(error, "Upload failed.") });
    } finally {
      request.complete();
    }
  }

  async function startDownloadBatch() {
    if (!runtimeTarget) return;
    const request = requests.begin("mutation");
    requests.invalidate("refresh");
    onNotice(null);
    setBatch({ state: "starting", item: null, error: null });
    try {
      const attempt = startAttempt(
        startAttemptRef,
        JSON.stringify({
          mode: "download",
          runtimeID: runtimeTarget.id,
          remotePaths: downloadQueue.map((item) => item.path),
        }),
        () => (downloadQueue.length > 1 ? suggestedArchiveName() : ""),
      );
      const response = await apiPost(
        "/api/file-transfers/download-batch",
        {
          runtime_id: Number(runtimeTarget.id),
          remote_paths: downloadQueue.map((item) => item.path),
          archive_name: attempt.archiveName,
          idempotency_key: attempt.key,
        },
        { signal: request.signal },
      );
      if (!request.isCurrent()) return;
      const item = transferBatchResponse(response);
      setBatch({ state: "ready", item, error: null });
    } catch (error) {
      if (!request.isCurrent()) return;
      setBatch({ state: "error", item: null, error: errorMessage(error, "Download batch failed.") });
    } finally {
      request.complete();
    }
  }

  async function transitionBatch(nextState: string, action: "pause" | "resume" | "cancel", successNotice: Notice | null = null) {
    if (!batch.item) return;
    const request = requests.begin("mutation");
    requests.invalidate("refresh");
    const batchID = batch.item.id;
    setBatch((current) => ({ ...current, state: nextState, error: null }));
    try {
      const response = await apiPost(`/api/file-transfer-batches/${batchID}/${action}`, {}, { signal: request.signal });
      if (!request.isCurrent()) return;
      const item = transferBatchResponse(response);
      setBatch({ state: "ready", item, error: null });
      if (successNotice) onNotice(successNotice);
    } catch (error) {
      if (!request.isCurrent()) return;
      setBatch((current) => ({ ...current, state: "error", error: errorMessage(error, "Transfer action failed.") }));
    } finally {
      request.complete();
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

function startAttempt(ref: RefObject<StartAttempt | null>, signature: string, createArchiveName: () => string = () => ""): StartAttempt {
  if (ref.current?.signature === signature) return ref.current;
  ref.current = {
    signature,
    key: globalThis.crypto.randomUUID(),
    archiveName: createArchiveName(),
  };
  return ref.current;
}

function isOverwriteConflict(
  error: unknown,
): error is { status: 409; data: { code: "remote_files_exist"; conflicts: { remote_path: string }[] } } {
  if (!error || typeof error !== "object" || !("status" in error) || error.status !== 409 || !("data" in error)) return false;
  const data = error.data;
  if (!data || typeof data !== "object" || !("code" in data) || data.code !== "remote_files_exist" || !("conflicts" in data)) return false;
  return (
    Array.isArray(data.conflicts) &&
    data.conflicts.every(
      (item: unknown) => !!item && typeof item === "object" && "remote_path" in item && typeof item.remote_path === "string",
    )
  );
}
