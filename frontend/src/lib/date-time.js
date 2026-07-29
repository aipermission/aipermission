export function formatRelativeAge(value) {
  if (!value) return "";
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "";
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function formatLocalTimestamp(value) {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "";
  return new Date(timestamp).toLocaleString();
}

export function formatRelativeDeadline(value) {
  if (!value) return "";
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "";
  const seconds = Math.ceil((timestamp - Date.now()) / 1000);
  if (seconds <= 0) return "expired";
  if (seconds < 60) return `expires in ${seconds}s`;
  const minutes = Math.ceil(seconds / 60);
  if (minutes < 60) return `expires in ${minutes} min`;
  const hours = Math.ceil(minutes / 60);
  return `expires in ${hours}h`;
}

export function toRFC3339(value) {
  if (!value) return "";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? "" : parsed.toISOString();
}

export function toLocalDateTime(value) {
  if (!value) return "";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "";
  const local = new Date(parsed.getTime() - parsed.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 16);
}
