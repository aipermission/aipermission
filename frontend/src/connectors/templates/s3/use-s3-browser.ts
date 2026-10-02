import { useEffect, useEffectEvent, useRef, useState } from "react";
import { saveBlob } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import { useRequestGuard } from "../../../lib/request-guard";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { approvalsForTarget, base64Blob, filenameFromKey, parentPrefix, safeDownloadName, visibleObjectBytes } from "./helpers";
import type { S3MetadataPanelProps } from "./metadata-panel";
import { readS3Directories, readS3Metadata, readS3Objects, s3OutputRecord } from "./output";

type BrowserObject = { key: string; size?: number | string | null; last_modified?: string; etag?: string };
type BrowserDirectory = { prefix: string; name?: string };
type BrowserApproval = { target_ref: string; status: string; action_name: string };
type ObjectListing = {
  query: { prefix: string; search: string };
  directories: BrowserDirectory[];
  objects: BrowserObject[];
  nextToken: string;
};
const emptyListing: ObjectListing = { query: { prefix: "", search: "" }, directories: [], objects: [], nextToken: "" };
export type S3BrowserOptions = {
  target: { ref: string };
  approvals?: { data?: BrowserApproval[] } | null;
  session?: { active: boolean; startedAt?: string } | null;
  onRefreshActivity?: () => Promise<unknown> | void;
};
type RefreshOptions = { reset?: boolean; token?: string; nextPrefix?: string; nextSearch?: string };
type NativeWritable = { write: (_blob: Blob) => Promise<void>; close: () => Promise<void>; abort?: () => Promise<void> };
type NativeSaveHandle = { createWritable: () => Promise<NativeWritable> };

