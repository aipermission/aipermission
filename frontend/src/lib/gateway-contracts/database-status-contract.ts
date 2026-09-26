export type DatabaseCatalogItem = {
  id: string;
  name: string;
  unlocked: boolean;
  current?: boolean;
  state?: string;
};

export type DatabaseStatus = {
  state?: string;
  database_id?: string;
  database_name?: string;
  unlocked?: boolean;
  databases: DatabaseCatalogItem[];
};

export function databaseStatusResponse(value: unknown): DatabaseStatus {
  const record = objectRecord(value);
  if (
    !record ||
    !optionalString(record.state) ||
    !optionalString(record.database_id) ||
    !optionalString(record.database_name) ||
    (record.unlocked !== undefined && typeof record.unlocked !== "boolean")
  )
    throw new Error("Invalid database status response.");
  const databases: unknown = record.databases ?? [];
  if (!Array.isArray(databases) || !databases.every(validDatabaseCatalogItem)) throw new Error("Invalid database catalog response.");
  return {
    state: typeof record.state === "string" ? record.state : undefined,
    database_id: typeof record.database_id === "string" ? record.database_id : undefined,
    database_name: typeof record.database_name === "string" ? record.database_name : undefined,
    unlocked: typeof record.unlocked === "boolean" ? record.unlocked : undefined,
    databases,
  };
}

function validDatabaseCatalogItem(value: unknown): value is DatabaseCatalogItem {
  const row = objectRecord(value);
  return (
    !!row &&
    typeof row.id === "string" &&
    row.id.length > 0 &&
    typeof row.name === "string" &&
    typeof row.unlocked === "boolean" &&
    (row.current === undefined || typeof row.current === "boolean") &&
    optionalString(row.state)
  );
}

function objectRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

function optionalString(value: unknown) {
  return value === undefined || typeof value === "string";
}
