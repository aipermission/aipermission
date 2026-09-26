import { backupItems } from "../components/settings/backup-contracts.ts";

export type RemoteBackupStream = { id: string; database_name: string };
export type RemoteBackupVersion = {
  id: string;
  filename?: string;
  created_at?: string;
  size_bytes?: number;
  source_installation_id?: string;
};

export function remoteBackupStreams(value: unknown): RemoteBackupStream[] {
  return backupItems(
    value,
    (item): item is RemoteBackupStream => objectRecord(item) && nonEmptyString(item.id) && typeof item.database_name === "string",
  );
}

export function remoteBackupVersions(value: unknown): RemoteBackupVersion[] {
  if (!objectRecord(value) || !Array.isArray(value.items)) throw new Error("Invalid remote backup version response.");
  if (value.items.length === 0) return [];
  const stream: unknown = value.items[0];
  if (!objectRecord(stream) || !Array.isArray(stream.backups) || !stream.backups.every(validVersion)) {
    throw new Error("Invalid remote backup version response.");
  }
  return stream.backups;
}

function validVersion(value: unknown): value is RemoteBackupVersion {
  return (
    objectRecord(value) &&
    nonEmptyString(value.id) &&
    ["filename", "created_at", "source_installation_id"].every((key) => value[key] === undefined || typeof value[key] === "string") &&
    (value.size_bytes === undefined ||
      (typeof value.size_bytes === "number" && Number.isSafeInteger(value.size_bytes) && value.size_bytes >= 0))
  );
}

function objectRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function nonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}
