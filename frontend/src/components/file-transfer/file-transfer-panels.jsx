import { Download, FolderOpen, Pause, Play, RefreshCcw, Upload } from "lucide-react";
import { Button } from "../ui/button";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";
import { fileTransferFailureText } from "../../lib/file-transfer-utils";
import { QueueList, QueueSummary } from "./file-transfer-queue";

export function TransferSetupPanel({
  runtimeTarget,
  mode,
  batch,
  activeBatch,
  queue,
  progress,
  notice,
  transferNotice,
  remoteDir,
  defaultRemoteDir,
  recursive,
  fileInputRef,
  folderInputRef,
  onModeChange,
  onRemoteDirectoryChange,
  onOpenBrowser,
  onLocalFileChange,
}) {
  return (
    <section className="grid min-h-0 content-start gap-4">
      <Notice tone="warn">{transferNotice}</Notice>
      {notice ? <Notice tone={notice.tone}>{notice.message}</Notice> : null}
      <div className="grid grid-cols-2 gap-2 rounded-md border border-stone-200 bg-stone-50 p-1">
        <Button
          type="button"
          variant={mode === "upload" ? "default" : "ghost"}
          className="h-9"
          onClick={() => onModeChange("upload")}
          disabled={Boolean(batch.item) || batch.state === "starting"}
        >
          <Upload className="h-4 w-4" />
          Upload
        </Button>
        <Button
          type="button"
          variant={mode === "download" ? "default" : "ghost"}
          className="h-9"
          onClick={() => onModeChange("download")}
          disabled={Boolean(batch.item) || batch.state === "starting"}
        >
          <Download className="h-4 w-4" />
          Download
        </Button>
      </div>

      <div className="rounded-md border border-stone-200 bg-white p-4">
        <p className="text-xs font-semibold uppercase tracking-wide text-stone-500">Target</p>
        <p className="mt-2 truncate text-sm font-semibold text-stone-900">{runtimeTarget?.name || "No target selected"}</p>
        <p className="truncate font-mono text-xs text-stone-500">{runtimeTarget?.subtitle || "Open Console and select a target first."}</p>
      </div>

      {mode === "upload" ? (
        <UploadSourcePanel
          runtimeTarget={runtimeTarget}
          activeBatch={activeBatch}
          remoteDir={remoteDir}
          defaultRemoteDir={defaultRemoteDir}
          recursive={recursive}
          fileInputRef={fileInputRef}
          folderInputRef={folderInputRef}
          onRemoteDirectoryChange={onRemoteDirectoryChange}
          onOpenBrowser={onOpenBrowser}
          onLocalFileChange={onLocalFileChange}
        />
      ) : (
        <DownloadSourcePanel
          runtimeTarget={runtimeTarget}
          activeBatch={activeBatch}
          defaultRemoteDir={defaultRemoteDir}
          onOpenBrowser={onOpenBrowser}
        />
      )}

      <QueueSummary batch={batch.item} queue={queue} mode={mode} progress={progress} />
    </section>
  );
}

function UploadSourcePanel({
  runtimeTarget,
  activeBatch,
  remoteDir,
  defaultRemoteDir,
  recursive,
  fileInputRef,
  folderInputRef,
  onRemoteDirectoryChange,
  onOpenBrowser,
  onLocalFileChange,
}) {
  return (
    <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
      <Field>
        Remote folder
        <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
          <Input
            value={remoteDir}
            onChange={(event) => onRemoteDirectoryChange(event.target.value)}
            placeholder={defaultRemoteDir}
            disabled={Boolean(activeBatch)}
          />
          <Button
            type="button"
            variant="outline"
            className="h-10"
            onClick={() => onOpenBrowser("upload")}
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
      <input ref={fileInputRef} className="hidden" type="file" multiple onChange={onLocalFileChange} />
      {recursive ? (
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
          <input ref={folderInputRef} className="hidden" type="file" multiple webkitdirectory="" onChange={onLocalFileChange} />
        </>
      ) : null}
    </div>
  );
}

function DownloadSourcePanel({ runtimeTarget, activeBatch, defaultRemoteDir, onOpenBrowser }) {
  return (
    <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
      <Button
        type="button"
        variant="outline"
        className="w-full"
        onClick={() => onOpenBrowser("download")}
        disabled={!runtimeTarget || Boolean(activeBatch)}
      >
        <FolderOpen className="h-4 w-4" />
        Add remote files
      </Button>
      <p className="text-xs text-stone-500">
        The browser opens at the last folder used for this target, or <code>{defaultRemoteDir}</code> when no folder is remembered. Multiple
        downloads are saved as one temporary zip archive.
      </p>
    </div>
  );
}

export function TransferQueuePanel({
  mode,
  queue,
  batch,
  activeBatch,
  canStart,
  onRefresh,
  onRemove,
  onMove,
  onPause,
  onResume,
  onCancel,
  onSaveDownload,
  onClear,
  onStart,
}) {
  const failureMessage = fileTransferFailureText(batch.item, batch.error);
  return (
    <section className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-sm font-semibold text-stone-900">Queue</p>
          <p className="text-xs text-stone-500">
            {activeBatch ? "Transfer is running from this queue." : `${queue.length} item${queue.length === 1 ? "" : "s"} ready.`}
          </p>
        </div>
        <Button type="button" variant="outline" className="h-9" onClick={onRefresh} disabled={!batch.item || batch.state === "loading"}>
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
        onRemove={onRemove}
        onMove={onMove}
      />

      <div className="grid gap-3 border-t border-stone-200 pt-3">
        {failureMessage ? <Notice tone="bad">{failureMessage}</Notice> : null}
        <div className="flex flex-wrap items-center justify-end gap-2">
          {batch.item?.status === "running" ? (
            <Button type="button" variant="outline" className="h-10" onClick={onPause} disabled={batch.state === "pausing"}>
              <Pause className="h-4 w-4" />
              Pause
            </Button>
          ) : null}
          {batch.item?.status === "paused" ? (
            <Button type="button" variant="outline" className="h-10" onClick={onResume} disabled={batch.state === "resuming"}>
              <Play className="h-4 w-4" />
              Resume
            </Button>
          ) : null}
          {activeBatch ? (
            <Button type="button" variant="danger" className="h-10" onClick={onCancel} disabled={batch.state === "canceling"}>
              Cancel
            </Button>
          ) : null}
          {batch.item?.direction === "download" && batch.item.status === "completed" ? (
            <Button type="button" className="h-10" onClick={onSaveDownload} disabled={batch.state === "downloading"}>
              <Download className="h-4 w-4" />
              {batch.state === "downloading" ? "Saving..." : "Save download"}
            </Button>
          ) : null}
          {batch.item && !activeBatch ? (
            <Button type="button" variant="outline" className="h-10" onClick={onClear}>
              Clear
            </Button>
          ) : null}
          <Button type="button" disabled={!canStart} onClick={onStart}>
            {mode === "upload" ? <Upload className="h-4 w-4" /> : <Download className="h-4 w-4" />}
            Start {mode}
          </Button>
        </div>
      </div>
    </section>
  );
}