export function useS3Browser({ target, approvals, session, onRefreshActivity }: S3BrowserOptions) {
  const activeSession = session || { active: false, startedAt: "" };
  const [prefix, setPrefix] = useState("");
  const [search, setSearch] = useState("");
  const [listing, setListing] = useState<ObjectListing>(emptyListing);
  const listingRef = useRef<ObjectListing | null>(listing);
  const { directories, objects, nextToken } = listing;
  const [selectedKey, setSelectedKey] = useState("");
  const [metadata, setMetadata] = useState<S3MetadataPanelProps["metadata"]>(null);
  const [metadataSearch, setMetadataSearch] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const scopeKey = `${target.ref}:${activeSession.active ? activeSession.startedAt || "active" : "inactive"}`;
  const requestGuard = useRequestGuard(scopeKey);
  const scopeKeyRef = useRef(scopeKey);
  const selectedKeyRef = useRef(selectedKey);
  scopeKeyRef.current = scopeKey;
  selectedKeyRef.current = selectedKey;
  const refreshObjectsForEffect = useEffectEvent((options: RefreshOptions) => refreshObjects(options));

  useEffect(() => {
    setPrefix("");
    setSearch("");
    replaceListing(emptyListing);
    requestGuard.invalidate("metadata");
    setSelectedKey("");
    setMetadata(null);
    setMetadataSearch("");
    setState({ state: "idle", error: "", message: "" });
    return () => {
      listingRef.current = null;
    };
  }, [requestGuard, target.ref, activeSession.active, activeSession.startedAt]);

  useEffect(() => {
    if (!activeSession.active) return;
    void refreshObjectsForEffect({ reset: true, nextPrefix: "", nextSearch: "" });
  }, [activeSession.active, activeSession.startedAt, target.ref]);

  useEffect(() => {
    if (selectedKey) return;
    requestGuard.invalidate("metadata");
    setMetadata(null);
    setMetadataSearch("");
  }, [requestGuard, selectedKey]);

  function replaceListing(next: ObjectListing) {
    listingRef.current = next;
    setListing(next);
  }

  async function runS3Action({
    actionName,
    input,
    reason,
    busy = "running",
    suppressError = false,
    channel = actionName,
  }: {
    actionName: string;
    input: Record<string, unknown>;
    reason: string;
    busy?: string;
    suppressError?: boolean;
    channel?: string;
  }) {
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

  async function refreshObjects({ reset = true, token = "", nextPrefix = prefix, nextSearch = search }: RefreshOptions = {}) {
    if (!activeSession.active || scopeKeyRef.current !== scopeKey) return [];
    const previous = listingRef.current;
    if (!previous || (!reset && (!token || token !== previous.nextToken))) return [];
    const query = reset ? { prefix: nextPrefix, search: nextSearch } : previous.query;
    const pending = { ...previous, nextToken: "" };
    replaceListing(pending);
    try {
      const item = await runS3Action({
        actionName: "list_objects",
        input: { ...query, cursor: reset ? "" : token, limit: 100 },
        reason: "manual S3 browser object list",
        busy: "loading",
        channel: "objects",
      });
      if (listingRef.current !== pending) return [];
      if (!item) {
        if (!reset) replaceListing(previous);
        return [];
      }
      const output = s3OutputRecord(item.output);
      const nextDirectories = readS3Directories(output.directories);
      const nextObjects = readS3Objects(output.objects);
      replaceListing({
        query,
        directories: reset ? nextDirectories : [...previous.directories, ...nextDirectories],
        objects: reset ? nextObjects : [...previous.objects, ...nextObjects],
        nextToken: typeof output.next_cursor === "string" ? output.next_cursor : "",
      });
      if (reset) setSelectedKey((current) => (current && !nextObjects.some((object) => object.key === current) ? "" : current));
      return nextObjects;
    } catch (error) {
      if (!reset && listingRef.current === pending) replaceListing(previous);
      throw error;
    }
  }

  async function openDirectory(directoryPrefix: string) {
    if (!activeSession.active || !directoryPrefix) return;
    setPrefix(directoryPrefix);
    setSearch("");
    clearSelection();
    await refreshObjects({ reset: true, nextPrefix: directoryPrefix, nextSearch: "" });
  }

  async function openParentDirectory() {
    const parent = parentPrefix(listingRef.current?.query.prefix || "");
    setPrefix(parent);
    setSearch("");
    clearSelection();
    await refreshObjects({ reset: true, nextPrefix: parent, nextSearch: "" });
  }

  async function selectObject(key: string) {
    if (!activeSession.active || !key) return;
    if (selectedKey === key) {
      clearSelection();
      return;
    }
    await readObjectMetadata(key);
  }

  async function readObjectMetadata(key: string) {
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
    if (item) setMetadata(readS3Metadata(item.output));
  }

  async function downloadSelected() {
    const key = selectedKey;
    if (!key) return;
    const operationScope = scopeKey;
    const preparation = requestGuard.begin("download-preparation");
    const filename = filenameFromKey(key);
    try {
      const saveHandle = await chooseSaveHandle(filename);
      if (!preparation.isCurrent() || scopeKeyRef.current !== operationScope || selectedKeyRef.current !== key) return;
      if (saveHandle === false) {
        setState({ state: "idle", error: "", message: "Download canceled." });
        return;
      }
      const item = await runS3Action({
        actionName: "download_object",
        input: { key },
        reason: "manual S3 browser object download",
        busy: "downloading",
      });
      if (!item || !preparation.isCurrent() || scopeKeyRef.current !== operationScope || selectedKeyRef.current !== key) return;
      const output = s3OutputRecord(item.output);
      if (typeof output.content_base64 !== "string") throw new Error("Invalid S3 download content.");
      const blob = base64Blob(
        output.content_base64,
        typeof output.content_type === "string" ? output.content_type : "application/octet-stream",
      );
      const savedFilename = typeof output.filename === "string" && output.filename ? output.filename : filename;
      if (saveHandle) {
        await writeNativeDownload(saveHandle, blob);
        if (preparation.isCurrent() && scopeKeyRef.current === operationScope && selectedKeyRef.current === key) {
          setState({ state: "idle", error: "", message: `Saved ${savedFilename}.` });
        }
        return;
      }
      await saveBlob(blob, savedFilename, { picker: false });
    } catch (error) {
      if (preparation.isCurrent()) {
        setState({ state: "error", error: errorMessage(error, "Download failed."), message: "" });
      }
    } finally {
      preparation.complete();
    }
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
    if (item) setMetadata(readS3Metadata(item.output));
  }

  function clearSelection() {
    requestGuard.invalidate("metadata");
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
    appliedQuery: listing.query,
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

async function writeNativeDownload(saveHandle: NativeSaveHandle, blob: Blob) {
  let writable: NativeWritable | undefined;
  try {
    writable = await saveHandle.createWritable();
    await writable.write(blob);
    await writable.close();
  } catch (error) {
    if (writable && typeof writable.abort === "function") {
      try {
        await writable.abort();
      } catch {
        // The original write failure is the actionable error.
      }
    }
    throw error;
  }
}

async function chooseSaveHandle(filename: string): Promise<NativeSaveHandle | false | null> {
  if (typeof window === "undefined") return null;
  const picker = (window as Window & { showSaveFilePicker?: (_options: { suggestedName: string }) => Promise<NativeSaveHandle> })
    .showSaveFilePicker;
  if (typeof picker !== "function") return null;
  try {
    return await picker.call(window, { suggestedName: safeDownloadName(filename) });
  } catch (error) {
    if (!error || typeof error !== "object" || !("name" in error) || error.name !== "AbortError") throw error;
    return false;
  }
}
