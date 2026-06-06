export function transferProgress(item) {
  if (!item) return { percent: 0, label: "" };
  const total = Number(item.size_bytes || 0);
  const transferred = Number(item.transferred_bytes || 0);
  const terminal = item.status === "completed" || item.status === "canceled";
  const percent = terminal ? 100 : total > 0 ? Math.min(100, Math.round((transferred / total) * 100)) : 0;
  return {
    percent,
    label: total > 0 ? `${formatBytes(transferred)} / ${formatBytes(total)}` : `${formatBytes(transferred)} transferred`,
  };
}

export function defaultRemoteDirectory() {
  return "/home";
}

export function pendingBatchItemIDs(batch) {
  return (batch?.items || []).filter((item) => item.status === "pending").map((item) => Number(item.id));
}

export function rememberedDownloadPath(server, fallback) {
  const defaultPath = normalizeRemoteDirectoryInput(fallback || "/home");
  if (typeof window === "undefined" || !server?.id) return defaultPath;
  const value = window.localStorage.getItem(downloadPathStorageKey(server.id));
  if (!value) return defaultPath;
  return normalizeRemoteDirectoryInput(value);
}

export function rememberDownloadPath(server, path) {
  if (typeof window === "undefined" || !server?.id) return;
  window.localStorage.setItem(downloadPathStorageKey(server.id), normalizeRemoteDirectoryInput(path || "/home"));
}

export function forgetDownloadPath(server) {
  if (typeof window === "undefined" || !server?.id) return;
  window.localStorage.removeItem(downloadPathStorageKey(server.id));
}

export function joinRemotePath(remoteDir, remoteName) {
  const dir = normalizeRemoteDirectoryInput(remoteDir);
  const name = String(remoteName || "").trim().replace(/^\/+/, "");
  if (dir === "/") return `/${name}`;
  return `${dir.replace(/\/+$/, "")}/${name}`;
}

export function normalizeRemoteDirectoryInput(value) {
  const text = String(value || "").trim() || "/";
  if (!text.startsWith("/")) return `/${text}`;
  return text.replace(/\/+$/, "") || "/";
}

export function localFileID(file) {
  return `${file.name}-${file.size}-${file.lastModified}-${Math.random().toString(16).slice(2)}`;
}

export function suggestedArchiveName() {
  const stamp = new Date().toISOString().slice(0, 19).replaceAll(":", "-");
  return `aipermission-download-${stamp}.zip`;
}

export function formatBytes(value) {
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

export function formatETA(value) {
  const seconds = Number(value);
  if (!Number.isFinite(seconds) || seconds < 0) return "-";
  if (seconds < 60) return `${Math.max(0, Math.round(seconds))}s`;
  const minutes = Math.floor(seconds / 60);
  const rest = Math.round(seconds % 60);
  return `${minutes}m ${rest}s`;
}

export function formatShortDate(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function downloadPathStorageKey(serverID) {
  return `aipermission-file-transfer-download-path:${serverID}`;
}
