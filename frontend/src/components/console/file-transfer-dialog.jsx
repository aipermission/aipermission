import { Download, RefreshCcw, Upload } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { apiDownload, apiGet, apiPost, apiPostForm } from "../../lib/api";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";

const emptyTransferState = { state: "idle", item: null, error: null };

export function FileTransferDialog({ open, server, onClose }) {
  const [mode, setMode] = useState("upload");
  const [uploadForm, setUploadForm] = useState({ remote_path: "", file: null });
  const [downloadForm, setDownloadForm] = useState({ remote_path: "" });
  const [transfer, setTransfer] = useState(emptyTransferState);
  const fileInputRef = useRef(null);

  useEffect(() => {
    if (!open) {
      setUploadForm({ remote_path: "", file: null });
      setDownloadForm({ remote_path: "" });
      setTransfer(emptyTransferState);
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
  }, [open]);

  useEffect(() => {
    if (!open || !transfer.item || !["pending", "running"].includes(transfer.item.status)) return undefined;
    const timer = window.setInterval(() => {
      void refreshTransfer(transfer.item.id, { silent: true });
    }, 900);
    return () => window.clearInterval(timer);
  }, [open, transfer.item?.id, transfer.item?.status]);

  const progress = useMemo(() => transferProgress(transfer.item), [transfer.item]);

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

  async function submitUpload(event) {
    event.preventDefault();
    if (!server || !uploadForm.file || !uploadForm.remote_path.trim()) return;
    const formData = new FormData();
    formData.append("server_id", String(server.id));
    formData.append("remote_path", uploadForm.remote_path.trim());
    formData.append("file", uploadForm.file);
    setTransfer({ state: "starting", item: null, error: null });
    try {
      const item = await apiPostForm("/api/file-transfers/upload", formData);
      setTransfer({ state: "ready", item, error: null });
    } catch (error) {
      setTransfer({ state: "error", item: null, error: error.message });
    }
  }

  async function submitDownload(event) {
    event.preventDefault();
    if (!server || !downloadForm.remote_path.trim()) return;
    setTransfer({ state: "starting", item: null, error: null });
    try {
      const item = await apiPost("/api/file-transfers/download", {
        server_id: Number(server.id),
        remote_path: downloadForm.remote_path.trim(),
      });
      setTransfer({ state: "ready", item, error: null });
    } catch (error) {
      setTransfer({ state: "error", item: null, error: error.message });
    }
  }

  async function downloadResult() {
    if (!transfer.item) return;
    try {
      await apiDownload(`/api/file-transfers/${transfer.item.id}/download`, transfer.item.file_name || "aipermission-download");
    } catch (error) {
      setTransfer((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  return (
    <Dialog
      open={open}
      title={server ? `${server.name} files` : "File transfer"}
      description="Upload or download one file over the selected server's SSH connection."
      onClose={onClose}
      size="lg"
      autoFocusClose={false}
    >
      <div className="grid gap-4">
        <Notice tone="warn">
          File transfers use SFTP over the server's existing gateway key. AIPermission stores metadata and progress only, never file contents.
        </Notice>
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
              <Input
                ref={fileInputRef}
                type="file"
                onChange={(event) => setUploadForm((current) => ({ ...current, file: event.target.files?.[0] || null }))}
                required
              />
            </Field>
            <Field>
              Remote path
              <Input
                value={uploadForm.remote_path}
                onChange={(event) => setUploadForm((current) => ({ ...current, remote_path: event.target.value }))}
                placeholder="/tmp/aipermission-upload.txt"
                required
              />
            </Field>
            <Button type="submit" disabled={!server || transfer.state === "starting"}>
              <Upload className="h-4 w-4" />
              Start upload
            </Button>
          </form>
        ) : (
          <form className="grid gap-3" onSubmit={submitDownload}>
            <Field>
              Remote path
              <Input
                value={downloadForm.remote_path}
                onChange={(event) => setDownloadForm({ remote_path: event.target.value })}
                placeholder="/var/log/syslog"
                required
              />
            </Field>
            <Button type="submit" disabled={!server || transfer.state === "starting"}>
              <Download className="h-4 w-4" />
              Start download
            </Button>
          </form>
        )}

        {transfer.error ? <Notice tone="bad">{transfer.error}</Notice> : null}
        {transfer.item ? (
          <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p className="text-sm font-semibold">{transfer.item.file_name || transfer.item.remote_path}</p>
                <p className="font-mono text-xs text-stone-500">{transfer.item.remote_path}</p>
              </div>
              <span className="rounded-full border border-stone-200 px-2.5 py-1 text-xs font-semibold text-stone-600">
                {transfer.item.status}
              </span>
            </div>
            <div className="grid gap-2">
              <div className="h-2 overflow-hidden rounded-full bg-stone-100">
                <div className="h-full rounded-full bg-emerald-700 transition-all" style={{ width: `${progress.percent}%` }} />
              </div>
              <div className="flex items-center justify-between gap-3 text-xs text-stone-500">
                <span>{progress.label}</span>
                <span>{progress.percent}%</span>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" className="h-9" onClick={() => refreshTransfer()} disabled={transfer.state === "loading"}>
                <RefreshCcw className="h-4 w-4" />
                Refresh
              </Button>
              {transfer.item.direction === "download" && transfer.item.status === "completed" ? (
                <Button type="button" className="h-9" onClick={downloadResult}>
                  <Download className="h-4 w-4" />
                  Save download
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}
      </div>
    </Dialog>
  );
}

function transferProgress(item) {
  if (!item) return { percent: 0, label: "" };
  const total = Number(item.size_bytes || 0);
  const transferred = Number(item.transferred_bytes || 0);
  const percent = total > 0 ? Math.min(100, Math.round((transferred / total) * 100)) : ["completed", "failed"].includes(item.status) ? 100 : 0;
  return {
    percent,
    label: total > 0 ? `${formatBytes(transferred)} / ${formatBytes(total)}` : `${formatBytes(transferred)} transferred`,
  };
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
