import { useEffect, useEffectEvent, useState } from "react";
import { saveBlob } from "../../../lib/api";
import { useRequestGuard } from "../../../lib/request-guard";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { approvalsForTarget, base64Blob, filenameFromKey, parentPrefix, safeDownloadName, visibleObjectBytes } from "./helpers";

export function useS3Browser({ target, approvals, session, onRefreshActivity }) {
  const activeSession = session || { active: false, startedAt: "" };
  const [prefix, setPrefix] = useState("");
  const [search, setSearch] = useState("");
  const [directories, setDirectories] = useState([]);
  const [objects, setObjects] = useState([]);
  const [nextToken, setNextToken] = useState("");
  const [selectedKey, setSelectedKey] = useState("");
  const [metadata, setMetadata] = useState(null);
  const [metadataSearch, setMetadataSearch] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const requestGuard = useRequestGuard(`${target.ref}:${activeSession.startedAt || "inactive"}`);
  const refreshObjectsForEffect = useEffectEvent((options) => refreshObjects(options));

  useEffect(() => {
    setPrefix("");
    setSearch("");
    setDirectories([]);
    setObjects([]);
    setNextToken("");
    clearSelection();
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
    if (!activeSession.active) return [];
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
    if (reset) setSelectedKey((current) => (current && !nextObjects.some((object) => object.key === current) ? "" : current));
    return nextObjects;
  }

  async function openDirectory(directoryPrefix) {
    if (!activeSession.active || !directoryPrefix) return;
    setPrefix(directoryPrefix);
    setSearch("");
    clearSelection();
    await refreshObjects({ reset: true, nextPrefix: directoryPrefix, nextSearch: "" });
  }

  async function openParentDirectory() {
    const parent = parentPrefix(prefix);
    setPrefix(parent);
    setSearch("");
    clearSelection();
    await refreshObjects({ reset: true, nextPrefix: parent, nextSearch: "" });
  }

  async function selectObject(key) {
    if (!activeSession.active || !key) return;
    if (selectedKey === key) {
      clearSelection();
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
    if (item) setMetadata(item.output || null);
  }

  async function downloadSelected() {
    if (!selectedKey) return;
    const filename = filenameFromKey(selectedKey);
    const saveHandle = await chooseSaveHandle(filename, setState);
    if (saveHandle === false) return;
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

  async function readBucketInfo() {
    setMetadataSearch("");
    const item = await runS3Action({
      actionName: "bucket_info",
      input: {},
      reason: "manual S3 browser bucket info",
      busy: "reading",
      channel: "metadata",
    });
    if (item) setMetadata(item.output || null);
  }

  function clearSelection() {
    setSelectedKey("");
    setMetadata(null);
    setMetadataSearch("");
  }

  return {
    activeSession,
    prefix,
    setPrefix,
    search,
    setSearch,
    directories,
    objects,
    nextToken,
    selectedKey,
    selectedObject: objects.find((item) => item.key === selectedKey) || null,
    metadata,
    metadataSearch,
    setMetadataSearch,
    visibleBytes: visibleObjectBytes(objects),
    state,
    setState,
    latestAction: approvalsForTarget(approvals?.data, target.ref)[0] || null,
    runS3Action,
    refreshObjects,
    openDirectory,
    openParentDirectory,
    selectObject,
    readObjectMetadata,
    downloadSelected,
    readBucketInfo,
    clearSelection,
  };
}

async function chooseSaveHandle(filename, setState) {
  if (typeof window === "undefined" || typeof window.showSaveFilePicker !== "function") return null;
  try {
    return await window.showSaveFilePicker({ suggestedName: safeDownloadName(filename) });
  } catch (error) {
    if (error?.name !== "AbortError") throw error;
    setState({ state: "idle", error: "", message: "Download canceled." });
    return false;
  }
}
