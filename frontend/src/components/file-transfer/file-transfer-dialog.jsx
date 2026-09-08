import { useEffect, useEffectEvent, useState } from "react";
import { Dialog } from "../ui/dialog";
import { RemoteBrowserDialog } from "./file-transfer-browser-dialog";
import { ClearDownloadDialog, OverwriteConfirmDialog, UnsavedDownloadCloseDialog } from "./file-transfer-confirm-dialogs";
import { TransferQueuePanel, TransferSetupPanel } from "./file-transfer-panels";
import { useTransferBrowser } from "./use-transfer-browser";
import { useTransferBatch } from "./use-transfer-batch";
import { useTransferDownload } from "./use-transfer-download";
import { useTransferQueues } from "./use-transfer-queues";
import { defaultRemoteDirectory, fileTransferPathPolicy } from "../../lib/file-transfer-utils";

export function FileTransferDialog({ open, runtimeTarget, options = {}, onClose }) {
  const defaultRemoteDir = options.defaultDirectory || defaultRemoteDirectory();
  const { joinRemotePath, normalizeRemoteDirectoryInput } = fileTransferPathPolicy(options);
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
  const download = useTransferDownload({
    batch,
    setBatch,
    mode,
    clearBatch: () => {
      clearBatch();
      setOverwritePrompt(null);
    },
    clearQueue,
    onNotice: setNotice,
    onClose,
  });
  const closeDisabled = Boolean(activeBatch) || ["starting", "pausing", "resuming", "canceling", "downloading"].includes(batch.state);
  const resetDialogForEffect = useEffectEvent((nextRemoteDir) => resetDialog(nextRemoteDir));

  useEffect(() => {
    if (!open) {
      resetDialogForEffect(defaultRemoteDir);
      return;
    }
    setRemoteDir((current) => current || defaultRemoteDir);
  }, [open, defaultRemoteDir, setRemoteDir]);

  function resetDialog(nextRemoteDir = defaultRemoteDir) {
    resetQueues(nextRemoteDir);
    resetBatch();
    resetBrowser();
    download.resetDownloadState();
    setOverwritePrompt(null);
    setNotice(null);
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
    download.prepareStart();
    await startBatchQueue(options);
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
        onClose={download.requestClose}
        size="wide"
        className="xl:max-w-[70vw]"
        bodyClassName="grid max-h-[calc(100vh-130px)] min-h-0 overflow-hidden"
        autoFocusClose={false}
        closeOnOverlay={false}
        closeOnEscape={false}
        closeDisabled={closeDisabled}
      >
        <div className="grid min-h-0 gap-4 lg:grid-cols-[minmax(280px,0.9fr)_minmax(0,1.4fr)]">
          <TransferSetupPanel
            runtimeTarget={runtimeTarget}
            mode={mode}
            batch={batch}
            activeBatch={activeBatch}
            queue={queue}
            progress={progress}
            notice={notice}
            transferNotice={
              options.notice ||
              "AIPermission stores transfer history metadata only; file contents use short-lived local staging files under the data directory."
            }
            remoteDir={remoteDir}
            defaultRemoteDir={defaultRemoteDir}
            recursive={Boolean(options.recursive)}
            fileInputRef={fileInputRef}
            folderInputRef={folderInputRef}
            onModeChange={switchMode}
            onRemoteDirectoryChange={updateRemoteDirectory}
            onOpenBrowser={openBrowser}
            onLocalFileChange={handleLocalFileChange}
          />
          <TransferQueuePanel
            mode={mode}
            queue={queue}
            batch={batch}
            activeBatch={activeBatch}
            canStart={canStart}
            onRefresh={() => refreshBatch()}
            onRemove={removeQueueItem}
            onMove={moveQueueItem}
            onPause={pauseBatch}
            onResume={resumeBatch}
            onCancel={cancelBatch}
            onSaveDownload={download.saveDownloadBatch}
            onClear={() => download.clearFinishedQueue()}
            onStart={() => startQueue()}
          />
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
        open={download.clearDownloadPrompt}
        onCancel={download.dismissClearPrompt}
        onContinue={() => download.clearFinishedQueue({ force: true })}
        onSave={() => void download.saveThenClear()}
      />

      <UnsavedDownloadCloseDialog
        open={download.closeDownloadPrompt}
        onCancel={download.dismissClosePrompt}
        onCloseAnyway={download.closeWithoutSaving}
        onSave={() => void download.saveThenClose()}
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
