import {
  ArrowDown,
  ArrowUp,
  Download,
  File,
  Folder,
  FolderOpen,
  Pause,
  Play,
  RefreshCcw,
  Trash2,
  Upload,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { apiDownload, apiGet, apiPost, apiPostForm } from "../../lib/api";
import { cn } from "../../lib/utils";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";

const emptyBatchState = { state: "idle", item: null, error: null };
const emptyBrowserState = { open: false, purpose: "upload", path: "/", state: "idle", data: null, error: null };

export function FileTransferDialog({ open, server, onClose }) {
  const defaultRemoteDir = useMemo(() => defaultRemoteDirectory(server), [server]);
  const [mode, setMode] = useState("upload");
  const [remoteDir, setRemoteDir] = useState(defaultRemoteDir);
  const [uploadQueue, setUploadQueue] = useState([]);
  const [downloadQueue, setDownloadQueue] = useState([]);
  const [batch, setBatch] = useState(emptyBatchState);
  const [browser, setBrowser] = useState(emptyBrowserState);
  const [downloadPrompted, setDownloadPrompted] = useState(false);
  const [overwritePrompt, setOverwritePrompt] = useState(null);
  const [notice, setNotice] = useState("");
  const fileInputRef = useRef(null);

  const queue = mode === "upload" ? uploadQueue : downloadQueue;
  const activeBatch = batch.item && ["pending", "running", "paused"].includes(batch.item.status);
  const progress = useMemo(() => transferProgress(batch.item), [batch.item]);
  const canStart = server && queue.length > 0 && !activeBatch && batch.state !== "starting";

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
      setNotice("Upload queue completed.");
      setUploadQueue([]);
      clearBatchPanel();
      return;
    }
    if (batch.item.direction === "download" && !downloadPrompted && batch.state !== "downloading") {
      void saveDownloadBatch();
    }
  }, [batch.item?.id, batch.item?.status, batch.item?.direction, downloadPrompted, batch.state]);

  function resetDialog(nextRemoteDir = defaultRemoteDir) {
    setMode("upload");
    setRemoteDir(nextRemoteDir);
    setUploadQueue([]);
    setDownloadQueue([]);
    setBatch(emptyBatchState);
    setBrowser(emptyBrowserState);
    setDownloadPrompted(false);
    setOverwritePrompt(null);
    setNotice("");
    if (fileInputRef.current) fileInputRef.current.value = "";
  }

  function clearBatchPanel() {
    setBatch(emptyBatchState);
    setDownloadPrompted(false);
    setOverwritePrompt(null);
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

  function addRemoteFile(entry) {
    if (!entry || entry.type !== "file") return;
    setDownloadQueue((current) => {
      if (current.some((item) => item.path === entry.path)) return current;
      return [
        ...current,
        {
          id: `remote-${entry.path}`,
          path: entry.path,
          name: entry.name,
          size: entry.size,
        },
      ];
    });
  }

  function removeQueueItem(id) {
    if (mode === "upload") {
      setUploadQueue((current) => current.filter((item) => item.id !== id));
    } else {
      setDownloadQueue((current) => current.filter((item) => item.id !== id));
    }
  }

  function moveQueueItem(id, direction) {
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

  async function saveDownloadBatch() {
    if (!batch.item) return;
    setBatch((current) => ({ ...current, state: "downloading", error: null }));
    try {
      const filename = batch.item.archive_name || batch.item.items?.[0]?.file_name || "aipermission-download";
      const result = await apiDownload(`/api/file-transfer-batches/${batch.item.id}/download`, filename, { picker: true });
      setDownloadPrompted(true);
      if (result?.canceled) {
        setNotice("Download was not saved. You can try Save download again.");
        setBatch((current) => ({ ...current, state: "ready", error: null }));
        return;
      }
      setNotice("Download saved.");
      setDownloadQueue([]);
      clearBatchPanel();
    } catch (error) {
      setDownloadPrompted(true);
      setBatch((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  function openBrowser(purpose) {
    const nextPath = normalizeRemoteDirectoryInput(remoteDir || defaultRemoteDir);
    setBrowser({ open: true, purpose, path: nextPath, state: "loading", data: null, error: null });
    void loadBrowser(nextPath, purpose);
  }

  async function loadBrowser(pathValue = browser.path, purpose = browser.purpose) {
    if (!server) return;
    const nextPath = normalizeRemoteDirectoryInput(pathValue || "/");
    setBrowser((current) => ({ ...current, purpose, path: nextPath, state: "loading", error: null }));
    try {
      const data = await apiPost("/api/file-transfers/browse", {
        server_id: Number(server.id),
        path: nextPath,
      });
      setBrowser({ open: true, purpose, path: data.path || nextPath, state: "ready", data, error: null });
    } catch (error) {
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
        onClose={onClose}
        size="wide"
        className="max-w-6xl"
        bodyClassName="grid max-h-[calc(100vh-130px)] min-h-0 overflow-hidden"
        autoFocusClose={false}
        closeOnOverlay={false}
        closeOnEscape={false}
      >
        <div className="grid min-h-0 gap-4 lg:grid-cols-[minmax(280px,0.9fr)_minmax(0,1.4fr)]">
          <section className="grid min-h-0 content-start gap-4">
            <Notice tone="warn">
              File transfers use SFTP over the server's existing gateway key. AIPermission stores metadata and progress only, never file contents.
            </Notice>
            {notice ? <Notice tone={notice.includes("canceled") ? "warn" : "good"}>{notice}</Notice> : null}
            <div className="grid grid-cols-2 gap-2 rounded-md border border-stone-200 bg-stone-50 p-1">
              <Button type="button" variant={mode === "upload" ? "default" : "ghost"} className="h-9" onClick={() => switchMode("upload")} disabled={Boolean(activeBatch)}>
                <Upload className="h-4 w-4" />
                Upload
              </Button>
              <Button type="button" variant={mode === "download" ? "default" : "ghost"} className="h-9" onClick={() => switchMode("download")} disabled={Boolean(activeBatch)}>
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
                </Field>
                <div className="grid grid-cols-2 gap-2">
                  <Button type="button" variant="outline" onClick={() => fileInputRef.current?.click()} disabled={Boolean(activeBatch)}>
                    <Upload className="h-4 w-4" />
                    Add files
                  </Button>
                  <Button type="button" variant="outline" onClick={() => openBrowser("upload")} disabled={!server || Boolean(activeBatch)}>
                    <FolderOpen className="h-4 w-4" />
                    Browse
                  </Button>
                </div>
                <input ref={fileInputRef} className="hidden" type="file" multiple onChange={handleLocalFileChange} />
              </div>
            ) : (
              <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
                <Field>
                  Remote folder
                  <Input
                    value={remoteDir}
                    onChange={(event) => setRemoteDir(event.target.value)}
                    placeholder={defaultRemoteDir}
                    disabled={Boolean(activeBatch)}
                  />
                </Field>
                <Button type="button" variant="outline" onClick={() => openBrowser("download")} disabled={!server || Boolean(activeBatch)}>
                  <FolderOpen className="h-4 w-4" />
                  Add remote files
                </Button>
                <p className="text-xs text-stone-500">Multiple downloads are saved as one temporary zip archive.</p>
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
              onRemove={removeQueueItem}
              onMove={moveQueueItem}
            />

            <div className="grid gap-3 border-t border-stone-200 pt-3">
              {overwritePrompt?.length ? (
                <Notice tone="warn" className="grid gap-3">
                  <div>
                    <p className="font-semibold">Some remote files already exist.</p>
                    <p className="mt-1 text-xs">Review before overwriting. Existing files will be replaced.</p>
                    <div className="mt-2 max-h-24 overflow-auto rounded border border-amber-200 bg-amber-50/70 p-2 font-mono text-xs">
                      {overwritePrompt.map((item) => (
                        <p key={item.remote_path} className="truncate">{item.remote_path}</p>
                      ))}
                    </div>
                  </div>
                  <div className="flex flex-wrap justify-end gap-2">
                    <Button type="button" variant="outline" className="h-9" onClick={() => setOverwritePrompt(null)}>
                      Cancel
                    </Button>
                    <Button type="button" variant="danger" className="h-9" onClick={() => startQueue({ overwrite: true })}>
                      Overwrite all
                    </Button>
                  </div>
                </Notice>
              ) : null}

              {batch.error ? <Notice tone="bad">{batch.error}</Notice> : null}

              <div className="flex flex-wrap items-center justify-between gap-2">
                <Button type="button" disabled={!canStart} onClick={() => startQueue()}>
                  {mode === "upload" ? <Upload className="h-4 w-4" /> : <Download className="h-4 w-4" />}
                  Start {mode}
                </Button>
                <div className="flex flex-wrap justify-end gap-2">
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
                      {downloadPrompted ? "Save download" : "Saving..."}
                    </Button>
                  ) : null}
                  {batch.item && !activeBatch ? (
                    <Button type="button" variant="outline" className="h-10" onClick={clearBatchPanel}>
                      Done
                    </Button>
                  ) : null}
                </div>
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
        onAddFile={addRemoteFile}
      />
    </>
  );
}

function QueueSummary({ batch, queue, mode, progress }) {
  const totalSize = batch ? batch.size_bytes : queue.reduce((sum, item) => sum + Number(item.size || 0), 0);
  const totalItems = batch ? batch.total_items : queue.length;
  return (
    <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide text-stone-500">Summary</p>
          <p className="mt-1 text-sm font-semibold text-stone-900">{totalItems} item{totalItems === 1 ? "" : "s"}</p>
        </div>
        <span className="rounded-full border border-stone-200 px-2.5 py-1 text-xs font-semibold text-stone-600">{batch?.status || mode}</span>
      </div>
      <div className="grid gap-2 text-sm">
        <div className="flex justify-between gap-3">
          <span className="text-stone-500">Total size</span>
          <span className="font-mono text-stone-800">{formatBytes(totalSize)}</span>
        </div>
        <div className="flex justify-between gap-3">
          <span className="text-stone-500">Progress</span>
          <span className="font-mono text-stone-800">{progress.percent}%</span>
        </div>
        <div className="flex justify-between gap-3">
          <span className="text-stone-500">Speed</span>
          <span className="font-mono text-stone-800">{batch?.bytes_per_second ? `${formatBytes(batch.bytes_per_second)}/s` : "-"}</span>
        </div>
        <div className="flex justify-between gap-3">
          <span className="text-stone-500">ETA</span>
          <span className="font-mono text-stone-800">{formatETA(batch?.eta_seconds)}</span>
        </div>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-stone-100">
        <div className={cn("h-full rounded-full bg-emerald-700 transition-all", batch?.status === "running" ? "animate-pulse" : "")} style={{ width: `${progress.percent}%` }} />
      </div>
    </div>
  );
}

function QueueList({ mode, queue, batch, active, onRemove, onMove }) {
  const items = batch?.items || queue;
  if (items.length === 0) {
    return (
      <div className="grid min-h-72 place-items-center rounded-md border border-dashed border-stone-300 bg-stone-50 p-8 text-center text-sm text-stone-500">
        {mode === "upload" ? "Add local files to build an upload queue." : "Add remote files to build a download queue."}
      </div>
    );
  }
  return (
    <div className="min-h-0 overflow-hidden rounded-md border border-stone-200 bg-white">
      <div className="max-h-[48vh] overflow-auto">
        {items.map((item, index) => (
          <QueueRow
            key={item.id || item.path || item.remote_path}
            item={item}
            index={index}
            total={items.length}
            active={active}
            batchMode={Boolean(batch)}
            mode={mode}
            onRemove={onRemove}
            onMove={onMove}
          />
        ))}
      </div>
    </div>
  );
}

function QueueRow({ item, index, total, active, batchMode, mode, onRemove, onMove }) {
  const name = item.file_name || item.name || item.path || item.remote_path;
  const source = mode === "upload" ? item.remote_path : item.path || item.remote_path;
  const progress = transferProgress(item.status ? item : null);
  return (
    <div className="grid gap-2 border-b border-stone-100 p-3 last:border-b-0">
      <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-stone-900">{name}</p>
          <p className="truncate font-mono text-xs text-stone-500">{source}</p>
        </div>
        <div className="flex items-center gap-1">
          {!batchMode ? (
            <>
              <Button type="button" variant="ghost" className="h-8 w-8 px-0" onClick={() => onMove(item.id, -1)} disabled={active || index === 0}>
                <ArrowUp className="h-4 w-4" />
              </Button>
              <Button type="button" variant="ghost" className="h-8 w-8 px-0" onClick={() => onMove(item.id, 1)} disabled={active || index === total - 1}>
                <ArrowDown className="h-4 w-4" />
              </Button>
              <Button type="button" variant="ghost" className="h-8 w-8 px-0 text-red-700" onClick={() => onRemove(item.id)} disabled={active}>
                <Trash2 className="h-4 w-4" />
              </Button>
            </>
          ) : (
            <span className="rounded-full border border-stone-200 px-2 py-1 text-xs font-semibold text-stone-600">{item.status}</span>
          )}
        </div>
      </div>
      <div className="flex items-center justify-between gap-3 text-xs text-stone-500">
        <span>{formatBytes(item.size || item.size_bytes || 0)}</span>
        {batchMode ? <span>{formatETA(item.eta_seconds)}</span> : <span>#{index + 1}</span>}
      </div>
      {batchMode ? (
        <div className="h-1.5 overflow-hidden rounded-full bg-stone-100">
          <div className={cn("h-full rounded-full bg-emerald-700 transition-all", item.status === "running" ? "animate-pulse" : "")} style={{ width: `${progress.percent}%` }} />
        </div>
      ) : null}
      {item.error ? <p className="text-xs text-red-700">{item.error}</p> : null}
    </div>
  );
}

function RemoteBrowserDialog({ browser, onClose, onLoad, onPathChange, onUseDirectory, onAddFile }) {
  if (!browser.open) return null;
  const entries = browser.data?.entries || [];
  const canUseCurrentDirectory = browser.purpose === "upload";

  return (
    <Dialog
      open={browser.open}
      title={browser.purpose === "upload" ? "Choose remote folder" : "Add remote files"}
      description="Browse the selected server over SFTP."
      onClose={onClose}
      size="xl"
      className="max-h-[calc(100vh-80px)]"
      bodyClassName="grid min-h-0 gap-4 overflow-hidden"
      autoFocusClose={false}
      closeOnOverlay={false}
      closeOnEscape={false}
    >
      <div className="grid gap-3">
        <div className="flex flex-wrap items-end gap-2">
          <Field className="min-w-0 flex-1">
            Remote path
            <Input
              value={browser.path}
              onChange={(event) => onPathChange(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  void onLoad(browser.path, browser.purpose);
                }
              }}
            />
          </Field>
          <Button type="button" variant="outline" className="h-10" onClick={() => onLoad(browser.path, browser.purpose)} disabled={browser.state === "loading"}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          {canUseCurrentDirectory ? (
            <Button type="button" className="h-10" onClick={() => onUseDirectory(browser.path)}>
              Use this folder
            </Button>
          ) : null}
        </div>

        {browser.error ? <Notice tone="bad">{browser.error}</Notice> : null}

        <div className="min-h-80 overflow-hidden rounded-md border border-stone-200 bg-white">
          <div className="max-h-[50vh] overflow-auto">
            <button
              type="button"
              className="grid w-full grid-cols-[24px_minmax(0,1fr)_120px_170px] items-center gap-3 border-b border-stone-100 px-3 py-2 text-left text-sm transition hover:bg-stone-50"
              onClick={() => onLoad(browser.data?.parent || "/", browser.purpose)}
            >
              <Folder className="h-4 w-4 text-emerald-700" />
              <span className="truncate font-medium">..</span>
              <span className="text-xs text-stone-500">parent</span>
              <span />
            </button>
            {browser.state === "loading" ? (
              <p className="px-3 py-8 text-center text-sm text-stone-500">Loading remote files...</p>
            ) : null}
            {browser.state !== "loading" && entries.length === 0 ? (
              <p className="px-3 py-8 text-center text-sm text-stone-500">No files in this directory.</p>
            ) : null}
            {browser.state !== "loading"
              ? entries.map((entry) => (
                  <div
                    key={entry.path}
                    className="grid w-full grid-cols-[24px_minmax(0,1fr)_120px_170px] items-center gap-3 border-b border-stone-100 px-3 py-2 text-left text-sm transition hover:bg-stone-50"
                  >
                    {entry.type === "directory" ? <Folder className="h-4 w-4 text-emerald-700" /> : <File className="h-4 w-4 text-stone-500" />}
                    <button type="button" className="min-w-0 text-left" onClick={() => (entry.type === "directory" ? onLoad(entry.path, browser.purpose) : onAddFile(entry))}>
                      <span className="block truncate font-medium">{entry.name}</span>
                    </button>
                    <span className="text-xs text-stone-500">{entry.type === "directory" ? "folder" : formatBytes(entry.size)}</span>
                    <span className="truncate text-right text-xs text-stone-500">{formatShortDate(entry.modified_at)}</span>
                  </div>
                ))
              : null}
          </div>
        </div>
      </div>
    </Dialog>
  );
}

function transferProgress(item) {
  if (!item) return { percent: 0, label: "" };
  const total = Number(item.size_bytes || 0);
  const transferred = Number(item.transferred_bytes || 0);
  const percent = total > 0 ? Math.min(100, Math.round((transferred / total) * 100)) : item.status === "completed" ? 100 : 0;
  return {
    percent,
    label: total > 0 ? `${formatBytes(transferred)} / ${formatBytes(total)}` : `${formatBytes(transferred)} transferred`,
  };
}

function defaultRemoteDirectory(server) {
  return "/home";
}

function joinRemotePath(remoteDir, remoteName) {
  const dir = normalizeRemoteDirectoryInput(remoteDir);
  const name = String(remoteName || "").trim().replace(/^\/+/, "");
  if (dir === "/") return `/${name}`;
  return `${dir.replace(/\/+$/, "")}/${name}`;
}

function normalizeRemoteDirectoryInput(value) {
  const text = String(value || "").trim() || "/";
  if (!text.startsWith("/")) return `/${text}`;
  return text.replace(/\/+$/, "") || "/";
}

function localFileID(file) {
  return `${file.name}-${file.size}-${file.lastModified}-${Math.random().toString(16).slice(2)}`;
}

function suggestedArchiveName() {
  const stamp = new Date().toISOString().slice(0, 19).replaceAll(":", "-");
  return `aipermission-download-${stamp}.zip`;
}

function formatBytes(value) {
  const bytes = Number(value || 0);
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let amount = bytes / 1024;
  let index = 0;
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024;
    index += 1;
  }
  return `${amount.toFixed(amount >= 10 ? 1 : 2)} ${units[index]}`;
}

function formatETA(value) {
  const seconds = Number(value);
  if (!Number.isFinite(seconds) || seconds < 0) return "-";
  if (seconds < 60) return `${Math.max(0, Math.round(seconds))}s`;
  const minutes = Math.floor(seconds / 60);
  const rest = Math.round(seconds % 60);
  return `${minutes}m ${rest}s`;
}

function formatShortDate(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
