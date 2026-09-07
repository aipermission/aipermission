import { Download, FolderOpen, Pause, Play, RefreshCcw, Upload } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { apiDownload, apiGet, apiPost, apiPostForm } from "../../lib/api";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";
import { RemoteBrowserDialog } from "./file-transfer-browser-dialog";
import { ClearDownloadDialog, OverwriteConfirmDialog, UnsavedDownloadCloseDialog } from "./file-transfer-confirm-dialogs";
import { QueueList, QueueSummary } from "./file-transfer-queue";
import { useTransferQueues } from "./use-transfer-queues";
import {
  defaultRemoteDirectory,
  fileTransferFailureText,
  fileTransferPathPolicy,
  forgetDownloadPath,
  pendingBatchItemIDs,
  rememberDownloadPath,
  rememberedDownloadPath,
  suggestedArchiveName,
  transferProgress,
} from "../../lib/file-transfer-utils";

const emptyBatchState = { state: "idle", item: null, error: null };
const emptyBrowserState = { open: false, purpose: "upload", path: "/", state: "idle", data: null, error: null };
export function FileTransferDialog({ open, runtimeTarget, options = {}, onClose }) {
  const defaultRemoteDir = options.defaultDirectory || defaultRemoteDirectory();
  const { joinRemotePath, normalizeRemoteDirectoryInput } = fileTransferPathPolicy(options);
  const [batch, setBatch] = useState(emptyBatchState);
  const [browser, setBrowser] = useState(emptyBrowserState);
  const [downloadPrompted, setDownloadPrompted] = useState(false);
  const [downloadSaved, setDownloadSaved] = useState(false);
  const [overwritePrompt, setOverwritePrompt] = useState(null);
  const [clearDownloadPrompt, setClearDownloadPrompt] = useState(false);
  const [closeDownloadPrompt, setCloseDownloadPrompt] = useState(false);
  const [notice, setNotice] = useState(null);
  const completedUploadRef = useRef(0);
  const batchRefreshRequestRef = useRef(0);
  const browserRequestRef = useRef(0);
  const {
    mode,
    setMode,
    remoteDir,
    setRemoteDir,
    uploadQueue,
    downloadQueue,
    queue,
    fileInputRef,
    folderInputRef,
    resetQueues,
    clearQueue,
    handleLocalFileChange,
    addRemoteFiles,
    removeQueueItem: removePendingQueueItem,
    moveQueueItem: movePendingQueueItem,
    updateRemoteDirectory,
  } = useTransferQueues({
    runtimeTarget,
    defaultRemoteDir,
    recursive: Boolean(options.recursive),
    joinRemotePath,
    onNotice: setNotice,
  });

  const activeBatch = batch.item && ["pending", "running", "paused"].includes(batch.item.status);
  const unsavedCompletedDownload = batch.item?.direction === "download" && batch.item.status === "completed" && !downloadSaved;
  const progress = useMemo(() => transferProgress(batch.item), [batch.item]);
  const canStart = runtimeTarget && queue.length > 0 && !batch.item && batch.state !== "starting";
  const closeDisabled = Boolean(activeBatch) || ["starting", "pausing", "resuming", "canceling", "downloading"].includes(batch.state);
  const batchItemID = batch.item?.id;
  const batchItemStatus = batch.item?.status;
  const batchState = batch.state;
  const resetDialogForEffect = useEffectEvent((nextRemoteDir) => resetDialog(nextRemoteDir));
  const refreshBatchForEffect = useEffectEvent((id, options) => refreshBatch(id, options));
  const updateCompletedBatchNotice = useEffectEvent(() => {
    if (!batch.item || batch.item.status !== "completed") return;
    if (batch.item.direction === "upload") {
      setNotice({ tone: "good", message: "Upload queue completed. Review the summary, then clear when ready." });
      if (completedUploadRef.current !== batch.item.id) {
        completedUploadRef.current = batch.item.id;
        void options.onUploadCompleted?.();
      }
      return;
    }
    if (batch.item.direction === "download" && !downloadPrompted && !downloadSaved) {
      setNotice({ tone: "good", message: "Download queue completed. Click Save download to choose where to save it." });
    }
  });

  useEffect(() => {
    if (!open) {
      resetDialogForEffect(defaultRemoteDir);
      return;
    }
    setRemoteDir((current) => current || defaultRemoteDir);
  }, [open, defaultRemoteDir, setRemoteDir]);

  useEffect(() => {
    if (!open || batchState !== "ready" || !batchItemID || !["pending", "running", "paused"].includes(batchItemStatus)) return undefined;
    const timer = window.setInterval(() => {
      void refreshBatchForEffect(batchItemID, { silent: true });
    }, 900);
    return () => window.clearInterval(timer);
  }, [open, batchItemID, batchItemStatus, batchState]);

  useEffect(() => {
    updateCompletedBatchNotice();
  }, [batch.item?.id, batch.item?.status, batch.item?.direction, downloadPrompted, downloadSaved]);

  function resetDialog(nextRemoteDir = defaultRemoteDir) {
    batchRefreshRequestRef.current += 1;
    browserRequestRef.current += 1;
    resetQueues(nextRemoteDir);
    setBatch(emptyBatchState);
    setBrowser(emptyBrowserState);
    setDownloadPrompted(false);
    setDownloadSaved(false);
    setOverwritePrompt(null);
    setClearDownloadPrompt(false);
    setCloseDownloadPrompt(false);
    setNotice(null);
    completedUploadRef.current = 0;
  }

  function clearBatchPanel() {
    setBatch(emptyBatchState);
    setDownloadPrompted(false);
    setDownloadSaved(false);
    setOverwritePrompt(null);
    setClearDownloadPrompt(false);
    setCloseDownloadPrompt(false);
  }

  function requestClose() {
    if (unsavedCompletedDownload) {
      setCloseDownloadPrompt(true);
      return;
    }
    onClose();
  }

  function clearFinishedQueue(options = {}) {
    if (batch.item?.direction === "download" && batch.item.status === "completed" && !downloadSaved && !options.force) {
      setClearDownloadPrompt(true);
      return;
    }
    const direction = batch.item?.direction || mode;
    clearQueue(direction);
    setNotice(null);
    clearBatchPanel();
  }

  async function refreshBatch(id = batch.item?.id, options = {}) {
    if (!id) return;
    const requestID = ++batchRefreshRequestRef.current;
    if (!options.silent) {
      setBatch((current) => ({ ...current, state: "loading", error: null }));
    }
    try {
      const item = await apiGet(`/api/file-transfer-batches/${id}`);
      if (requestID !== batchRefreshRequestRef.current) return;
      setBatch((current) => {
        if (options.silent && !["idle", "loading", "ready"].includes(current.state)) return current;
        return { state: "ready", item, error: null };
      });
    } catch (error) {
      if (requestID !== batchRefreshRequestRef.current) return;
      setBatch((current) => {
        if (options.silent && !["idle", "loading", "ready"].includes(current.state)) return current;
        return { ...current, state: "error", error: error.message };
      });
    }
  }

  function removeQueueItem(id) {
    if (batch.item?.status === "paused") {
      const nextIDs = pendingBatchItemIDs(batch.item).filter((itemID) => itemID !== Number(id));
      void updatePausedBatchQueue(nextIDs);
      return;
    }
    removePendingQueueItem(id);
  }

  function moveQueueItem(id, direction) {
    if (batch.item?.status === "paused") {
      const ids = pendingBatchItemIDs(batch.item);
      const index = ids.indexOf(Number(id));
      const nextIndex = index + direction;
      if (index < 0 || nextIndex < 0 || nextIndex >= ids.length) return;
      const next = [...ids];
      [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
      void updatePausedBatchQueue(next);
      return;
    }
    movePendingQueueItem(id, direction);
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

  async function startQueue(options = {}) {
    if (!runtimeTarget || queue.length === 0) return;
    if (mode === "upload") {
      await startUploadBatch(options);
    } else {
      await startDownloadBatch();
    }
  }

  async function startUploadBatch(options = {}) {
    const formData = new FormData();
    formData.append("runtime_id", String(runtimeTarget.id));
    formData.append("remote_dir", remoteDir);
    formData.append("overwrite", options.overwrite ? "true" : "false");
    uploadQueue.forEach((item) => formData.append("files", item.file, item.name));
    formData.append("relative_paths", JSON.stringify(uploadQueue.map((item) => item.relative_path || item.name)));
    setNotice(null);
    setOverwritePrompt(null);
    setDownloadPrompted(false);
    setDownloadSaved(false);
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
    setNotice(null);
    setDownloadPrompted(false);
    setDownloadSaved(false);
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

  async function pauseBatch() {
    if (!batch.item) return;
    batchRefreshRequestRef.current += 1;
    setBatch((current) => ({ ...current, state: "pausing", error: null }));
    try {
      const item = await apiPost(`/api/file-transfer-batches/${batch.item.id}/pause`, {});
      setBatch({ state: "ready", item, error: null });
    } catch (error) {
      setBatch((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  async function resumeBatch() {
    if (!batch.item) return;
    batchRefreshRequestRef.current += 1;
    setBatch((current) => ({ ...current, state: "resuming", error: null }));
    try {
      const item = await apiPost(`/api/file-transfer-batches/${batch.item.id}/resume`, {});
      setBatch({ state: "ready", item, error: null });
    } catch (error) {
      setBatch((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  async function cancelBatch() {
    if (!batch.item) return;
    batchRefreshRequestRef.current += 1;
    setBatch((current) => ({ ...current, state: "canceling", error: null }));
    try {
      const item = await apiPost(`/api/file-transfer-batches/${batch.item.id}/cancel`, {});
      setBatch({ state: "ready", item, error: null });
      setNotice({ tone: "warn", message: "Transfer queue canceled." });
    } catch (error) {
      setBatch((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  async function saveDownloadBatch(options = {}) {
    if (!batch.item) return false;
    setBatch((current) => ({ ...current, state: "downloading", error: null }));
    try {
      const filename = batch.item.archive_name || batch.item.items?.[0]?.file_name || "aipermission-download";
      const result = await apiDownload(`/api/file-transfer-batches/${batch.item.id}/download`, filename, { picker: true });
      setDownloadPrompted(true);
      if (result?.canceled) {
        setNotice({ tone: "warn", message: "Download was not saved. You can try Save download again." });
        setBatch((current) => ({ ...current, state: "ready", error: null }));
        return false;
      }
      setDownloadSaved(true);
      if (options.clearAfterSave) {
        clearQueue("download");
        setNotice(null);
        clearBatchPanel();
        return true;
      }
      if (options.closeAfterSave) {
        setCloseDownloadPrompt(false);
        onClose();
        return true;
      }
      setNotice({ tone: "good", message: "Download saved. Review the summary, then clear when ready." });
      setBatch((current) => ({ ...current, state: "ready", error: null }));
      return true;
    } catch (error) {
      setDownloadPrompted(true);
      setBatch((current) => ({ ...current, state: "error", error: error.message }));
      return false;
    }
  }

  function openBrowser(purpose) {
    const nextPath =
      purpose === "download"
        ? rememberedDownloadPath(runtimeTarget, defaultRemoteDir, normalizeRemoteDirectoryInput)
        : normalizeRemoteDirectoryInput(remoteDir || defaultRemoteDir);
    setBrowser({ open: true, purpose, path: nextPath, state: "loading", data: null, error: null });
    void loadBrowser(nextPath, purpose, { fallbackToDefault: purpose === "download" });
  }

  async function loadBrowser(pathValue = browser.path, purpose = browser.purpose, options = {}) {
    if (!runtimeTarget) return;
    const requestID = ++browserRequestRef.current;
    const nextPath = normalizeRemoteDirectoryInput(pathValue || "/");
    setBrowser((current) => ({ ...current, purpose, path: nextPath, state: options.append ? "loading-more" : "loading", error: null }));
    try {
      const data = await apiPost("/api/file-transfers/browse", {
        runtime_id: Number(runtimeTarget.id),
        path: nextPath,
        ...(options.cursor ? { cursor: options.cursor } : {}),
      });
      if (requestID !== browserRequestRef.current) return;
      if (purpose === "download") {
        rememberDownloadPath(runtimeTarget, data.path || nextPath, normalizeRemoteDirectoryInput);
      }
      setBrowser((current) => ({
        open: true,
        purpose,
        path: data.path || nextPath,
        state: "ready",
        data: options.append ? { ...data, entries: [...(current.data?.entries || []), ...(data.entries || [])] } : data,
        error: null,
      }));
    } catch (error) {
      if (requestID !== browserRequestRef.current) return;
      if (purpose === "download" && options.fallbackToDefault && nextPath !== defaultRemoteDir) {
        forgetDownloadPath(runtimeTarget);
        void loadBrowser(defaultRemoteDir, purpose, { fallbackToDefault: false });
        return;
      }
      setBrowser((current) => ({ ...current, purpose, path: nextPath, state: "error", error: error.message }));
    }
  }

  function useBrowserDirectory(pathValue = browser.path) {
    updateRemoteDirectory(pathValue);
    setBrowser(emptyBrowserState);
  }

  function switchMode(nextMode) {
    setMode(nextMode);
    setOverwritePrompt(null);
    setNotice(null);
  }

  return (
    <>
      <Dialog
        open={open}
        title={runtimeTarget ? `${runtimeTarget.name} file transfers` : "File transfers"}
        description={`Queue uploads and downloads over ${options.transportLabel || "the selected connector"}.`}
        onClose={requestClose}
        size="wide"
        className="xl:max-w-[70vw]"
        bodyClassName="grid max-h-[calc(100vh-130px)] min-h-0 overflow-hidden"
        autoFocusClose={false}
        closeOnOverlay={false}
        closeOnEscape={false}
        closeDisabled={closeDisabled}
      >
        <div className="grid min-h-0 gap-4 lg:grid-cols-[minmax(280px,0.9fr)_minmax(0,1.4fr)]">
          <section className="grid min-h-0 content-start gap-4">
            <Notice tone="warn">
              {options.notice ||
                "AIPermission stores transfer history metadata only; file contents use short-lived local staging files under the data directory."}
            </Notice>
            {notice ? <Notice tone={notice.tone}>{notice.message}</Notice> : null}
            <div className="grid grid-cols-2 gap-2 rounded-md border border-stone-200 bg-stone-50 p-1">
              <Button
                type="button"
                variant={mode === "upload" ? "default" : "ghost"}
                className="h-9"
                onClick={() => switchMode("upload")}
                disabled={Boolean(batch.item)}
              >
                <Upload className="h-4 w-4" />
                Upload
              </Button>
              <Button
                type="button"
                variant={mode === "download" ? "default" : "ghost"}
                className="h-9"
                onClick={() => switchMode("download")}
                disabled={Boolean(batch.item)}
              >
                <Download className="h-4 w-4" />
                Download
              </Button>
            </div>

            <div className="rounded-md border border-stone-200 bg-white p-4">
              <p className="text-xs font-semibold uppercase tracking-wide text-stone-500">Target</p>
              <p className="mt-2 truncate text-sm font-semibold text-stone-900">{runtimeTarget?.name || "No target selected"}</p>
              <p className="truncate font-mono text-xs text-stone-500">
                {runtimeTarget?.subtitle || "Open Console and select a target first."}
              </p>
            </div>

            {mode === "upload" ? (
              <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
                <Field>
                  Remote folder
                  <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
                    <Input
                      value={remoteDir}
                      onChange={(event) => updateRemoteDirectory(event.target.value)}
                      placeholder={defaultRemoteDir}
                      disabled={Boolean(activeBatch)}
                    />
                    <Button
                      type="button"
                      variant="outline"
                      className="h-10"
                      onClick={() => openBrowser("upload")}
                      disabled={!runtimeTarget || Boolean(activeBatch)}
                    >
                      <FolderOpen className="h-4 w-4" />
                      Browse
                    </Button>
                  </div>
                </Field>
                <Button
                  type="button"
                  variant="outline"
                  className="w-full"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={Boolean(activeBatch)}
                >
                  <Upload className="h-4 w-4" />
                  Add files
                </Button>
                <input ref={fileInputRef} className="hidden" type="file" multiple onChange={handleLocalFileChange} />
                {options.recursive ? (
                  <>
                    <Button
                      type="button"
                      variant="outline"
                      className="w-full"
                      onClick={() => folderInputRef.current?.click()}
                      disabled={Boolean(activeBatch)}
                    >
                      <FolderOpen className="h-4 w-4" />
                      Add folder
                    </Button>
                    <input
                      ref={folderInputRef}
                      className="hidden"
                      type="file"
                      multiple
                      webkitdirectory=""
                      onChange={handleLocalFileChange}
                    />
                  </>
                ) : null}
              </div>
            ) : (
              <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
                <Button
                  type="button"
                  variant="outline"
                  className="w-full"
                  onClick={() => openBrowser("download")}
                  disabled={!runtimeTarget || Boolean(activeBatch)}
                >
                  <FolderOpen className="h-4 w-4" />
                  Add remote files
                </Button>
                <p className="text-xs text-stone-500">
                  The browser opens at the last folder used for this target, or <code>{defaultRemoteDir}</code> when no folder is
                  remembered. Multiple downloads are saved as one temporary zip archive.
                </p>
              </div>
            )}

            <QueueSummary batch={batch.item} queue={queue} mode={mode} progress={progress} />
          </section>

          <section className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p className="text-sm font-semibold text-stone-900">Queue</p>
                <p className="text-xs text-stone-500">
                  {activeBatch ? "Transfer is running from this queue." : `${queue.length} item${queue.length === 1 ? "" : "s"} ready.`}
                </p>
              </div>
              <Button
                type="button"
                variant="outline"
                className="h-9"
                onClick={() => refreshBatch()}
                disabled={!batch.item || batch.state === "loading"}
              >
                <RefreshCcw className="h-4 w-4" />
                Refresh
              </Button>
            </div>

            <QueueList
              mode={mode}
              queue={queue}
              batch={batch.item}
              active={Boolean(activeBatch)}
              canEditPausedBatch={batch.item?.status === "paused" && batch.state !== "updating"}
              onRemove={removeQueueItem}
              onMove={moveQueueItem}
            />

            <div className="grid gap-3 border-t border-stone-200 pt-3">
              <TransferFailureNotice item={batch.item} fallback={batch.error} />

              <div className="flex flex-wrap items-center justify-end gap-2">
                {batch.item?.status === "running" ? (
                  <Button type="button" variant="outline" className="h-10" onClick={pauseBatch} disabled={batch.state === "pausing"}>
                    <Pause className="h-4 w-4" />
                    Pause
                  </Button>
                ) : null}
                {batch.item?.status === "paused" ? (
                  <Button type="button" variant="outline" className="h-10" onClick={resumeBatch} disabled={batch.state === "resuming"}>
                    <Play className="h-4 w-4" />
                    Resume
                  </Button>
                ) : null}
                {activeBatch ? (
                  <Button type="button" variant="danger" className="h-10" onClick={cancelBatch} disabled={batch.state === "canceling"}>
                    Cancel
                  </Button>
                ) : null}
                {batch.item?.direction === "download" && batch.item.status === "completed" ? (
                  <Button type="button" className="h-10" onClick={saveDownloadBatch} disabled={batch.state === "downloading"}>
                    <Download className="h-4 w-4" />
                    {batch.state === "downloading" ? "Saving..." : "Save download"}
                  </Button>
                ) : null}
                {batch.item && !activeBatch ? (
                  <Button type="button" variant="outline" className="h-10" onClick={() => clearFinishedQueue()}>
                    Clear
                  </Button>
                ) : null}
                <Button type="button" disabled={!canStart} onClick={() => startQueue()}>
                  {mode === "upload" ? <Upload className="h-4 w-4" /> : <Download className="h-4 w-4" />}
                  Start {mode}
                </Button>
              </div>
            </div>
          </section>
        </div>
      </Dialog>

      <RemoteBrowserDialog
        browser={browser}
        transportLabel={options.transportLabel || "the connector"}
        recursive={Boolean(options.recursive)}
        onClose={() => setBrowser(emptyBrowserState)}
        onLoad={loadBrowser}
        onPathChange={(path) => setBrowser((current) => ({ ...current, path }))}
        onUseDirectory={useBrowserDirectory}
        onAddFiles={addRemoteFiles}
        queuedPaths={new Set(downloadQueue.map((item) => item.path))}
      />

      <ClearDownloadDialog
        open={clearDownloadPrompt}
        onCancel={() => setClearDownloadPrompt(false)}
        onContinue={() => clearFinishedQueue({ force: true })}
        onSave={() => {
          setClearDownloadPrompt(false);
          void saveDownloadBatch({ clearAfterSave: true });
        }}
      />

      <UnsavedDownloadCloseDialog
        open={closeDownloadPrompt}
        onCancel={() => setCloseDownloadPrompt(false)}
        onCloseAnyway={() => {
          setCloseDownloadPrompt(false);
          onClose();
        }}
        onSave={() => {
          setCloseDownloadPrompt(false);
          void saveDownloadBatch({ closeAfterSave: true });
        }}
      />

      <OverwriteConfirmDialog
        open={Boolean(overwritePrompt?.length)}
        conflicts={overwritePrompt || []}
        onCancel={() => setOverwritePrompt(null)}
        onOverwrite={() => startQueue({ overwrite: true })}
      />
    </>
  );
}

function TransferFailureNotice({ item, fallback }) {
  const message = fileTransferFailureText(item, fallback);
  return message ? <Notice tone="bad">{message}</Notice> : null;
}
