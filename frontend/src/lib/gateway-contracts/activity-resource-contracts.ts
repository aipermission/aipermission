export type RuntimeMessage = {
  id: number;
  token_id: number;
  token_name?: string;
  runtime_id?: number | null;
  target_name?: string;
  session_id?: number | null;
  direction: "ai_to_user" | "user_to_ai";
  message: string;
  consumed_at?: string | null;
  created_at: string;
};

export type BackupFreshnessItem = {
  provider_id: number;
  provider_name?: string;
  remote_newer?: boolean;
  latest_remote_id?: string;
  latest_remote_at?: string;
  latest_remote_source?: string;
  latest_known_id?: string;
  latest_known_at?: string;
};
export type BackupCheckError = { provider_id: number; provider_name?: string; error?: string };

export function runtimeMessagesResponse(value: unknown): RuntimeMessage[] {
  if (
    !Array.isArray(value) ||
    value.length > 100 ||
    !value.every(validRuntimeMessage) ||
    new Set(value.map((row) => row.id)).size !== value.length
  )
    throw new Error("Invalid runtime messages response.");
  return value;
}

export function backupFreshnessResponse(value: unknown): { items: BackupFreshnessItem[]; checkErrors: BackupCheckError[] } {
  const data = objectRecord(value);
  const items: unknown = data?.items ?? [];
  const checkErrors: unknown = data?.check_errors ?? [];
  if (
    !data ||
    !Array.isArray(items) ||
    !items.every(validFreshnessItem) ||
    !Array.isArray(checkErrors) ||
    !checkErrors.every(validCheckError)
  ) {
    throw new Error("Invalid backup freshness response.");
  }
  return { items, checkErrors };
}

function validRuntimeMessage(value: unknown): value is RuntimeMessage {
  const row = objectRecord(value);
  return (
    !!row &&
    positiveID(row.id) &&
    positiveID(row.token_id) &&
    optionalID(row.runtime_id) &&
    optionalID(row.session_id) &&
    (row.direction === "ai_to_user" || row.direction === "user_to_ai") &&
    typeof row.message === "string" &&
    typeof row.created_at === "string" &&
    optionalString(row.token_name) &&
    optionalString(row.target_name) &&
    (row.consumed_at === null || optionalString(row.consumed_at))
  );
}

function validFreshnessItem(value: unknown): value is BackupFreshnessItem {
  const row = objectRecord(value);
  return (
    !!row &&
    positiveID(row.provider_id) &&
    (row.remote_newer === undefined || typeof row.remote_newer === "boolean") &&
    ["provider_name", "latest_remote_id", "latest_remote_at", "latest_remote_source", "latest_known_id", "latest_known_at"].every((key) =>
      optionalString(row[key]),
    )
  );
}

function validCheckError(value: unknown): value is BackupCheckError {
  const row = objectRecord(value);
  return !!row && positiveID(row.provider_id) && optionalString(row.provider_name) && optionalString(row.error);
}

function objectRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

function positiveID(value: unknown) {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

function optionalID(value: unknown) {
  return value === undefined || value === null || positiveID(value);
}

function optionalString(value: unknown) {
  return value === undefined || typeof value === "string";
}
