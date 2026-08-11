import { CornerUpLeft, Database, Download, FileUp, Folder, Pencil, Plus, RefreshCcw, Search, Trash2, Upload, X } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { FileTransferDialog } from "../../../components/file-transfer/file-transfer-dialog";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { Field, Input, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { apiPost, saveBlob } from "../../../lib/api";

const defaultListLimit = 100;
const defaultUploadDialog = {
  open: false,
  mode: "files",
  prefix: "",
  files: [],
  textKey: "",
  textContent: "",
  textContentType: "text/plain",
  overwrite: false,
  pending: false,
  error: "",
};
const defaultRenameDialog = { open: false, value: "", pending: false, error: "" };

export function S3ConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const activeSession = session || { active: false, startedAt: "" };
  const [prefix, setPrefix] = useState("");
  const [search, setSearch] = useState("");
  const [directories, setDirectories] = useState([]);
  const [objects, setObjects] = useState([]);
  const [nextToken, setNextToken] = useState("");
  const [selectedKey, setSelectedKey] = useState("");
  const [metadata, setMetadata] = useState(null);
  const [metadataSearch, setMetadataSearch] = useState("");
  const [uploadDialog, setUploadDialog] = useState(defaultUploadDialog);
  const [transferOpen, setTransferOpen] = useState(false);
  const [renameDialog, setRenameDialog] = useState(defaultRenameDialog);
  const [confirmDialog, setConfirmDialog] = useState({
    open: false,
    title: "",
    description: "",
    details: [],
    action: null,
    pending: false,
    danger: false,
  });
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const panelClass = theme === "light" ? "bg-white text-stone-900" : "bg-[#1e1e1e] text-stone-100";
  const mutedClass = theme === "light" ? "text-stone-500" : "text-stone-400";
  const borderClass = theme === "light" ? "border-stone-200" : "border-stone-700";
  const subtlePanelClass = theme === "light" ? "bg-stone-50" : "bg-[#252526]";
  const inputClass =
    theme === "light"
      ? "border-stone-300 bg-white text-stone-900 placeholder:text-stone-400"
      : "border-stone-700 bg-[#1a1a1a] text-stone-100 placeholder:text-stone-500";
  const rowHoverClass = theme === "light" ? "hover:bg-stone-50" : "hover:bg-stone-800/60";
  const activeRowClass =
    theme === "light" ? "border-emerald-200 bg-emerald-50 text-emerald-950" : "border-emerald-700 bg-emerald-950/40 text-emerald-100";
  const activeItems = useMemo(
    () => (approvals?.data || []).filter((item) => item.target_ref === target.ref),
    [approvals?.data, target.ref],
  );
  const latestAction = activeItems[0] || null;
  const selectedObject = objects.find((item) => item.key === selectedKey) || null;
  const visibleBytes = useMemo(() => objects.reduce((total, object) => total + Number(object.size || 0), 0), [objects]);
  const refreshObjectsForEffect = useEffectEvent((options) => refreshObjects(options));

  useEffect(() => {
    setPrefix("");
    setSearch("");
    setDirectories([]);
    setObjects([]);
    setNextToken("");
    setSelectedKey("");
    setMetadata(null);
    setMetadataSearch("");
    setUploadDialog(defaultUploadDialog);
    setTransferOpen(false);
    setRenameDialog(defaultRenameDialog);
    setState({ state: "idle", error: "", message: "" });
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  useEffect(() => {
    if (!activeSession.active) return;
    void refreshObjectsForEffect({ reset: true });
  }, [activeSession.active, activeSession.startedAt, target.ref]);

  async function runS3Action({ actionName, input, reason, busy = "running", suppressError = false }) {
    setState({ state: busy, error: "", message: "" });
    try {
      const item = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: actionName,
        input,
        reason,
      });
      setState({ state: "idle", error: "", message: item.display_text || "" });
      await onRefreshActivity?.();
      return item;
    } catch (error) {
      if (suppressError) {
        setState({ state: "idle", error: "", message: "" });
      } else {
        setState({ state: "error", error: error.message || "S3 action failed.", message: "" });
      }
      throw error;
    }
  }

  async function refreshObjects({ reset = true, token = "", nextPrefix = prefix, nextSearch = search } = {}) {
    if (!activeSession.active) return;
    const item = await runS3Action({
      actionName: "list_objects",
      input: { prefix: nextPrefix, search: nextSearch, cursor: reset ? "" : token, limit: defaultListLimit },
      reason: "manual S3 browser object list",
      busy: "loading",
    });
    const nextDirectories = Array.isArray(item.output?.directories) ? item.output.directories : [];
    const nextObjects = Array.isArray(item.output?.objects) ? item.output.objects : [];
    setDirectories((current) => (reset ? nextDirectories : [...current, ...nextDirectories]));
    setObjects((current) => (reset ? nextObjects : [...current, ...nextObjects]));
    setNextToken(item.output?.next_cursor || "");
    if (reset && selectedKey && !nextObjects.some((object) => object.key === selectedKey)) {
      setSelectedKey("");
      setMetadata(null);
    }
    return nextObjects;
  }

  async function openDirectory(directoryPrefix) {
    if (!activeSession.active || !directoryPrefix) return;
    setPrefix(directoryPrefix);
    setSearch("");
    setSelectedKey("");
    setMetadata(null);
    setMetadataSearch("");
    await refreshObjects({ reset: true, nextPrefix: directoryPrefix, nextSearch: "" });
  }

  async function openParentDirectory() {
    const parent = parentPrefix(prefix);
    setPrefix(parent);
    setSearch("");
    setSelectedKey("");
    setMetadata(null);
    setMetadataSearch("");
    await refreshObjects({ reset: true, nextPrefix: parent, nextSearch: "" });
  }

  async function selectObject(key) {
    if (!activeSession.active || !key) return;
    if (selectedKey === key) {
      setSelectedKey("");
      setMetadata(null);
      setMetadataSearch("");
      return;
    }
    await readObjectMetadata(key);
  }

  async function readObjectMetadata(key) {
    setSelectedKey(key);
    setMetadataSearch("");
    const item = await runS3Action({
      actionName: "get_object_metadata",
      input: { key },
      reason: "manual S3 browser object metadata",
      busy: "reading",
      suppressError: false,
    });
    setMetadata(item.output || null);
  }

  async function downloadSelected() {
    if (!selectedKey) return;
    const filename = filenameFromKey(selectedKey);
    const pickerAvailable = typeof window !== "undefined" && typeof window.showSaveFilePicker === "function";
    let saveHandle = null;
    if (pickerAvailable) {
      try {
        saveHandle = await window.showSaveFilePicker({ suggestedName: safeDownloadName(filename) });
      } catch (error) {
        if (error?.name === "AbortError") {
          setState({ state: "idle", error: "", message: "Download canceled." });
          return;
        }
        throw error;
      }
    }
    const item = await runS3Action({
      actionName: "download_object",
      input: { key: selectedKey },
      reason: "manual S3 browser object download",
      busy: "downloading",
    });
    const output = item.output || {};
    const blob = base64Blob(output.content_base64 || "", output.content_type || "application/octet-stream");
    if (saveHandle) {
      const writable = await saveHandle.createWritable();
      await writable.write(blob);
      await writable.close();
      setState({ state: "idle", error: "", message: `Saved ${output.filename || filename}.` });
      return;
    }
    await saveBlob(blob, output.filename || filename, { picker: false });
  }

  function openUploadDialog() {
    setUploadDialog({
      ...defaultUploadDialog,
      open: true,
      prefix: prefix || "",
      textKey: prefix || "",
    });
  }

  function closeUploadDialog() {
    setUploadDialog((current) => (current.pending ? current : defaultUploadDialog));
  }

  function addUploadFiles(fileList) {
    const files = Array.from(fileList || []);
    if (files.length === 0) return;
    setUploadDialog((current) => ({
      ...current,
      error: "",
      files: [
        ...current.files,
        ...files.map((file) => ({
          id: `${file.name}-${file.size}-${file.lastModified}-${Math.random().toString(36).slice(2)}`,
          file,
          key: joinObjectKey(current.prefix, file.name),
          contentType: file.type || "application/octet-stream",
        })),
      ],
    }));
  }

  function removeUploadFile(id) {
    setUploadDialog((current) => ({ ...current, files: current.files.filter((item) => item.id !== id) }));
  }

  function updateUploadFile(id, patch) {
    setUploadDialog((current) => ({
      ...current,
      files: current.files.map((item) => (item.id === id ? { ...item, ...patch } : item)),
    }));
  }

  async function uploadObjects(event) {
    event.preventDefault();
    if (!activeSession.active || uploadDialog.pending) return;
    const preparedFiles = uploadDialog.files.map((item) => ({ ...item, key: normalizeObjectKey(item.key) })).filter((item) => item.key);
    const textKey = normalizeObjectKey(uploadDialog.textKey);
    const fileMode = uploadDialog.mode !== "text";
    const includeText = uploadDialog.mode === "text" && textKey && uploadDialog.textContent;
    if (fileMode && preparedFiles.length === 0) {
      setUploadDialog((current) => ({ ...current, error: "Choose one or more files to upload." }));
      return;
    }
    if (!fileMode && !includeText) {
      setUploadDialog((current) => ({ ...current, error: "Enter an object key and text content." }));
      return;
    }
    setUploadDialog((current) => ({ ...current, pending: true, error: "", message: "" }));
    let lastKey = "";
    try {
      if (fileMode) {
        for (const item of preparedFiles) {
          await runS3Action({
            actionName: "upload_object",
            input: {
              key: item.key,
              content_base64: await fileToBase64(item.file),
              content_type: item.contentType || item.file.type || "application/octet-stream",
              overwrite: uploadDialog.overwrite,
            },
            reason: "manual S3 browser object upload",
            busy: "uploading",
          });
          lastKey = item.key;
        }
      }
      if (includeText) {
        await runS3Action({
          actionName: "upload_object",
          input: {
            key: textKey,
            content_text: uploadDialog.textContent,
            content_type: uploadDialog.textContentType || "text/plain",
            overwrite: uploadDialog.overwrite,
          },
          reason: "manual S3 browser object upload",
          busy: "uploading",
        });
        lastKey = textKey;
      }
      setUploadDialog(defaultUploadDialog);
      await refreshObjects({ reset: true });
      if (lastKey) {
        await readObjectMetadata(lastKey);
      }
      setState({ state: "idle", error: "", message: `Uploaded ${fileMode ? preparedFiles.length : 1} object(s).` });
    } catch (error) {
      setUploadDialog((current) => ({ ...current, pending: false, error: error.message || "Upload failed." }));
    }
  }

  function openRenameDialog() {
    if (!selectedKey) return;
    setRenameDialog({ open: true, value: selectedKey, pending: false, error: "" });
  }

  function closeRenameDialog() {
    setRenameDialog((current) => (current.pending ? current : defaultRenameDialog));
  }

  async function renameSelectedObject(event) {
    event.preventDefault();
    if (!selectedKey || renameDialog.pending) return;
    const nextKey = normalizeObjectKey(renameDialog.value);
    if (!nextKey || nextKey === selectedKey) {
      setRenameDialog((current) => ({ ...current, error: "Enter a different destination object key." }));
      return;
    }
    setRenameDialog((current) => ({ ...current, pending: true, error: "" }));
    try {
      await runS3Action({
        actionName: "rename_object",
        input: { source_key: selectedKey, destination_key: nextKey, overwrite: false },
        reason: "manual S3 browser object rename",
        busy: "renaming",
      });
      setRenameDialog(defaultRenameDialog);
      setSelectedKey("");
      setMetadata(null);
      await refreshObjects({ reset: true });
      await readObjectMetadata(nextKey);
    } catch (error) {
      setRenameDialog((current) => ({ ...current, pending: false, error: error.message || "Rename failed." }));
    }
  }

  function requestDelete() {
    if (!selectedKey) return;
    openConfirmDialog({
      title: "Delete S3 object",
      description: "This permanently deletes the selected object from the bucket.",
      details: [{ label: "Object", value: selectedKey }],
      danger: true,
      action: async () => {
        await runS3Action({
          actionName: "delete_object",
          input: { key: selectedKey },
          reason: "manual S3 browser object delete",
          busy: "deleting",
        });
        setSelectedKey("");
        setMetadata(null);
        setMetadataSearch("");
        await refreshObjects({ reset: true });
      },
    });
  }

  async function readBucketInfo() {
    setMetadataSearch("");
    const item = await runS3Action({
      actionName: "bucket_info",
      input: {},
      reason: "manual S3 browser bucket info",
      busy: "reading",
    });
    setMetadata(item.output || null);
  }

  function openConfirmDialog({ title, description, details, action, danger = false }) {
    setConfirmDialog({ open: true, title, description, details, action, pending: false, danger });
  }

  async function confirmPendingAction() {
    if (!confirmDialog.action) return;
    setConfirmDialog((current) => ({ ...current, pending: true }));
    try {
      await confirmDialog.action();
      setConfirmDialog({ open: false, title: "", description: "", details: [], action: null, pending: false, danger: false });
    } catch {
      setConfirmDialog((current) => ({ ...current, pending: false }));
    }
  }

  if (!activeSession.active) {
    return (
      <div className={`grid min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
        <div className="grid place-items-center p-8 text-center">
          <div className="grid max-w-lg gap-4">
            <Database className={`mx-auto h-10 w-10 ${mutedClass}`} />
            <div>
              <h3 className="text-lg font-semibold">No active S3 session</h3>
              <p className={`mt-2 text-sm ${mutedClass}`}>
                Start a structured session to browse objects through the connector approval, history, and audit pipeline.
              </p>
            </div>
            <Button type="button" className="mx-auto" onClick={onNewStructuredSession}>
              Start S3 session
            </Button>
          </div>
        </div>
        <S3EndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      </div>
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
      <div className="grid min-h-0 gap-4 overflow-hidden p-4 xl:grid-cols-[380px_minmax(0,1fr)]">
        <section
          className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}
        >
          <div className={`border-b p-3 ${borderClass}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <p className="text-sm font-semibold">Objects</p>
                <p className={`text-xs ${mutedClass}`}>
                  {directories.length + objects.length} loaded · {target.config?.bucket || "bucket"}
                </p>
              </div>
              <div className="flex items-center gap-2">
                {latestAction ? (
                  <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>
                    {latestAction.action_name}
                  </Badge>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 w-8 px-0"
                  title="Bucket info"
                  onClick={readBucketInfo}
                  disabled={state.state !== "idle"}
                >
                  <Database className="h-3.5 w-3.5" />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 w-8 px-0"
                  title="Transfer files and folders"
                  onClick={() => setTransferOpen(true)}
                  disabled={state.state !== "idle" || !target.transfer_runtime_id}
                >
                  <Upload className="h-3.5 w-3.5" />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 w-8 px-0"
                  title="Create a small object"
                  onClick={openUploadDialog}
                  disabled={state.state !== "idle"}
                >
                  <Plus className="h-3.5 w-3.5" />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 w-8 px-0"
                  title="Refresh objects"
                  onClick={() => refreshObjects({ reset: true })}
                  disabled={state.state !== "idle"}
                >
                  <RefreshCcw className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          </div>
          <form
            className={`grid gap-2 border-b p-3 ${borderClass}`}
            onSubmit={(event) => {
              event.preventDefault();
              void refreshObjects({ reset: true });
            }}
          >
            <Input
              className={inputClass}
              value={prefix}
              onChange={(event) => setPrefix(event.target.value)}
              placeholder="Prefix, e.g. backups/2026/"
            />
            <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
              <div className="relative">
                <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${mutedClass}`} />
                <Input
                  className={`pl-9 ${inputClass}`}
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder="Search object keys"
                />
              </div>
              <Button type="submit" variant="outline" className="h-10" disabled={state.state !== "idle"}>
                {state.state === "loading" ? "Loading" : "Search"}
              </Button>
            </div>
          </form>
          <div className="min-h-0 overflow-auto p-2">
            {prefix && !search ? (
              <button
                type="button"
                className={`mb-1 flex w-full items-center gap-3 rounded-md border px-3 py-2 text-left text-sm transition ${borderClass} ${rowHoverClass}`}
                onClick={openParentDirectory}
              >
                <CornerUpLeft className={`h-4 w-4 shrink-0 ${mutedClass}`} />
                <span className="min-w-0">
                  <span className="block truncate font-semibold">..</span>
                  <span className={`block truncate text-xs ${mutedClass}`}>{parentPrefix(prefix) || "bucket root"}</span>
                </span>
              </button>
            ) : null}
            {!search
              ? directories.map((directory) => (
                  <button
                    key={directory.prefix}
                    type="button"
                    className={`mb-1 flex w-full items-center gap-3 rounded-md border px-3 py-2 text-left text-sm transition ${borderClass} ${rowHoverClass}`}
                    onClick={() => openDirectory(directory.prefix)}
                  >
                    <Folder className="h-4 w-4 shrink-0 text-amber-400" />
                    <span className="min-w-0">
                      <span className="block truncate font-mono text-xs font-semibold" title={directory.prefix}>
                        {directory.name || directory.prefix}
                      </span>
                      <span className={`block truncate text-xs ${mutedClass}`}>{directory.prefix}</span>
                    </span>
                  </button>
                ))
              : null}
            {objects.map((object) => (
              <button
                key={object.key}
                type="button"
                className={`mb-1 grid w-full gap-1 rounded-md border px-3 py-2 text-left text-sm transition ${selectedKey === object.key ? activeRowClass : `${borderClass} ${rowHoverClass}`}`}
                onClick={() => selectObject(object.key)}
              >
                <span className="truncate font-mono text-xs font-semibold" title={object.key}>
                  {object.key}
                </span>
                <span className={`text-xs ${selectedKey === object.key ? "" : mutedClass}`}>
                  {formatBytes(object.size)} · {shortDate(object.last_modified)}
                </span>
              </button>
            ))}
            {directories.length === 0 && objects.length === 0 ? (
              <Notice>{state.state === "loading" ? "Loading S3 objects..." : "No objects found for this prefix/search."}</Notice>
            ) : null}
          </div>
          <div className={`flex items-center justify-between gap-2 border-t p-3 ${borderClass}`}>
            <span className={`text-xs ${mutedClass}`}>{nextToken ? "More objects available" : "End of current listing"}</span>
            <Button
              type="button"
              variant="outline"
              className="h-8"
              disabled={!nextToken || state.state !== "idle"}
              onClick={() => refreshObjects({ reset: false, token: nextToken })}
            >
              Load more
            </Button>
          </div>
        </section>

        <section
          className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}
        >
          <div>
            <div className={`border-b p-3 ${borderClass}`}>
              <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold">{selectedKey || "S3 object detail"}</p>
                  <p className={`truncate text-xs ${mutedClass}`}>
                    {selectedObject
                      ? `${formatBytes(selectedObject.size)} · ${shortDate(selectedObject.last_modified)}`
                      : "Select an object or upload a new one."}
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    className="h-8 w-8 px-0"
                    title="Download object"
                    disabled={!selectedKey || state.state !== "idle"}
                    onClick={downloadSelected}
                  >
                    <Download className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    className="h-8 w-8 px-0"
                    title="Rename object"
                    disabled={!selectedKey || state.state !== "idle"}
                    onClick={openRenameDialog}
                  >
                    <Pencil className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    type="button"
                    variant="danger"
                    className="h-8 w-8 px-0"
                    title="Delete object"
                    disabled={!selectedKey || state.state !== "idle"}
                    onClick={requestDelete}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </div>
            </div>
            {state.error ? (
              <div className={`border-b px-3 py-2 text-right text-xs text-red-500 ${borderClass}`}>
                <span className="break-words">{state.error}</span>
              </div>
            ) : null}
          </div>
          <div className="grid h-full min-h-0 grid-rows-[minmax(0,1fr)] overflow-hidden p-3">
            <S3MetadataPanel
              metadata={metadata}
              selectedKey={selectedKey}
              directories={directories}
              objects={objects}
              visibleBytes={visibleBytes}
              prefix={prefix}
              search={search}
              metadataSearch={metadataSearch}
              onMetadataSearch={setMetadataSearch}
              inputClass={inputClass}
            />
          </div>
        </section>
      </div>
      <S3EndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      <FileTransferDialog
        open={transferOpen}
        runtimeTarget={
          target.transfer_runtime_id
            ? {
                id: target.transfer_runtime_id,
                name: target.target_name || target.name || "S3 target",
                subtitle: `${target.config?.scheme || "https"}://${target.config?.host || "s3.amazonaws.com"}:${target.config?.port || 443}/${target.config?.bucket || "bucket"}`,
              }
            : null
        }
        options={{
          transportLabel: "S3 object storage",
          defaultDirectory: "/",
          recursive: true,
          notice:
            "S3 transfers use bounded queues with multipart uploads, progress, pause, cancel, and short-lived local staging. A paused transfer resumes only while this gateway process remains running.",
        }}
        onClose={() => {
          setTransferOpen(false);
          void refreshObjects({ reset: true });
        }}
      />
      <S3UploadDialog
        value={uploadDialog}
        theme={theme}
        inputClass={inputClass}
        borderClass={borderClass}
        mutedClass={mutedClass}
        subtlePanelClass={subtlePanelClass}
        onClose={closeUploadDialog}
        onChange={setUploadDialog}
        onFiles={addUploadFiles}
        onRemoveFile={removeUploadFile}
        onUpdateFile={updateUploadFile}
        onSubmit={uploadObjects}
      />
      <S3RenameDialog
        value={renameDialog}
        theme={theme}
        selectedKey={selectedKey}
        inputClass={inputClass}
        onClose={closeRenameDialog}
        onChange={setRenameDialog}
        onSubmit={renameSelectedObject}
      />
      <S3ConfirmDialog
        value={confirmDialog}
        theme={theme}
        onClose={() =>
          setConfirmDialog({ open: false, title: "", description: "", details: [], action: null, pending: false, danger: false })
        }
        onConfirm={confirmPendingAction}
      />
    </div>
  );
}

function S3EndpointFooter({ target, borderClass, mutedClass }) {
  const scheme = target.config?.scheme || "https";
  const host = target.config?.host || "s3.amazonaws.com";
  const port = target.config?.port || (scheme === "http" ? 80 : 443);
  const bucket = target.config?.bucket || "bucket";
  const mode =
    target.config?.connection_mode === "over_ssh" ? `over ssh · ${target.config?.transport_target_ref || "transport"}` : "direct";
  return (
    <div className={`flex items-center justify-between gap-3 border-t px-4 py-2 text-xs ${borderClass} ${mutedClass}`}>
      <span>S3 transport</span>
      <span className="truncate">
        {scheme}://{host}:{port}/{bucket} · {mode}
      </span>
    </div>
  );
}

function S3MetadataPanel({
  metadata,
  selectedKey,
  directories,
  objects,
  visibleBytes,
  prefix,
  search,
  metadataSearch,
  onMetadataSearch,
  inputClass,
}) {
  if (!metadata) {
    return (
      <TerminalBlock className="min-h-0 text-xs" surface="log">
        {selectedKey ? "Reading object metadata..." : "Select an object to inspect metadata, or use Bucket info for endpoint details."}
      </TerminalBlock>
    );
  }
  const isBucketInfo = !metadata.key && Boolean(metadata.bucket);
  const cards = isBucketInfo
    ? [
        { label: "Bucket", value: metadata.bucket || "unknown" },
        { label: "Endpoint", value: metadata.endpoint || "unknown" },
        { label: "Region", value: metadata.region || "unknown" },
        { label: "Visible folders", value: String(directories.length) },
        { label: "Visible objects", value: String(objects.length) },
        { label: "Visible size", value: formatBytes(visibleBytes) },
        { label: "Current prefix", value: prefix || "bucket root" },
        { label: "Search", value: search || "none" },
        { label: "Request id", value: metadata.headers?.["X-Amz-Request-Id"] || metadata.headers?.["X-Amz-Id-2"] || "not provided" },
      ]
    : [
        { label: "Object", value: metadata.key || selectedKey || "unknown" },
        { label: "Bucket", value: metadata.bucket || "unknown" },
        { label: "Size", value: formatBytes(metadata.content_length) },
        { label: "Content type", value: metadata.content_type || "unknown" },
        { label: "Last modified", value: metadata.last_modified || "unknown" },
        { label: "ETag", value: metadata.etag || "unknown" },
      ];
  const rawValue = JSON.stringify(metadata, null, 2);
  return (
    <div className="grid min-h-0 grid-rows-[auto_minmax(0,450px)_auto_minmax(0,1fr)] overflow-hidden">
      <S3ResultHeader
        title={isBucketInfo ? "Bucket summary" : "Object summary"}
        subtitle={isBucketInfo ? "visible listing stats" : "metadata"}
      />
      <S3MetadataSummary cards={cards} />
      <div className="mt-3 flex items-center justify-between gap-3">
        <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">S3 raw data</p>
        <div className="flex min-w-0 items-center justify-end gap-2">
          <Input
            className={`h-8 w-56 text-xs ${inputClass || ""}`}
            value={metadataSearch}
            onChange={(event) => onMetadataSearch?.(event.target.value)}
            placeholder="Search raw data"
          />
          <CopyButton value={rawValue} variant="outline" className="h-8 px-2 text-xs" />
        </div>
      </div>
      <div className="mt-2 grid min-h-0 overflow-hidden">
        <TerminalBlock className="min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]" surface="dark">
          <HighlightedText text={rawValue} query={metadataSearch} />
        </TerminalBlock>
      </div>
    </div>
  );
}

function S3ResultHeader({ title, subtitle }) {
  return (
    <div className="mb-2 flex items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">{title}</p>
        {subtitle ? <p className="truncate text-xs text-stone-500">{subtitle}</p> : null}
      </div>
    </div>
  );
}

function S3MetadataSummary({ cards }) {
  return (
    <div className="min-h-0 overflow-auto rounded-md border border-stone-700 bg-[#1a1a1a] p-3">
      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {cards.map((card) => (
          <div key={card.label} className="min-w-0 rounded border border-stone-700 bg-[#202020] p-2">
            <p className="text-[10px] font-semibold uppercase tracking-wide text-stone-500">{card.label}</p>
            <p className="mt-1 whitespace-pre-wrap break-words font-mono text-xs text-stone-100">{String(card.value)}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

function HighlightedText({ text, query }) {
  const value = String(text || "");
  const needle = String(query || "");
  if (!needle.trim()) return value;
  const lowerValue = value.toLowerCase();
  const lowerNeedle = needle.toLowerCase();
  const parts = [];
  let index = 0;
  let matchIndex = lowerValue.indexOf(lowerNeedle, index);
  let key = 0;
  while (matchIndex !== -1) {
    if (matchIndex > index) parts.push(value.slice(index, matchIndex));
    parts.push(
      <mark key={`m-${key++}`} className="rounded bg-yellow-300 px-0.5 text-stone-950">
        {value.slice(matchIndex, matchIndex + needle.length)}
      </mark>,
    );
    index = matchIndex + needle.length;
    matchIndex = lowerValue.indexOf(lowerNeedle, index);
  }
  if (index < value.length) parts.push(value.slice(index));
  return parts;
}

function S3UploadDialog({
  value,
  theme,
  inputClass,
  borderClass,
  mutedClass,
  subtlePanelClass,
  onClose,
  onChange,
  onFiles,
  onRemoveFile,
  onUpdateFile,
  onSubmit,
}) {
  if (!value.open) return null;
  const darkTextClass = theme === "light" ? "" : "text-stone-200";
  const fileMode = value.mode !== "text";
  const canUpload = fileMode
    ? value.files.some((item) => normalizeObjectKey(item.key))
    : Boolean(normalizeObjectKey(value.textKey) && value.textContent);
  const uploadCount = fileMode ? value.files.filter((item) => normalizeObjectKey(item.key)).length : canUpload ? 1 : 0;
  return (
    <Dialog
      open={value.open}
      onClose={onClose}
      title="Upload S3 objects"
      description="Upload local files or create a small text object."
      size="wide"
      closeDisabled={value.pending}
      closeOnOverlay={false}
      closeOnEscape={false}
    >
      <form
        className="grid max-h-[calc(100vh-150px)] min-h-0 gap-4 overflow-hidden lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]"
        onSubmit={onSubmit}
      >
        <section className="grid min-h-0 content-start gap-4">
          <Notice tone="warn">
            Uploads run as bounded S3 connector write actions. Keep write permissions in Prompt mode until the workflow is trusted.
          </Notice>
          <div className={`grid grid-cols-2 rounded-lg border p-1 ${borderClass} ${subtlePanelClass}`}>
            <button
              type="button"
              className={`rounded-md px-3 py-2 text-sm font-semibold ${fileMode ? "bg-emerald-600 text-white" : mutedClass}`}
              onClick={() => onChange((current) => ({ ...current, mode: "files", error: "" }))}
              disabled={value.pending}
            >
              File upload
            </button>
            <button
              type="button"
              className={`rounded-md px-3 py-2 text-sm font-semibold ${!fileMode ? "bg-emerald-600 text-white" : mutedClass}`}
              onClick={() => onChange((current) => ({ ...current, mode: "text", error: "" }))}
              disabled={value.pending}
            >
              Create text object
            </button>
          </div>
          <div className={`grid gap-3 rounded-lg border p-3 ${borderClass} ${subtlePanelClass}`}>
            {fileMode ? (
              <>
                <Field className={darkTextClass}>
                  Object prefix
                  <Input
                    className={inputClass}
                    value={value.prefix}
                    onChange={(event) => {
                      const nextPrefix = event.target.value;
                      onChange((current) => ({
                        ...current,
                        prefix: nextPrefix,
                        files: current.files.map((item) => ({ ...item, key: joinObjectKey(nextPrefix, item.file.name) })),
                      }));
                    }}
                    placeholder="folder/subfolder/"
                  />
                </Field>
                <Field className={darkTextClass}>
                  Add files
                  <Input className={inputClass} type="file" multiple onChange={(event) => onFiles(event.target.files)} />
                </Field>
              </>
            ) : (
              <>
                <Field className={darkTextClass}>
                  Object key
                  <Input
                    className={inputClass}
                    value={value.textKey}
                    onChange={(event) => onChange((current) => ({ ...current, textKey: event.target.value }))}
                    placeholder="notes/readme.txt"
                  />
                </Field>
                <Field className={darkTextClass}>
                  Content type
                  <Input
                    className={inputClass}
                    value={value.textContentType}
                    onChange={(event) => onChange((current) => ({ ...current, textContentType: event.target.value }))}
                    placeholder="text/plain"
                  />
                </Field>
                <Field className={darkTextClass}>
                  Content
                  <Textarea
                    className={`${inputClass} min-h-36 font-mono text-xs`}
                    value={value.textContent}
                    onChange={(event) => onChange((current) => ({ ...current, textContent: event.target.value }))}
                    placeholder="Text content"
                  />
                </Field>
              </>
            )}
            <label className={`flex items-center gap-2 rounded-md border px-3 py-2 text-sm ${borderClass}`}>
              <input
                type="checkbox"
                checked={value.overwrite}
                onChange={(event) => onChange((current) => ({ ...current, overwrite: event.target.checked }))}
              />
              overwrite existing objects
            </label>
          </div>
        </section>
        <section className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-3">
          <div>
            <p className="text-sm font-semibold">{fileMode ? "Upload queue" : "Text object preview"}</p>
            <p className={`text-xs ${mutedClass}`}>
              {fileMode
                ? `${value.files.length} file${value.files.length === 1 ? "" : "s"} selected`
                : normalizeObjectKey(value.textKey) || "No object key yet"}
            </p>
          </div>
          <div className={`min-h-0 overflow-auto rounded-lg border ${borderClass}`}>
            {!fileMode ? (
              <div className="grid gap-3 p-3">
                <p className="break-all font-mono text-xs">{normalizeObjectKey(value.textKey) || "notes/readme.txt"}</p>
                <TerminalBlock className="min-h-48 text-xs" surface="log">
                  {value.textContent || "Text content preview will appear here."}
                </TerminalBlock>
              </div>
            ) : value.files.length === 0 ? (
              <div className="grid h-full min-h-48 place-items-center p-6 text-center">
                <p className={`text-sm ${mutedClass}`}>Selected files will appear here with editable object keys.</p>
              </div>
            ) : (
              <div className="grid divide-y divide-stone-200 dark:divide-stone-700">
                {value.files.map((item) => (
                  <div className="grid gap-2 p-3" key={item.id}>
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-semibold" title={item.file.name}>
                          {item.file.name}
                        </p>
                        <p className={`text-xs ${mutedClass}`}>
                          {formatBytes(item.file.size)} · {item.contentType || "application/octet-stream"}
                        </p>
                      </div>
                      <Button
                        type="button"
                        variant="ghost"
                        className="h-8 w-8 px-0"
                        title="Remove file"
                        onClick={() => onRemoveFile(item.id)}
                        disabled={value.pending}
                      >
                        <X className="h-4 w-4" />
                      </Button>
                    </div>
                    <Input
                      className={inputClass}
                      value={item.key}
                      onChange={(event) => onUpdateFile(item.id, { key: event.target.value })}
                      disabled={value.pending}
                    />
                  </div>
                ))}
              </div>
            )}
          </div>
          {value.error ? <Notice tone="bad">{value.error}</Notice> : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={value.pending}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canUpload || value.pending}>
              <FileUp className="h-4 w-4" />
              {value.pending ? "Uploading..." : `Upload ${uploadCount} object(s)`}
            </Button>
          </div>
        </section>
      </form>
    </Dialog>
  );
}

function S3RenameDialog({ value, theme, selectedKey, inputClass, onClose, onChange, onSubmit }) {
  if (!value.open) return null;
  return (
    <Dialog
      open={value.open}
      onClose={onClose}
      title="Rename S3 object"
      description="Rename copies the object to a new key and deletes the original key."
      size="md"
      closeDisabled={value.pending}
    >
      <form className="grid gap-4" onSubmit={onSubmit}>
        <div
          className={`grid gap-2 rounded-md border p-3 text-sm ${theme === "light" ? "border-stone-200 bg-stone-50 text-stone-700" : "border-stone-700 bg-stone-900 text-stone-200"}`}
        >
          <span className="text-xs font-semibold uppercase tracking-wide opacity-70">Source</span>
          <span className="break-all font-mono text-xs">{selectedKey}</span>
        </div>
        <Field className={theme === "light" ? "" : "text-stone-200"}>
          Destination key
          <Input
            className={inputClass}
            value={value.value}
            onChange={(event) => onChange((current) => ({ ...current, value: event.target.value, error: "" }))}
            autoFocus
          />
        </Field>
        {value.error ? <Notice tone="bad">{value.error}</Notice> : <Notice tone="warn">Review the destination key before renaming.</Notice>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={value.pending}>
            Cancel
          </Button>
          <Button type="submit" disabled={value.pending}>
            {value.pending ? "Renaming..." : "Rename"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function S3ConfirmDialog({ value, theme, onClose, onConfirm }) {
  if (!value.open) return null;
  const detailClass = theme === "light" ? "border-stone-200 bg-stone-50 text-stone-700" : "border-stone-700 bg-stone-900 text-stone-200";
  return (
    <Dialog open={value.open} onClose={onClose} title={value.title} description={value.description} size="md" closeDisabled={value.pending}>
      <div className="grid gap-4">
        <div className={`grid gap-2 rounded-md border p-3 text-sm ${detailClass}`}>
          {value.details.map((detail) => (
            <div className="grid gap-1" key={detail.label}>
              <span className="text-xs font-semibold uppercase tracking-wide opacity-70">{detail.label}</span>
              <span className="break-all font-mono text-xs">{detail.value}</span>
            </div>
          ))}
        </div>
        {value.danger ? (
          <Notice tone="bad">This action changes object storage state and cannot be undone by AIPermission.</Notice>
        ) : (
          <Notice tone="warn">Review the object keys before continuing.</Notice>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={value.pending}>
            Cancel
          </Button>
          <Button type="button" variant={value.danger ? "danger" : "default"} onClick={onConfirm} disabled={value.pending}>
            {value.pending ? "Working..." : value.danger ? "Delete" : "Confirm"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

function fileToBase64(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result || "");
      resolve(result.includes(",") ? result.split(",").pop() : result);
    };
    reader.onerror = () => reject(reader.error || new Error("Failed to read file."));
    reader.readAsDataURL(file);
  });
}

function base64Blob(value, contentType) {
  const binary = atob(value || "");
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return new Blob([bytes], { type: contentType || "application/octet-stream" });
}

function filenameFromKey(key) {
  const parts = String(key || "s3-object")
    .split("/")
    .filter(Boolean);
  return parts[parts.length - 1] || "s3-object";
}

function safeDownloadName(value) {
  return String(value || "s3-object").replaceAll(":", "-");
}

function normalizeObjectKey(value) {
  return String(value || "")
    .trim()
    .replace(/^\/+/, "");
}

function joinObjectKey(prefix, name) {
  const cleanPrefix = normalizeObjectKey(prefix);
  const cleanName = normalizeObjectKey(name);
  if (!cleanPrefix) return cleanName;
  return `${cleanPrefix.replace(/\/+$/, "")}/${cleanName}`;
}

function parentPrefix(value) {
  const clean = normalizeObjectKey(value).replace(/\/+$/, "");
  const index = clean.lastIndexOf("/");
  if (index <= 0) return "";
  return `${clean.slice(0, index)}/`;
}

function formatBytes(value) {
  const number = Number(value || 0);
  if (number < 1024) return `${number} B`;
  if (number < 1024 * 1024) return `${(number / 1024).toFixed(1)} KiB`;
  return `${(number / 1024 / 1024).toFixed(1)} MiB`;
}

function shortDate(value) {
  if (!value) return "unknown";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}
