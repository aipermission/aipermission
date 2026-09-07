import { Database } from "lucide-react";
import { useEffect, useEffectEvent, useState } from "react";
import { FileTransferDialog } from "../../../components/file-transfer/file-transfer-dialog";
import { saveBlob } from "../../../lib/api";
import { S3PresignDialog } from "./presign-dialog";
import { joinTransferPath, normalizeTransferDirectory } from "./transfer-paths";
import { S3VersionsDialog } from "./versions-dialog";
import { S3LifecycleDialog } from "./lifecycle-dialog";
import { defaultS3ConfirmDialog, defaultUploadDialog, S3ConfirmDialog, S3UploadDialog } from "./dialogs";
import {
  base64Blob,
  approvalsForTarget,
  fileToBase64,
  filenameFromKey,
  joinObjectKey,
  normalizeObjectKey,
  parentPrefix,
  safeDownloadName,
  visibleObjectBytes,
} from "./helpers";
import { S3EndpointFooter } from "./endpoint-footer";
import { S3ObjectBrowser } from "./object-browser";
import { S3ObjectDetailPane } from "./object-detail-pane";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { StructuredSessionEmpty } from "../_shared/structured-session-empty";
import { useRequestGuard } from "../../../lib/request-guard";

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
  const [confirmDialog, setConfirmDialog] = useState(defaultS3ConfirmDialog);
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const requestGuard = useRequestGuard(`${target.ref}:${activeSession.startedAt || "inactive"}`);
  const {
    panel: panelClass,
    muted: mutedClass,
    border: borderClass,
    subtlePanel: subtlePanelClass,
    input: inputClass,
    rowHover: rowHoverClass,
    activeRow: activeRowClass,
  } = connectorConsoleTheme(theme);
  const latestAction = approvalsForTarget(approvals?.data, target.ref)[0] || null;
  const selectedObject = objects.find((item) => item.key === selectedKey) || null;
  const visibleBytes = visibleObjectBytes(objects);
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
    setState({ state: "idle", error: "", message: "" });
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  useEffect(() => {
    if (!activeSession.active) return;
    void refreshObjectsForEffect({ reset: true });
  }, [activeSession.active, activeSession.startedAt, target.ref]);

  useEffect(() => {
    if (selectedKey) return;
    requestGuard.invalidate("metadata");
    setMetadata(null);
    setMetadataSearch("");
  }, [requestGuard, selectedKey]);

  async function runS3Action({ actionName, input, reason, busy = "running", suppressError = false, channel = actionName }) {
    return runGuardedConnectorAction({
      requestGuard,
      channel,
      targetRef: target.ref,
      actionName,
      input,
      reason,
      busy,
      product: "S3",
      setState,
      onRefreshActivity,
      suppressError,
    });
  }

  async function refreshObjects({ reset = true, token = "", nextPrefix = prefix, nextSearch = search } = {}) {
    if (!activeSession.active) return;
    const item = await runS3Action({
      actionName: "list_objects",
      input: { prefix: nextPrefix, search: nextSearch, cursor: reset ? "" : token, limit: 100 },
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
    setMetadata(null);
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
          if (!uploaded) {
            setUploadDialog((current) => ({ ...current, pending: false }));
            return;
          }
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
        if (!uploaded) {
          setUploadDialog((current) => ({ ...current, pending: false }));
          return;
        }
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

  function requestDelete() {
    if (!selectedKey) return;
    openConfirmDialog({
      title: "Delete S3 object",
      description: "This permanently deletes the selected object from the bucket.",
      details: [{ label: "Object", value: JSON.stringify(selectedKey) }],
      danger: true,
      action: async () => {
        const deleted = await runS3Action({
          actionName: "delete_object",
          input: { key: selectedKey },
          reason: "manual S3 browser object delete",
          busy: "deleting",
        });
        if (!deleted) return false;
        setSelectedKey("");
        setMetadata(null);
        setMetadataSearch("");
        await refreshObjects({ reset: true });
        return true;
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
      const completed = await confirmDialog.action();
      if (completed === false) {
        setConfirmDialog((current) => ({ ...current, pending: false }));
        return;
      }
      setConfirmDialog(defaultS3ConfirmDialog);
    } catch {
      setConfirmDialog((current) => ({ ...current, pending: false }));
    }
  }

  if (!activeSession.active) {
    return (
      <StructuredSessionEmpty
        icon={Database}
        title="No active S3 session"
        description="Start a structured session to browse objects through the connector approval, history, and audit pipeline."
        buttonLabel="Start S3 session"
        onStart={onNewStructuredSession}
        panelClass={panelClass}
        mutedClass={mutedClass}
        footer={<S3EndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />}
      />
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
      <div className="grid min-h-0 gap-4 overflow-hidden p-4 xl:grid-cols-[380px_minmax(0,1fr)]">
        <S3ObjectBrowser
          target={target}
          directories={directories}
          objects={objects}
          prefix={prefix}
          search={search}
          selectedKey={selectedKey}
          nextToken={nextToken}
          latestAction={latestAction}
          state={state}
          classes={{
            border: borderClass,
            muted: mutedClass,
            subtlePanel: subtlePanelClass,
            input: inputClass,
            rowHover: rowHoverClass,
            activeRow: activeRowClass,
          }}
          onPrefixChange={setPrefix}
          onSearchChange={setSearch}
          onSearch={() => void refreshObjects({ reset: true })}
          onBucketInfo={() => void readBucketInfo()}
          onOpenTransfer={() => setTransferOpen(true)}
          onOpenUpload={openUploadDialog}
          onRefresh={() => void refreshObjects({ reset: true })}
          onOpenParent={() => void openParentDirectory()}
          onOpenDirectory={(directoryPrefix) => void openDirectory(directoryPrefix)}
          onSelectObject={(key) => void selectObject(key)}
          onLoadMore={() => void refreshObjects({ reset: false, token: nextToken })}
        />

        <S3ObjectDetailPane
          active={activeSession.active}
          selectedKey={selectedKey}
          selectedObject={selectedObject}
          metadata={metadata}
          directories={directories}
          objects={objects}
          visibleBytes={visibleBytes}
          prefix={prefix}
          search={search}
          metadataSearch={metadataSearch}
          state={state}
          classes={{ border: borderClass, muted: mutedClass, subtlePanel: subtlePanelClass, input: inputClass }}
          onMetadataSearch={setMetadataSearch}
          onOpenLifecycle={() => setLifecycleOpen(true)}
          onOpenPresign={() => setPresignOpen(true)}
          onOpenVersions={() => setVersionsOpen(true)}
          onDownload={() => void downloadSelected()}
          onDelete={requestDelete}
        />
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
          joinRemotePath: joinTransferPath,
          normalizeRemoteDirectoryInput: normalizeTransferDirectory,
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
      <S3ConfirmDialog
        value={confirmDialog}
        theme={theme}
        onClose={() => setConfirmDialog(defaultS3ConfirmDialog)}
        onConfirm={confirmPendingAction}
      />
    </div>
  );
}
