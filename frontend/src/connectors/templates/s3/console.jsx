import { CornerUpLeft, Database, Download, Folder, Link2, Pencil, Plus, RefreshCcw, Search, Trash2, Upload } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { FileTransferDialog } from "../../../components/file-transfer/file-transfer-dialog";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { apiPost, saveBlob } from "../../../lib/api";
import { formatBytes } from "../../../lib/file-transfer-utils";
import { S3PresignDialog } from "./presign-dialog";
import { S3VersionsDialog, VersionsIcon } from "./versions-dialog";
import { LifecycleIcon, S3LifecycleDialog } from "./lifecycle-dialog";
import { S3ConfirmDialog, S3RenameDialog, S3UploadDialog } from "./dialogs";
import {
  base64Blob,
  fileToBase64,
  filenameFromKey,
  joinObjectKey,
  normalizeObjectKey,
  parentPrefix,
  safeDownloadName,
  shortDate,
} from "./helpers";
import { S3MetadataPanel } from "./metadata-panel";
import { requireCompletedConnectorAction } from "../_shared/action-result";
import { createRequestGuard } from "../_shared/request-guard";

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
  const [presignOpen, setPresignOpen] = useState(false);
  const [versionsOpen, setVersionsOpen] = useState(false);
  const [lifecycleOpen, setLifecycleOpen] = useState(false);
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
  const requestGuard = useRef(createRequestGuard()).current;
  requestGuard.setScope(`${target.ref}:${activeSession.startedAt || "inactive"}`);
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
    setPresignOpen(false);
    setVersionsOpen(false);
    setLifecycleOpen(false);
    setRenameDialog(defaultRenameDialog);
    setState({ state: "idle", error: "", message: "" });
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  useEffect(() => {
    if (!activeSession.active) return;
    void refreshObjectsForEffect({ reset: true });
  }, [activeSession.active, activeSession.startedAt, target.ref]);

  useEffect(() => () => requestGuard.dispose(), [requestGuard]);

  useEffect(() => {
    if (selectedKey) return;
    requestGuard.invalidate("metadata");
    setMetadata(null);
    setMetadataSearch("");
  }, [requestGuard, selectedKey]);

  async function runS3Action({ actionName, input, reason, busy = "running", suppressError = false, channel = actionName }) {
    const request = requestGuard.begin(channel);
    setState({ state: busy, error: "", message: "" });
    try {
      const response = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: actionName,
        input,
        reason,
      });
      if (!request.isCurrent()) return null;
      const item = requireCompletedConnectorAction(response, "S3 action failed.");
      if (!item) {
        setState({ state: "idle", error: "", message: response.display_text || "S3 action is awaiting approval." });
        void onRefreshActivity?.();
        return null;
      }
      setState({ state: "idle", error: "", message: item.display_text || "" });
      try {
        await onRefreshActivity?.();
      } catch (refreshError) {
        if (request.isCurrent()) {
          setState({
            state: "idle",
            error: `Action completed, but activity refresh failed: ${refreshError.message || "unknown error"}`,
            message: item.display_text || "",
          });
        }
      }
      if (!request.isCurrent()) return null;
      return item;
    } catch (error) {
      if (!request.isCurrent()) return null;
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
      channel: "objects",
    });
    if (!item) return [];
    const nextDirectories = Array.isArray(item.output?.directories) ? item.output.directories : [];
    const nextObjects = Array.isArray(item.output?.objects) ? item.output.objects : [];
    setDirectories((current) => (reset ? nextDirectories : [...current, ...nextDirectories]));
    setObjects((current) => (reset ? nextObjects : [...current, ...nextObjects]));
    setNextToken(item.output?.next_cursor || "");
    if (reset) {
      setSelectedKey((current) => (current && !nextObjects.some((object) => object.key === current) ? "" : current));
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
      channel: "metadata",
    });
    if (!item) return;
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
    if (!item) return;
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
          const uploaded = await runS3Action({
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
          if (!uploaded) return;
          lastKey = item.key;
        }
      }
      if (includeText) {
        const uploaded = await runS3Action({
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
        if (!uploaded) return;
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
      const renamed = await runS3Action({
        actionName: "rename_object",
        input: { source_key: selectedKey, destination_key: nextKey, overwrite: false },
        reason: "manual S3 browser object rename",
        busy: "renaming",
      });
      if (!renamed) return;
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
        const deleted = await runS3Action({
          actionName: "delete_object",
          input: { key: selectedKey },
          reason: "manual S3 browser object delete",
          busy: "deleting",
        });
        if (!deleted) return;
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
      channel: "metadata",
    });
    if (!item) return;
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
                    title="Bucket lifecycle"
                    disabled={!activeSession.active || state.state !== "idle"}
                    onClick={() => setLifecycleOpen(true)}
                  >
                    <LifecycleIcon />
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    className="h-8 w-8 px-0"
                    title="Create temporary S3 URL"
                    disabled={!activeSession.active || state.state !== "idle"}
                    onClick={() => setPresignOpen(true)}
                  >
                    <Link2 className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    className="h-8 w-8 px-0"
                    title="Object versions"
                    disabled={!selectedKey || state.state !== "idle"}
                    onClick={() => setVersionsOpen(true)}
                  >
                    <VersionsIcon />
                  </Button>
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
          onUploadCompleted: () => refreshObjects({ reset: true }),
        }}
        onClose={() => {
          setTransferOpen(false);
        }}
      />
      <S3PresignDialog
        open={presignOpen}
        selectedKey={selectedKey}
        theme={theme}
        inputClass={inputClass}
        borderClass={borderClass}
        mutedClass={mutedClass}
        onClose={() => setPresignOpen(false)}
        onRun={runS3Action}
      />
      <S3VersionsDialog
        open={versionsOpen}
        objectKey={selectedKey}
        theme={theme}
        borderClass={borderClass}
        mutedClass={mutedClass}
        onClose={() => setVersionsOpen(false)}
        onRun={runS3Action}
        onChanged={async () => {
          await refreshObjects({ reset: true });
          if (selectedKey) await readObjectMetadata(selectedKey);
        }}
      />
      <S3LifecycleDialog
        open={lifecycleOpen}
        bucket={target.config?.bucket || "bucket"}
        theme={theme}
        inputClass={inputClass}
        borderClass={borderClass}
        mutedClass={mutedClass}
        onClose={() => setLifecycleOpen(false)}
        onRun={runS3Action}
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
