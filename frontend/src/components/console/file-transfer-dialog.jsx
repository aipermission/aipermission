import { Download, File, Folder, FolderOpen, RefreshCcw, Upload } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { apiDownload, apiGet, apiPost, apiPostForm } from "../../lib/api";
import { cn } from "../../lib/utils";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";

const emptyTransferState = { state: "idle", item: null, error: null };
const emptyBrowserState = { open: false, purpose: "upload", path: "/", state: "idle", data: null, error: null };

export function FileTransferDialog({ open, server, onClose }) {
  const defaultRemoteDir = useMemo(() => defaultRemoteDirectory(server), [server]);
  const [mode, setMode] = useState("upload");
  const [uploadForm, setUploadForm] = useState({ remote_dir: defaultRemoteDir, remote_name: "", file: null });
  const [downloadForm, setDownloadForm] = useState({ remote_dir: defaultRemoteDir, remote_name: "" });
  const [transfer, setTransfer] = useState(emptyTransferState);
  const [browser, setBrowser] = useState(emptyBrowserState);
  const [downloadPrompted, setDownloadPrompted] = useState(false);
  const [overwritePrompt, setOverwritePrompt] = useState(null);
  const [notice, setNotice] = useState("");
  const fileInputRef = useRef(null);

  const activeTransfer = transfer.item && ["pending", "running"].includes(transfer.item.status);
  const progress = useMemo(() => transferProgress(transfer.item), [transfer.item]);

  useEffect(() => {
    if (!open) {
      resetDialog(defaultRemoteDir);
      return;
    }
    setUploadForm((current) => ({ ...current, remote_dir: current.remote_dir || defaultRemoteDir }));
    setDownloadForm((current) => ({ ...current, remote_dir: current.remote_dir || defaultRemoteDir }));
  }, [open, defaultRemoteDir]);

  useEffect(() => {
    if (!open || !transfer.item || !["pending", "running"].includes(transfer.item.status)) return undefined;
    const timer = window.setInterval(() => {
      void refreshTransfer(transfer.item.id, { silent: true });
    }, 900);
    return () => window.clearInterval(timer);
  }, [open, transfer.item?.id, transfer.item?.status]);

  useEffect(() => {
    if (!transfer.item || transfer.item.status !== "completed") return;
    if (transfer.item.direction === "upload") {
      setNotice("Upload completed.");
      resetUploadForm(defaultRemoteDir);
      clearTransferPanel();
      return;
    }
    if (transfer.item.direction === "download" && !downloadPrompted && transfer.state !== "downloading") {
      void saveDownload();
    }
  }, [transfer.item?.id, transfer.item?.status, transfer.item?.direction, downloadPrompted, transfer.state]);

  function resetDialog(remoteDir = defaultRemoteDir) {
    setMode("upload");
    resetUploadForm(remoteDir);
    resetDownloadForm(remoteDir);
    setTransfer(emptyTransferState);
    setBrowser(emptyBrowserState);
    setDownloadPrompted(false);
    setOverwritePrompt(null);
    setNotice("");
  }

  function resetUploadForm(remoteDir = defaultRemoteDir) {
    setUploadForm({ remote_dir: remoteDir, remote_name: "", file: null });
    if (fileInputRef.current) fileInputRef.current.value = "";
  }

  function resetDownloadForm(remoteDir = defaultRemoteDir) {
    setDownloadForm({ remote_dir: remoteDir, remote_name: "" });
  }

  function clearTransferPanel() {
    setTransfer(emptyTransferState);
    setDownloadPrompted(false);
  }

  async function refreshTransfer(id = transfer.item?.id, options = {}) {
    if (!id) return;
    if (!options.silent) {
      setTransfer((current) => ({ ...current, state: "loading", error: null }));
    }
    try {
      const item = await apiGet(`/api/file-transfers/${id}`);
      setTransfer({ state: "ready", item, error: null });
    } catch (error) {
      setTransfer((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  async function startUpload(options = {}) {
    if (!server || !uploadForm.file || !uploadForm.remote_dir.trim() || !uploadForm.remote_name.trim()) return;
    const remotePath = joinRemotePath(uploadForm.remote_dir, uploadForm.remote_name);
    const formData = new FormData();
    formData.append("server_id", String(server.id));
    formData.append("remote_path", remotePath);
    formData.append("overwrite", options.overwrite ? "true" : "false");
    formData.append("file", uploadForm.file);
    setNotice("");
    setOverwritePrompt(null);
    setDownloadPrompted(false);
    setTransfer({ state: "starting", item: null, error: null });
    try {
      const item = await apiPostForm("/api/file-transfers/upload", formData);
      setTransfer({ state: "ready", item, error: null });
    } catch (error) {
      if (error.status === 409 && error.data?.code === "remote_file_exists") {
        setTransfer(emptyTransferState);
        setOverwritePrompt({
          remote_path: error.data.remote_path || remotePath,
          size: error.data.size || 0,
        });
        return;
      }
      setTransfer({ state: "error", item: null, error: error.message });
    }
  }

  async function submitUpload(event) {
    event.preventDefault();
    await startUpload();
  }

  async function submitDownload(event) {
    event.preventDefault();
    if (!server || !downloadForm.remote_dir.trim() || !downloadForm.remote_name.trim()) return;
    const remotePath = joinRemotePath(downloadForm.remote_dir, downloadForm.remote_name);
    setNotice("");
    setDownloadPrompted(false);
    setTransfer({ state: "starting", item: null, error: null });
    try {
      const item = await apiPost("/api/file-transfers/download", {
        server_id: Number(server.id),
        remote_path: remotePath,
      });
      setTransfer({ state: "ready", item, error: null });
    } catch (error) {
      setTransfer({ state: "error", item: null, error: error.message });
    }
  }

  async function cancelTransfer() {
    if (!transfer.item) return;
    setTransfer((current) => ({ ...current, state: "canceling", error: null }));
    try {
      const item = await apiPost(`/api/file-transfers/${transfer.item.id}/cancel`, {});
      setTransfer({ state: "ready", item, error: null });
      setNotice("Transfer canceled.");
    } catch (error) {
      setTransfer((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  async function saveDownload() {
    if (!transfer.item) return;
    setTransfer((current) => ({ ...current, state: "downloading", error: null }));
    try {
      const result = await apiDownload(`/api/file-transfers/${transfer.item.id}/download`, transfer.item.file_name || "aipermission-download", { picker: true });
      setDownloadPrompted(true);
      if (result?.canceled) {
        setNotice("Download was not saved. You can try Save download again.");
        setTransfer((current) => ({ ...current, state: "ready", error: null }));
        return;
      }
      setNotice("Download saved.");
      resetDownloadForm(defaultRemoteDir);
      clearTransferPanel();
    } catch (error) {
      setDownloadPrompted(true);
      setTransfer((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  function openBrowser(purpose) {
    const pathValue = purpose === "upload" ? uploadForm.remote_dir : downloadForm.remote_dir;
    const nextPath = normalizeRemoteDirectoryInput(pathValue || defaultRemoteDir);
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
    if (browser.purpose === "upload") {
      setUploadForm((current) => ({ ...current, remote_dir: pathValue }));
    } else {
      setDownloadForm((current) => ({ ...current, remote_dir: pathValue }));
    }
    setBrowser(emptyBrowserState);
  }

  function useBrowserFile(entry) {
    const dir = remoteDirName(entry.path);
    if (browser.purpose === "download") {
      setDownloadForm({ remote_dir: dir, remote_name: entry.name });
      setBrowser(emptyBrowserState);
    }
  }

  function handleLocalFileChange(event) {
    const file = event.target.files?.[0] || null;
    setUploadForm((current) => ({
      ...current,
      file,
      remote_name: file && !current.remote_name ? file.name : current.remote_name,
    }));
  }

  return (
    <>
      <Dialog
        open={open}
        title={server ? `${server.name} files` : "File transfer"}
        description="Upload or download one file over the selected server's SSH connection."
        onClose={onClose}
        size="lg"
        autoFocusClose={false}
        closeOnOverlay={false}
        closeOnEscape={false}
      >
        <div className="grid gap-4">
          <Notice tone="warn">
            File transfers use SFTP over the server's existing gateway key. AIPermission stores metadata and progress only, never file contents.
          </Notice>
          {notice ? <Notice tone={notice.includes("canceled") ? "warn" : "good"}>{notice}</Notice> : null}
          <div className="grid grid-cols-2 gap-2 rounded-md border border-stone-200 bg-stone-50 p-1">
            <Button type="button" variant={mode === "upload" ? "default" : "ghost"} className="h-9" onClick={() => setMode("upload")}>
              <Upload className="h-4 w-4" />
              Upload
            </Button>
            <Button type="button" variant={mode === "download" ? "default" : "ghost"} className="h-9" onClick={() => setMode("download")}>
              <Download className="h-4 w-4" />
              Download
            </Button>
          </div>

          {mode === "upload" ? (
            <form className="grid gap-3" onSubmit={submitUpload}>
              <Field>
                Local file
                <Input ref={fileInputRef} type="file" onChange={handleLocalFileChange} required disabled={Boolean(activeTransfer)} />
              </Field>
              <div className="grid gap-3 md:grid-cols-[minmax(0,3fr)_minmax(0,2fr)_auto] md:items-end">
                <Field>
                  Remote path
                  <Input
                    value={uploadForm.remote_dir}
                    onChange={(event) => setUploadForm((current) => ({ ...current, remote_dir: event.target.value }))}
                    placeholder={defaultRemoteDir}
                    required
                    disabled={Boolean(activeTransfer)}
                  />
                </Field>
                <Field>
                  Remote file
                  <Input
                    value={uploadForm.remote_name}
                    onChange={(event) => setUploadForm((current) => ({ ...current, remote_name: event.target.value }))}
                    placeholder="app.log"
                    required
                    disabled={Boolean(activeTransfer)}
                  />
                </Field>
                <Button type="button" variant="outline" className="h-10" onClick={() => openBrowser("upload")} disabled={!server || Boolean(activeTransfer)}>
                  <FolderOpen className="h-4 w-4" />
                  Browse
                </Button>
              </div>
              {overwritePrompt ? (
                <Notice tone="warn" className="grid gap-3">
                  <div>
                    <p className="font-semibold">Remote file already exists.</p>
                    <p className="mt-1 break-all font-mono text-xs">{overwritePrompt.remote_path}</p>
                    {overwritePrompt.size > 0 ? <p className="mt-1 text-xs">Existing size: {formatBytes(overwritePrompt.size)}</p> : null}
                  </div>
                  <div className="flex flex-wrap justify-end gap-2">
                    <Button type="button" variant="outline" className="h-9" onClick={() => setOverwritePrompt(null)}>
                      Cancel
                    </Button>
                    <Button type="button" variant="danger" className="h-9" onClick={() => startUpload({ overwrite: true })}>
                      Overwrite file
                    </Button>
                  </div>
                </Notice>
              ) : null}
              {!activeTransfer ? (
                <Button type="submit" disabled={!server || transfer.state === "starting"}>
                  <Upload className="h-4 w-4" />
                  Start upload
                </Button>
              ) : null}
            </form>
          ) : (
            <form className="grid gap-3" onSubmit={submitDownload}>
              <div className="grid gap-3 md:grid-cols-[minmax(0,3fr)_minmax(0,2fr)_auto] md:items-end">
                <Field>
                  Remote path
                  <Input
                    value={downloadForm.remote_dir}
                    onChange={(event) => setDownloadForm((current) => ({ ...current, remote_dir: event.target.value }))}
                    placeholder={defaultRemoteDir}
                    required
                    disabled={Boolean(activeTransfer)}
                  />
                </Field>
                <Field>
                  Remote file
                  <Input
                    value={downloadForm.remote_name}
                    onChange={(event) => setDownloadForm((current) => ({ ...current, remote_name: event.target.value }))}
                    placeholder="syslog"
                    required
                    disabled={Boolean(activeTransfer)}
                  />
                </Field>
                <Button type="button" variant="outline" className="h-10" onClick={() => openBrowser("download")} disabled={!server || Boolean(activeTransfer)}>
                  <FolderOpen className="h-4 w-4" />
                  Browse
                </Button>
              </div>
              {!activeTransfer ? (
                <Button type="submit" disabled={!server || transfer.state === "starting"}>
                  <Download className="h-4 w-4" />
                  Start download
                </Button>
              ) : null}
            </form>
          )}

          {transfer.error ? <Notice tone="bad">{transfer.error}</Notice> : null}
          {transfer.item ? (
            <TransferPanel
              item={transfer.item}
              state={transfer.state}
              progress={progress}
              downloadPrompted={downloadPrompted}
              onRefresh={() => refreshTransfer()}
              onCancel={cancelTransfer}
              onSaveDownload={() => saveDownload()}
              onDone={clearTransferPanel}
            />
          ) : null}
        </div>
      </Dialog>

      <RemoteBrowserDialog
        browser={browser}
        onClose={() => setBrowser(emptyBrowserState)}
        onLoad={loadBrowser}
        onPathChange={(path) => setBrowser((current) => ({ ...current, path }))}
        onUseDirectory={useBrowserDirectory}
        onUseFile={useBrowserFile}
      />
    </>
  );
}

function TransferPanel({ item, state, progress, downloadPrompted, onRefresh, onCancel, onSaveDownload, onDone }) {
  const active = ["pending", "running"].includes(item.status);
  return (
    <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold">{item.file_name || item.remote_path}</p>
          <p className="truncate font-mono text-xs text-stone-500">{item.remote_path}</p>
        </div>
        <span className="rounded-full border border-stone-200 px-2.5 py-1 text-xs font-semibold text-stone-600">{item.status}</span>
      </div>
      <div className="grid gap-2">
        <div className="h-2 overflow-hidden rounded-full bg-stone-100">
          <div
            className={cn("h-full rounded-full bg-emerald-700 transition-all", active ? "animate-pulse" : "")}
            style={{ width: `${progress.percent}%` }}
          />
        </div>
        <div className="flex items-center justify-between gap-3 text-xs text-stone-500">
          <span>{progress.label}</span>
          <span>{progress.percent}%</span>
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button type="button" variant="outline" className="h-9" onClick={onRefresh} disabled={state === "loading"}>
          <RefreshCcw className="h-4 w-4" />
          Refresh
        </Button>
        <div className="flex flex-wrap justify-end gap-2">
          {active ? (
            <Button type="button" variant="danger" className="h-9" onClick={onCancel} disabled={state === "canceling"}>
              Cancel {item.direction}
            </Button>
          ) : null}
          {item.direction === "download" && item.status === "completed" ? (
            <>
              <Button type="button" className="h-9" onClick={onSaveDownload} disabled={state === "downloading"}>
                <Download className="h-4 w-4" />
                {downloadPrompted ? "Save download" : "Saving..."}
              </Button>
              <Button type="button" variant="outline" className="h-9" onClick={onDone}>
                Done
              </Button>
            </>
          ) : null}
          {!active && item.direction === "upload" ? (
            <Button type="button" variant="outline" className="h-9" onClick={onDone}>
              Done
            </Button>
          ) : null}
          {!active && item.status !== "completed" ? (
            <Button type="button" variant="outline" className="h-9" onClick={onDone}>
              Close
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function RemoteBrowserDialog({ browser, onClose, onLoad, onPathChange, onUseDirectory, onUseFile }) {
  if (!browser.open) return null;
  const entries = browser.data?.entries || [];
  const canUseCurrentDirectory = browser.purpose === "upload";

  return (
    <Dialog
      open={browser.open}
      title={browser.purpose === "upload" ? "Choose remote folder" : "Choose remote file"}
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
                  <button
                    key={entry.path}
                    type="button"
                    className="grid w-full grid-cols-[24px_minmax(0,1fr)_120px_170px] items-center gap-3 border-b border-stone-100 px-3 py-2 text-left text-sm transition hover:bg-stone-50"
                    onClick={() => (entry.type === "directory" ? onLoad(entry.path, browser.purpose) : onUseFile(entry))}
                    disabled={browser.purpose === "upload" && entry.type !== "directory"}
                  >
                    {entry.type === "directory" ? <Folder className="h-4 w-4 text-emerald-700" /> : <File className="h-4 w-4 text-stone-500" />}
                    <span className="truncate font-medium">{entry.name}</span>
                    <span className="text-xs text-stone-500">{entry.type === "directory" ? "folder" : formatBytes(entry.size)}</span>
                    <span className="truncate text-right text-xs text-stone-500">{formatShortDate(entry.modified_at)}</span>
                  </button>
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

function remoteDirName(value) {
  const path = String(value || "").trim();
  const index = path.lastIndexOf("/");
  if (index <= 0) return "/";
  return path.slice(0, index);
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

function formatShortDate(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
