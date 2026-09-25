export type BackupProvider = {
  id: number;
  name: string;
  provider_type?: string;
  public?: { base_url?: string };
  status?: string;
};

export type BackupCatalogItem = { provider_type: string; label: string };

export type BackupRecord = {
  id: number;
  filename?: string;
  database_name?: string;
  database_id?: string;
};

export type LoadState<T> = { state: string; data: T[]; error: string | null };

export function backupItems<T>(response: unknown, isItem: (item: unknown) => item is T): T[] {
  if (!response || typeof response !== "object" || !("items" in response)) throw new Error("Invalid backup response.");
  const items = response.items;
  if (!Array.isArray(items) || !items.every(isItem)) throw new Error("Invalid backup items in response.");
  return items;
}

export function isBackupProvider(value: unknown): value is BackupProvider {
  return Boolean(
    value &&
    typeof value === "object" &&
    "id" in value &&
    typeof value.id === "number" &&
    Number.isSafeInteger(value.id) &&
    value.id > 0 &&
    "name" in value &&
    typeof value.name === "string",
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
  return Boolean(
    value && typeof value === "object" && "id" in value && typeof value.id === "number" && Number.isSafeInteger(value.id) && value.id > 0,
  );
}
