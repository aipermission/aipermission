export function parseBackupKeepLatest(value: string | number | null | undefined): number | null {
  const text = String(value || "").trim();
  if (!/^\d+$/.test(text)) return null;
  const parsed = Number(text);
  return Number.isSafeInteger(parsed) && parsed >= 1 && parsed <= 1000 ? parsed : null;
}

export function backupRecordsActionBusy(state: string | null | undefined): boolean {
  return Boolean(state?.startsWith("restoring-") || state === "pruning" || state === "deleting-records");
}
