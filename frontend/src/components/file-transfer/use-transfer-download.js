import { useEffect, useEffectEvent, useState } from "react";
import { apiDownload } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";

export function useTransferDownload({ batch, setBatch, mode, clearBatch, clearQueue, onNotice, onClose }) {
  const [downloadPrompted, setDownloadPrompted] = useState(false);
  const [downloadSaved, setDownloadSaved] = useState(false);
  const [clearDownloadPrompt, setClearDownloadPrompt] = useState(false);
  const [closeDownloadPrompt, setCloseDownloadPrompt] = useState(false);
  const guard = useRequestGuard(String(batch.item?.id || ""));
  const publishCompletedNotice = useEffectEvent(() => {
    if (batch.item?.direction !== "download" || batch.item.status !== "completed" || downloadPrompted || downloadSaved) return;
    onNotice({ tone: "good", message: "Download queue completed. Click Save download to choose where to save it." });
  });
  const unsavedCompletedDownload = batch.item?.direction === "download" && batch.item.status === "completed" && !downloadSaved;

  useEffect(() => {
    publishCompletedNotice();
  }, [batch.item?.id, batch.item?.status, batch.item?.direction, downloadPrompted, downloadSaved]);

  function resetDownloadState() {
    guard.invalidate("save");
    setDownloadPrompted(false);
    setDownloadSaved(false);
    setClearDownloadPrompt(false);
    setCloseDownloadPrompt(false);
  }

  function clearFinishedQueue(options = {}) {
    if (unsavedCompletedDownload && !options.force) {
      setClearDownloadPrompt(true);
      return;
    }
    clearQueue(batch.item?.direction || mode);
    onNotice(null);
    clearBatch();
    resetDownloadState();
  }

  function requestClose() {
    if (unsavedCompletedDownload) {
      setCloseDownloadPrompt(true);
      return;
    }
    onClose();
  }

  async function saveDownloadBatch(options = {}) {
    if (!batch.item) return false;
    const request = guard.begin("save");
    setBatch((current) => ({ ...current, state: "downloading", error: null }));
    try {
      const filename = batch.item.archive_name || batch.item.items?.[0]?.file_name || "aipermission-download";
      const result = await apiDownload(`/api/file-transfer-batches/${batch.item.id}/download`, filename, {
        picker: true,
        signal: request.signal,
      });
      if (!request.isCurrent()) return false;
      setDownloadPrompted(true);
      if (result?.canceled) {
        onNotice({ tone: "warn", message: "Download was not saved. You can try Save download again." });
        setBatch((current) => ({ ...current, state: "ready", error: null }));
        return false;
      }
      setDownloadSaved(true);
      if (options.clearAfterSave) {
        clearFinishedQueue({ force: true });
        return true;
      }
      if (options.closeAfterSave) {
        setCloseDownloadPrompt(false);
        onClose();
        return true;
      }
      onNotice({ tone: "good", message: "Download saved. Review the summary, then clear when ready." });
      setBatch((current) => ({ ...current, state: "ready", error: null }));
      return true;
    } catch (error) {
      if (!request.isCurrent()) return false;
      setDownloadPrompted(true);
      setBatch((current) => ({ ...current, state: "error", error: error.message }));
      return false;
    } finally {
      request.complete();
    }
  }

  return {
    clearDownloadPrompt,
    closeDownloadPrompt,
    resetDownloadState,
    prepareStart: resetDownloadState,
    requestClose,
    clearFinishedQueue,
    saveDownloadBatch,
    dismissClearPrompt: () => setClearDownloadPrompt(false),
    saveThenClear: () => {
      setClearDownloadPrompt(false);
      return saveDownloadBatch({ clearAfterSave: true });
    },
    dismissClosePrompt: () => setCloseDownloadPrompt(false),
    closeWithoutSaving: () => {
      setCloseDownloadPrompt(false);
      onClose();
    },
    saveThenClose: () => {
      setCloseDownloadPrompt(false);
      return saveDownloadBatch({ closeAfterSave: true });
    },
  };
}
