import { Download, FolderOpen, Pause, Play, RefreshCcw, Upload } from "lucide-react";
import { useEffect, useEffectEvent, useState } from "react";
import { apiDownload } from "../../lib/api";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";
import { RemoteBrowserDialog } from "./file-transfer-browser-dialog";
import { ClearDownloadDialog, OverwriteConfirmDialog, UnsavedDownloadCloseDialog } from "./file-transfer-confirm-dialogs";
import { QueueList, QueueSummary } from "./file-transfer-queue";
import { useTransferBrowser } from "./use-transfer-browser";
import { useTransferBatch } from "./use-transfer-batch";
import { useTransferQueues } from "./use-transfer-queues";
import { defaultRemoteDirectory, fileTransferFailureText, fileTransferPathPolicy } from "../../lib/file-transfer-utils";

export function FileTransferDialog({ open, runtimeTarget, options = {}, onClose }) {
  const defaultRemoteDir = options.defaultDirectory || defaultRemoteDirectory();
  const { joinRemotePath, normalizeRemoteDirectoryInput } = fileTransferPathPolicy(options);
  const [downloadPrompted, setDownloadPrompted] = useState(false);
  const [downloadSaved, setDownloadSaved] = useState(false);
  const [clearDownloadPrompt, setClearDownloadPrompt] = useState(false);
  const [closeDownloadPrompt, setCloseDownloadPrompt] = useState(false);
  const [notice, setNotice] = useState(null);
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
  const { browser, openBrowser, loadBrowser, closeBrowser, setBrowserPath, useBrowserDirectory, resetBrowser } = useTransferBrowser({
    runtimeTarget,
    defaultRemoteDir,
    remoteDir,
    normalizeRemoteDirectoryInput,
    onUseDirectory: updateRemoteDirectory,
  });
  const {
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
    startQueue: startBatchQueue,
    pauseBatch,
    resumeBatch,
    cancelBatch,
  } = useTransferBatch({
    open,
    runtimeTarget,
    mode,
    remoteDir,
    uploadQueue,
    downloadQueue,
    queue,
    onNotice: setNotice,
    onUploadCompleted: options.onUploadCompleted,
  });
  const unsavedCompletedDownload = batch.item?.direction === "download" && batch.item.status === "completed" && !downloadSaved;
  const closeDisabled = Boolean(activeBatch) || ["starting", "pausing", "resuming", "canceling", "downloading"].includes(batch.state);
  const resetDialogForEffect = useEffectEvent((nextRemoteDir) => resetDialog(nextRemoteDir));
  const updateCompletedBatchNotice = useEffectEvent(() => {
    if (!batch.item || batch.item.status !== "completed") return;
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
    updateCompletedBatchNotice();
  }, [batch.item?.id, batch.item?.status, batch.item?.direction, downloadPrompted, downloadSaved]);

  function resetDialog(nextRemoteDir = defaultRemoteDir) {
    resetQueues(nextRemoteDir);
    resetBatch();
    resetBrowser();
    setDownloadPrompted(false);
    setDownloadSaved(false);
    setOverwritePrompt(null);
    setClearDownloadPrompt(false);
    setCloseDownloadPrompt(false);
    setNotice(null);
  }

  function clearBatchPanel() {
    clearBatch();
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

  function removeQueueItem(id) {
    if (batch.item?.status === "paused") {
      const nextIDs = pausedQueueWithout(id);
      void updatePausedBatchQueue(nextIDs);
      return;
    }
    removePendingQueueItem(id);
  }

  function moveQueueItem(id, direction) {
    if (batch.item?.status === "paused") {
      const next = movePausedQueueItem(id, direction);
      if (next) void updatePausedBatchQueue(next);
      return;
    }
    movePendingQueueItem(id, direction);
  }

  async function startQueue(options = {}) {
    setDownloadPrompted(false);
    setDownloadSaved(false);
    await startBatchQueue(options);
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
        onClose={closeBrowser}
        onLoad={loadBrowser}
        onPathChange={setBrowserPath}
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
