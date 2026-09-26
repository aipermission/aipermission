export type SettingsDatabase = { database_name?: string; database_size_bytes?: number };

export function settingsDatabaseResponse(value: unknown): SettingsDatabase {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Database status response is invalid.");
  const data = value as Record<string, unknown>;
  if (data.database_name !== undefined && typeof data.database_name !== "string") throw new Error("Database name response is invalid.");
  const size = data.database_size_bytes;
  if (size !== undefined && (typeof size !== "number" || !Number.isFinite(size) || !Number.isInteger(size) || size < 0)) throw new Error("Database size response is invalid.");
  return data as SettingsDatabase;
}
