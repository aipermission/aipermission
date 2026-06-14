import { File, Folder, RefreshCcw } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Field, Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { formatBytes, formatShortDate } from "../../../lib/file-transfer-utils";

export function RemoteBrowserDialog({ browser, onClose, onLoad, onPathChange, onUseDirectory, onAddFiles, queuedPaths }) {
  const [selectedFiles, setSelectedFiles] = useState({});

  useEffect(() => {
    if (browser.open && browser.purpose === "download") {
      setSelectedFiles({});
    }
  }, [browser.open, browser.purpose]);

  if (!browser.open) return null;
  const entries = browser.data?.entries || [];
  const canUseCurrentDirectory = browser.purpose === "upload";
  const selectedList = Object.values(selectedFiles);
  const selectedCount = selectedList.length;

  function toggleFile(entry) {
    if (!entry || entry.type !== "file" || queuedPaths?.has(entry.path)) return;
    setSelectedFiles((current) => {
      const next = { ...current };
      if (next[entry.path]) {
        delete next[entry.path];
      } else {
        next[entry.path] = entry;
      }
      return next;
    });
  }

  function addSelectedFiles() {
    if (selectedCount === 0) return;
    onAddFiles(selectedList);
    setSelectedFiles({});
    onClose();
  }

  return (
    <Dialog
      open={browser.open}
      title={browser.purpose === "upload" ? "Choose remote folder" : "Add remote files"}
      description="Browse the selected target over SFTP."
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
          {!canUseCurrentDirectory ? (
            <Button type="button" className="h-10" onClick={addSelectedFiles} disabled={selectedCount === 0}>
              Add Selected Files ({selectedCount})
            </Button>
          ) : null}
        </div>

        {browser.error ? <Notice tone="bad">{browser.error}</Notice> : null}

        <div className="min-h-80 overflow-hidden rounded-md border border-stone-200 bg-white">
          <div className="max-h-[50vh] overflow-auto">
            <button
              type="button"
              className="grid w-full grid-cols-[24px_24px_minmax(0,1fr)_120px_170px] items-center gap-3 border-b border-stone-100 px-3 py-2 text-left text-sm transition hover:bg-stone-50"
              onClick={() => onLoad(browser.data?.parent || "/", browser.purpose)}
            >
              <span />
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
                    className="grid w-full grid-cols-[24px_24px_minmax(0,1fr)_120px_170px] items-center gap-3 border-b border-stone-100 px-3 py-2 text-left text-sm transition hover:bg-stone-50"
                  >
                    {entry.type === "file" && browser.purpose === "download" ? (
                      <input
                        type="checkbox"
                        className="h-4 w-4 rounded border-stone-300 accent-emerald-700"
                        checked={Boolean(selectedFiles[entry.path]) || queuedPaths?.has(entry.path)}
                        disabled={queuedPaths?.has(entry.path)}
                        onChange={() => toggleFile(entry)}
                      />
                    ) : (
                      <span />
                    )}
                    {entry.type === "directory" ? <Folder className="h-4 w-4 text-emerald-700" /> : <File className="h-4 w-4 text-stone-500" />}
                    <button type="button" className="min-w-0 text-left" onClick={() => (entry.type === "directory" ? onLoad(entry.path, browser.purpose) : toggleFile(entry))}>
                      <span className="block truncate font-medium">{entry.name}</span>
                    </button>
                    <span className="text-xs text-stone-500">{entry.type === "directory" ? "folder" : queuedPaths?.has(entry.path) ? "queued" : formatBytes(entry.size)}</span>
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
