export type BackupProvider = {
  id: number;
  name: string;
  provider_type?: string;
  public?: { base_url?: string } | null;
  status?: string;
  last_checked_at?: string;
  has_secret?: boolean;
};

export type BackupCatalogItem = { provider_type: string; label: string };

export type BackupRecord = {
  id: number;
  filename?: string;
  database_name?: string;
  database_id?: string;
  size_bytes?: number;
  backup_created_at?: string;
  uploaded_at?: string;
  source_machine?: string;
  checksum_sha256?: string;
};

export type LoadState<T> = { state: string; data: T[]; error: string | null };

export function uploadedBackupRecordResponse(value: unknown): BackupRecord & { filename: string } {
  if (!isBackupRecord(value) || typeof value.filename !== "string" || !value.filename) throw new Error("Invalid uploaded backup record.");
  return { ...value, filename: value.filename };
}

export function backupCountResponse(value: unknown): { deleted_count: number; keep_latest: number } {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid backup deletion response.");
  const data = value as Record<string, unknown>;
  if (
    typeof data.deleted_count !== "number" ||
    !Number.isSafeInteger(data.deleted_count) ||
    data.deleted_count < 0 ||
    (data.keep_latest !== undefined &&
      (typeof data.keep_latest !== "number" || !Number.isSafeInteger(data.keep_latest) || data.keep_latest < 1))
  )
    throw new Error("Invalid backup deletion response.");
  return { deleted_count: data.deleted_count, keep_latest: typeof data.keep_latest === "number" ? data.keep_latest : 0 };
}

export function backupItems<T>(response: unknown, isItem: (item: unknown) => item is T): T[] {
  if (!response || typeof response !== "object" || !("items" in response)) throw new Error("Invalid backup response.");
  const items = response.items;
  if (!Array.isArray(items) || !items.every(isItem)) throw new Error("Invalid backup items in response.");
  return items;
}

export function isBackupProvider(value: unknown): value is BackupProvider {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const data = value as Record<string, unknown>;
  const publicData = data.public;
  const validPublic =
    publicData == null ||
    (typeof publicData === "object" &&
      !Array.isArray(publicData) &&
      (!("base_url" in publicData) || typeof publicData.base_url === "string"));
  return (
    validBackupID(data.id) &&
    typeof data.name === "string" &&
    optionalStrings(data, ["provider_type", "status", "last_checked_at"]) &&
    (data.has_secret === undefined || typeof data.has_secret === "boolean") &&
    validPublic
  );
}

export function isBackupCatalogItem(value: unknown): value is BackupCatalogItem {
  return Boolean(
    value &&
    typeof value === "object" &&
    "provider_type" in value &&
    typeof value.provider_type === "string" &&
    "label" in value &&
    typeof value.label === "string",
  );
}

export function isBackupRecord(value: unknown): value is BackupRecord {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const data = value as Record<string, unknown>;
  return (
    validBackupID(data.id) &&
    optionalStrings(data, [
      "filename",
      "database_name",
      "database_id",
      "backup_created_at",
      "uploaded_at",
      "source_machine",
      "checksum_sha256",
    ]) &&
    (data.size_bytes === undefined ||
      (typeof data.size_bytes === "number" &&
        Number.isFinite(data.size_bytes) &&
        Number.isInteger(data.size_bytes) &&
        data.size_bytes >= 0))
  );
}

function validBackupID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

function optionalStrings(value: Record<string, unknown>, fields: string[]) {
  return fields.every((field) => value[field] === undefined || typeof value[field] === "string");
}
