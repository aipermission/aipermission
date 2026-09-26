export type BackupStorage = { used_bytes: number; quota_enabled: boolean; quota_bytes?: number; remaining_bytes?: number; pending_deletions: number };
export type BackupRetentionPolicy = { enabled: boolean; keep_latest?: number };
export type BackupRetentionPreview = { keep_latest: number; retain_count: number; retain_bytes: number; delete_count: number; delete_bytes: number };

function objectResponse(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Backup retention response is invalid.");
  return value as Record<string, unknown>;
}

function nonnegativeInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

function byteCount(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && Number.isInteger(value) && value >= 0;
}

export function backupStorageResponse(value: unknown): BackupStorage {
  const data = objectResponse(value);
  if (!byteCount(data.used_bytes) || !nonnegativeInteger(data.pending_deletions) || typeof data.quota_enabled !== "boolean") throw new Error("Backup storage response is invalid.");
  if (data.quota_enabled && (!byteCount(data.quota_bytes) || !byteCount(data.remaining_bytes))) throw new Error("Backup storage quota response is invalid.");
  return data as BackupStorage;
}

export function backupRetentionPolicyResponse(value: unknown): BackupRetentionPolicy {
  const data = objectResponse(value);
  if (typeof data.enabled !== "boolean" || (data.keep_latest !== undefined && (!nonnegativeInteger(data.keep_latest) || data.keep_latest > 1000)) || (data.enabled && (!nonnegativeInteger(data.keep_latest) || data.keep_latest < 1))) throw new Error("Backup retention policy response is invalid.");
  return data as BackupRetentionPolicy;
}

export function backupRetentionPreviewResponse(value: unknown): BackupRetentionPreview {
  const data = objectResponse(value);
  if (!["keep_latest", "retain_count", "delete_count"].every((field) => nonnegativeInteger(data[field])) || !byteCount(data.retain_bytes) || !byteCount(data.delete_bytes) || Number(data.keep_latest) > 1000) throw new Error("Backup retention preview response is invalid.");
  return data as BackupRetentionPreview;
}

export function backupRetentionUpdateResponse(value: unknown) {
  const data = objectResponse(value);
  if (!nonnegativeInteger(data.deleted_count)) throw new Error("Backup retention update response is invalid.");
  return { policy: backupRetentionPolicyResponse(data.policy), preview: data.preview == null ? null : backupRetentionPreviewResponse(data.preview), deleted_count: data.deleted_count };
}
