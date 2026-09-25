import { useRef, useState, type ChangeEvent } from "react";
import { apiPost } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { localFileID, mergeUploadQueue, relocateUploadQueue } from "../../lib/file-transfer-utils";
import { useRequestGuard } from "../../lib/request-guard";
import { remoteExpansionEntries, type RemoteEntry } from "./transfer-contracts";

const maxTransferObjectBytes = 512 * 1024 * 1024;
const maxTransferBatchBytes = 1024 * 1024 * 1024;
const maxTransferBatchItems = 100;

type Notice = { tone: "bad"; message: string } | null;
type UploadItem = { id: string; file: File; name: string; size: number; relative_path: string; remote_path: string };
type DownloadItem = { id: string; path: string; name: string; size?: number };
type Props = {
  runtimeTarget: { id: number } | null;
  defaultRemoteDir: string;
  recursive: boolean;
  joinRemotePath: (_directory: string, _name: string) => string;
  onNotice: (_notice: Notice) => void;
};

export function useTransferQueues({ runtimeTarget, defaultRemoteDir, recursive, joinRemotePath, onNotice }: Props) {
  const [mode, setMode] = useState<"upload" | "download">("upload");
  const [remoteDir, setRemoteDir] = useState(defaultRemoteDir);
  const [uploadQueue, setUploadQueue] = useState<UploadItem[]>([]);
  const [downloadQueue, setDownloadQueue] = useState<DownloadItem[]>([]);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const folderInputRef = useRef<HTMLInputElement | null>(null);
  const uploadQueueRef = useRef(uploadQueue);
  const downloadQueueRef = useRef(downloadQueue);
  const requestGuard = useRequestGuard(`transfer-queue:${runtimeTarget?.id || ""}`);
  uploadQueueRef.current = uploadQueue;
  downloadQueueRef.current = downloadQueue;

  function reset(nextRemoteDir = defaultRemoteDir) {
    requestGuard.invalidate("expand");
    setMode("upload");
    setRemoteDir(nextRemoteDir);
    setUploadQueue([]);
    setDownloadQueue([]);
    if (fileInputRef.current) fileInputRef.current.value = "";
    if (folderInputRef.current) folderInputRef.current.value = "";
  }

  function clear(direction: "upload" | "download") {
    if (direction === "upload") {
      setUploadQueue([]);
      setRemoteDir(defaultRemoteDir);
      if (fileInputRef.current) fileInputRef.current.value = "";
      return;
    }
    requestGuard.invalidate("expand");
    setDownloadQueue([]);
  }

  function handleLocalFileChange(event: ChangeEvent<HTMLInputElement>) {
    const files = Array.from(event.target.files || []);
    if (files.length === 0) return;
    const oversized = files.find((file) => file.size > maxTransferObjectBytes);
    if (oversized) {
      onNotice({ tone: "bad", message: `${oversized.name} exceeds the 512 MiB per-object upload limit.` });
      event.target.value = "";
      return;
    }
    const additions = files.map((file) => ({
      id: localFileID(file),
      file,
      name: file.webkitRelativePath || file.name,
      size: file.size,
      relative_path: file.webkitRelativePath || file.name,
      remote_path: joinRemotePath(remoteDir, file.webkitRelativePath || file.name),
    }));
    const next = mergeUploadQueue(uploadQueueRef.current, additions);
    const totalSize = next.reduce((total, item) => total + Number(item.size || 0), 0);
    if (next.length > maxTransferBatchItems || totalSize > maxTransferBatchBytes) {
      onNotice({ tone: "bad", message: `The upload queue cannot exceed ${maxTransferBatchItems} objects or 1 GiB total size.` });
      event.target.value = "";
      return;
    }
    onNotice(null);
    setUploadQueue(next);
    event.target.value = "";
  }

  async function addRemoteFiles(entries: RemoteEntry[]): Promise<boolean> {
    if (!runtimeTarget) return false;
    const request = requestGuard.begin("expand");
    const selected = Array.isArray(entries) ? entries : [];
    const files = selected.filter((entry) => entry?.type === "file");
    const directories = recursive ? selected.filter((entry) => entry?.type === "directory") : [];
    try {
      for (const directory of directories) {
        const expanded = await apiPost(
          "/api/file-transfers/expand",
          {
            runtime_id: Number(runtimeTarget.id),
            path: directory.path,
          },
          { signal: request.signal },
        );
        if (!request.isCurrent()) return false;
        files.push(...remoteExpansionEntries(expanded).filter((entry) => entry.type === "file"));
      }
    } catch (error) {
      if (!request.isCurrent()) return false;
      onNotice({ tone: "bad", message: errorMessage(error, "Could not expand the selected folder.") });
      return false;
    } finally {
      request.complete();
    }
    if (!request.isCurrent()) return false;
    const existing = new Set(downloadQueueRef.current.map((item) => item.path));
    const nextFiles: RemoteEntry[] = [];
    for (const entry of files) {
      if (existing.has(entry.path)) continue;
      existing.add(entry.path);
      nextFiles.push(entry);
    }
    if (nextFiles.length === 0) return true;
    const additions = nextFiles.map((entry) => ({
      id: `remote-${entry.path}`,
      path: entry.path,
      name: entry.name,
      size: entry.size,
    }));
    const nextQueue = [...downloadQueueRef.current, ...additions];
    const nextSize = nextQueue.reduce((total, entry) => total + Number(entry.size || 0), 0);
    const oversized = additions.find((entry) => Number(entry.size || 0) > maxTransferObjectBytes);
    if (oversized) {
      onNotice({ tone: "bad", message: `${oversized.name} exceeds the 512 MiB per-object download limit.` });
      return false;
    }
    if (nextQueue.length > maxTransferBatchItems || nextSize > maxTransferBatchBytes) {
      onNotice({ tone: "bad", message: `The download queue cannot exceed ${maxTransferBatchItems} objects or 1 GiB total size.` });
      return false;
    }
    setDownloadQueue(nextQueue);
    return true;
  }

  function removeQueueItem(id: string) {
    if (mode === "upload") setUploadQueue((current) => current.filter((item) => item.id !== id));
    else setDownloadQueue((current) => current.filter((item) => item.id !== id));
  }

  function moveQueueItem(id: string, direction: number) {
    function move<T extends { id: string }>(current: T[]): T[] {
      const index = current.findIndex((item) => item.id === id);
      const nextIndex = index + direction;
      if (index < 0 || nextIndex < 0 || nextIndex >= current.length) return current;
      const next = [...current];
      [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
      return next;
    }
    if (mode === "upload") setUploadQueue(move);
    else setDownloadQueue(move);
  }

  function updateRemoteDirectory(pathValue: string) {
    setRemoteDir(pathValue);
    setUploadQueue((current) => relocateUploadQueue(current, pathValue, joinRemotePath));
  }

  return {
    mode,
    setMode,
    remoteDir,
    setRemoteDir,
    uploadQueue,
    downloadQueue,
    queue: mode === "upload" ? uploadQueue : downloadQueue,
    fileInputRef,
    folderInputRef,
    resetQueues: reset,
    clearQueue: clear,
    handleLocalFileChange,
    addRemoteFiles,
    removeQueueItem,
    moveQueueItem,
    updateRemoteDirectory,
  };
}
