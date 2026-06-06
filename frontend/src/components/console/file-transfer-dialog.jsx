import {
  Download,
  FolderOpen,
  Pause,
  Play,
  RefreshCcw,
  Upload,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { apiDownload, apiGet, apiPost, apiPostForm } from "../../lib/api";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";
import { RemoteBrowserDialog } from "./file-transfer-browser-dialog";
import { ClearDownloadDialog, OverwriteConfirmDialog, UnsavedDownloadCloseDialog } from "./file-transfer-confirm-dialogs";
import { QueueList, QueueSummary } from "./file-transfer-queue";
import {
  defaultRemoteDirectory,
  forgetDownloadPath,
  joinRemotePath,
  localFileID,
  normalizeRemoteDirectoryInput,
  pendingBatchItemIDs,
  rememberDownloadPath,
  rememberedDownloadPath,
  suggestedArchiveName,
  transferProgress,
} from "./file-transfer-utils";

const emptyBatchState = { state: "idle", item: null, error: null };
const emptyBrowserState = { open: false, purpose: "upload", path: "/", state: "idle", data: null, error: null };

export function FileTransferDialog({ open, server, onClose }) {
  const defaultRemoteDir = defaultRemoteDirectory();
  const [mode, setMode] = useState("upload");
  const [remoteDir, setRemoteDir] = useState(defaultRemoteDir);
  const [uploadQueue, setUploadQueue] = useState([]);
  const [downloadQueue, setDownloadQueue] = useState([]);
  const [batch, setBatch] = useState(emptyBatchState);
  const [browser, setBrowser] = useState(emptyBrowserState);
  const [downloadPrompted, setDownloadPrompted] = useState(false);
  const [downloadSaved, setDownloadSaved] = useState(false);
  const [overwritePrompt, setOverwritePrompt] = useState(null);
  const [clearDownloadPrompt, setClearDownloadPrompt] = useState(false);
  const [closeDownloadPrompt, setCloseDownloadPrompt] = useState(false);
  const [notice, setNotice] = useState("");
  const fileInputRef = useRef(null);

  const queue = mode === "upload" ? uploadQueue : downloadQueue;
  const activeBatch = batch.item && ["pending", "running", "paused"].includes(batch.item.status);
  const unsavedCompletedDownload = batch.item?.direction === "download" && batch.item.status === "completed" && !downloadSaved;
  const progress = useMemo(() => transferProgress(batch.item), [batch.item]);
  const canStart = server && queue.length > 0 && !batch.item && batch.state !== "starting";

  useEffect(() => {
    if (!open) {
      resetDialog(defaultRemoteDir);
      return;
    }
    setRemoteDir((current) => current || defaultRemoteDir);
  }, [open, defaultRemoteDir]);

  useEffect(() => {
    if (!open || !batch.item || !["pending", "running", "paused"].includes(batch.item.status)) return undefined;
    const timer = window.setInterval(() => {
      void refreshBatch(batch.item.id, { silent: true });
    }, 900);
    return () => window.clearInterval(timer);
  }, [open, batch.item?.id, batch.item?.status]);

  useEffect(() => {
    if (!batch.item || batch.item.status !== "completed") return;
    if (batch.item.direction === "upload") {
      setNotice("Upload queue completed. Review the summary, then clear when ready.");
      return;
    }
    if (batch.item.direction === "download" && !downloadPrompted && !downloadSaved) {
      setNotice("Download queue completed. Click Save download to choose where to save it.");
    }
  }, [batch.item?.id, batch.item?.status, batch.item?.direction, downloadPrompted, downloadSaved]);

  function resetDialog(nextRemoteDir = defaultRemoteDir) {
    setMode("upload");
    setRemoteDir(nextRemoteDir);
    setUploadQueue([]);
    setDownloadQueue([]);
    setBatch(emptyBatchState);
    setBrowser(emptyBrowserState);
    setDownloadPrompted(false);
    setDownloadSaved(false);
    setOverwritePrompt(null);
    setClearDownloadPrompt(false);
    setCloseDownloadPrompt(false);
    setNotice("");
    if (fileInputRef.current) fileInputRef.current.value = "";
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
    if (direction === "upload") {
      setUploadQueue([]);
      setRemoteDir(defaultRemoteDir);
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
    if (direction === "download") {
      setDownloadQueue([]);
    }
    setNotice("");
    clearBatchPanel();
  }

  async function refreshBatch(id = batch.item?.id, options = {}) {
    if (!id) return;
    if (!options.silent) {
      setBatch((current) => ({ ...current, state: "loading", error: null }));
    }
    try {
      const item = await apiGet(`/api/file-transfer-batches/${id}`);
      setBatch({ state: "ready", item, error: null });
    } catch (error) {
      setBatch((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  function handleLocalFileChange(event) {
    const files = Array.from(event.target.files || []);
    if (files.length === 0) return;
    setUploadQueue((current) => [
      ...current,
      ...files.map((file) => ({
        id: localFileID(file),
        file,
        name: file.name,
        size: file.size,
        remote_path: joinRemotePath(remoteDir, file.name),
      })),
    ]);
    if (fileInputRef.current) fileInputRef.current.value = "";
  }

  function addRemoteFiles(entries) {
    const files = Array.isArray(entries) ? entries.filter((entry) => entry?.type === "file") : [];
    if (files.length === 0) return;
    setDownloadQueue((current) => {
      const existing = new Set(current.map((item) => item.path));
      const nextFiles = files.filter((entry) => !existing.has(entry.path));
      if (nextFiles.length === 0) return current;
      return [
        ...current,
        ...nextFiles.map((entry) => ({
          id: `remote-${entry.path}`,
          path: entry.path,
          name: entry.name,
          size: entry.size,
        })),
      ];
    });
  }

  function removeQueueItem(id) {
    if (batch.item?.status === "paused") {
      const nextIDs = pendingBatchItemIDs(batch.item).filter((itemID) => itemID !== Number(id));
      void updatePausedBatchQueue(nextIDs);
      return;
    }
    if (mode === "upload") {
      setUploadQueue((current) => current.filter((item) => item.id !== id));
    } else {
      setDownloadQueue((current) => current.filter((item) => item.id !== id));
    }
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
    const setter = mode === "upload" ? setUploadQueue : setDownloadQueue;
    setter((current) => {
      const index = current.findIndex((item) => item.id === id);
      const nextIndex = index + direction;
      if (index < 0 || nextIndex < 0 || nextIndex >= current.length) return current;
      const next = [...current];
      [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
      return next;
    });
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
    if (!server || queue.length === 0) return;
    if (mode === "upload") {
      await startUploadBatch(options);
    } else {
      await startDownloadBatch();
    }
  }

  async function startUploadBatch(options = {}) {
    const formData = new FormData();
    formData.append("server_id", String(server.id));
    formData.append("remote_dir", remoteDir);
    formData.append("overwrite", options.overwrite ? "true" : "false");
    uploadQueue.forEach((item) => formData.append("files", item.file, item.name));
    setNotice("");
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
    setNotice("");
    setDownloadPrompted(false);
    setDownloadSaved(false);
    setBatch({ state: "starting", item: null, error: null });
    try {
      const item = await apiPost("/api/file-transfers/download-batch", {
        server_id: Number(server.id),
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
    setBatch((current) => ({ ...current, state: "canceling", error: null }));
    try {
      const item = await apiPost(`/api/file-transfer-batches/${batch.item.id}/cancel`, {});
      setBatch({ state: "ready", item, error: null });
      setNotice("Transfer queue canceled.");
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
        setNotice("Download was not saved. You can try Save download again.");
        setBatch((current) => ({ ...current, state: "ready", error: null }));
        return false;
      }
      setDownloadSaved(true);
      if (options.clearAfterSave) {
        setDownloadQueue([]);
        setNotice("");
        clearBatchPanel();
        return true;
      }
      if (options.closeAfterSave) {
        setCloseDownloadPrompt(false);
        onClose();
        return true;
      }
      setNotice("Download saved. Review the summary, then clear when ready.");
      setBatch((current) => ({ ...current, state: "ready", error: null }));
      return true;
    } catch (error) {
      setDownloadPrompted(true);
      setBatch((current) => ({ ...current, state: "error", error: error.message }));
      return false;
    }
  }

  function openBrowser(purpose) {
    const nextPath = purpose === "download"
      ? rememberedDownloadPath(server, defaultRemoteDir)
      : normalizeRemoteDirectoryInput(remoteDir || defaultRemoteDir);
    setBrowser({ open: true, purpose, path: nextPath, state: "loading", data: null, error: null });
    void loadBrowser(nextPath, purpose, { fallbackToDefault: purpose === "download" });
  }

  async function loadBrowser(pathValue = browser.path, purpose = browser.purpose, options = {}) {
    if (!server) return;
    const nextPath = normalizeRemoteDirectoryInput(pathValue || "/");
    setBrowser((current) => ({ ...current, purpose, path: nextPath, state: "loading", error: null }));
    try {
      const data = await apiPost("/api/file-transfers/browse", {
        server_id: Number(server.id),
        path: nextPath,
      });
      if (purpose === "download") {
        rememberDownloadPath(server, data.path || nextPath);
      }
      setBrowser({ open: true, purpose, path: data.path || nextPath, state: "ready", data, error: null });
    } catch (error) {
      if (purpose === "download" && options.fallbackToDefault && nextPath !== defaultRemoteDir) {
        forgetDownloadPath(server);
        void loadBrowser(defaultRemoteDir, purpose, { fallbackToDefault: false });
        return;
      }
      setBrowser((current) => ({ ...current, purpose, path: nextPath, state: "error", error: error.message }));
    }
  }

  function useBrowserDirectory(pathValue = browser.path) {
    setRemoteDir(pathValue);
    setUploadQueue((current) => current.map((item) => ({ ...item, remote_path: joinRemotePath(pathValue, item.name) })));
    setBrowser(emptyBrowserState);
  }

  function switchMode(nextMode) {
    setMode(nextMode);
    setOverwritePrompt(null);
    setNotice("");
  }

  return (
    <>
      <Dialog
        open={open}
        title={server ? `${server.name} file transfers` : "File transfers"}
        description="Queue uploads and downloads over the selected server's SSH connection."
        onClose={requestClose}
        size="wide"
        className="xl:max-w-[70vw]"
        bodyClassName="grid max-h-[calc(100vh-130px)] min-h-0 overflow-hidden"
        autoFocusClose={false}
        closeOnOverlay={false}
        closeOnEscape={false}
        closeDisabled={Boolean(activeBatch)}
      >
        <div className="grid min-h-0 gap-4 lg:grid-cols-[minmax(280px,0.9fr)_minmax(0,1.4fr)]">
          <section className="grid min-h-0 content-start gap-4">
            <Notice tone="warn">
              File transfers use SFTP over the server's existing gateway key. AIPermission stores transfer history metadata only; file contents use short-lived local staging files under the data directory.
            </Notice>
            {notice ? <Notice tone={notice.includes("canceled") ? "warn" : "good"}>{notice}</Notice> : null}
            <div className="grid grid-cols-2 gap-2 rounded-md border border-stone-200 bg-stone-50 p-1">
              <Button type="button" variant={mode === "upload" ? "default" : "ghost"} className="h-9" onClick={() => switchMode("upload")} disabled={Boolean(batch.item)}>
                <Upload className="h-4 w-4" />
                Upload
              </Button>
              <Button type="button" variant={mode === "download" ? "default" : "ghost"} className="h-9" onClick={() => switchMode("download")} disabled={Boolean(batch.item)}>
                <Download className="h-4 w-4" />
                Download
              </Button>
            </div>

            <div className="rounded-md border border-stone-200 bg-white p-4">
              <p className="text-xs font-semibold uppercase tracking-wide text-stone-500">Target</p>
              <p className="mt-2 truncate text-sm font-semibold text-stone-900">{server?.name || "No server selected"}</p>
              <p className="truncate font-mono text-xs text-stone-500">
                {server ? `${server.username}@${server.host}:${server.port}` : "Open Console and select a server first."}
              </p>
            </div>

            {mode === "upload" ? (
              <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
                <Field>
                  Remote folder
                  <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
                    <Input
                      value={remoteDir}
                      onChange={(event) => {
                        const value = event.target.value;
                        setRemoteDir(value);
                        setUploadQueue((current) => current.map((item) => ({ ...item, remote_path: joinRemotePath(value, item.name) })));
                      }}
                      placeholder={defaultRemoteDir}
                      disabled={Boolean(activeBatch)}
                    />
                    <Button type="button" variant="outline" className="h-10" onClick={() => openBrowser("upload")} disabled={!server || Boolean(activeBatch)}>
                      <FolderOpen className="h-4 w-4" />
                      Browse
                    </Button>
                  </div>
                </Field>
                <Button type="button" variant="outline" className="w-full" onClick={() => fileInputRef.current?.click()} disabled={Boolean(activeBatch)}>
                  <Upload className="h-4 w-4" />
                  Add files
                </Button>
                <input ref={fileInputRef} className="hidden" type="file" multiple onChange={handleLocalFileChange} />
              </div>
            ) : (
              <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
                <Button type="button" variant="outline" className="w-full" onClick={() => openBrowser("download")} disabled={!server || Boolean(activeBatch)}>
                  <FolderOpen className="h-4 w-4" />
                  Add remote files
                </Button>
                <p className="text-xs text-stone-500">The browser opens at the last folder used for this server, or `/home` when no folder is remembered. Multiple downloads are saved as one temporary zip archive.</p>
              </div>
            )}

            <QueueSummary batch={batch.item} queue={queue} mode={mode} progress={progress} />
          </section>

          <section className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p className="text-sm font-semibold text-stone-900">Queue</p>
                <p className="text-xs text-stone-500">{activeBatch ? "Transfer is running from this queue." : `${queue.length} item${queue.length === 1 ? "" : "s"} ready.`}</p>
              </div>
              <Button type="button" variant="outline" className="h-9" onClick={() => refreshBatch()} disabled={!batch.item || batch.state === "loading"}>
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
              {batch.error ? <Notice tone="bad">{batch.error}</Notice> : null}

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
