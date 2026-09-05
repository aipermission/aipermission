export function normalizeTransferDirectory(value) {
  const text = String(value ?? "");
  if (!text) return "/";
  return text.startsWith("/") ? text : `/${text}`;
}

export function joinTransferPath(directory, name) {
  const prefix = normalizeTransferDirectory(directory);
  return `${prefix}${prefix.endsWith("/") ? "" : "/"}${name}`;
}
