export function normalizeTransferDirectory(value: string | null | undefined): string {
  const text = String(value ?? "");
  if (!text) return "/";
  return text.startsWith("/") ? text : `/${text}`;
}

export function joinTransferPath(directory: string, name: string): string {
  const prefix = normalizeTransferDirectory(directory);
  return `${prefix}${prefix.endsWith("/") ? "" : "/"}${name}`;
}
