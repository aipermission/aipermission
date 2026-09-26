export type RetentionSettings = {
  history_days: number;
  audit_days: number;
  console_days: number;
  message_days: number;
};

export function retentionSettingsResponse(value: unknown): RetentionSettings {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Retention settings response is invalid.");
  const data = value as Record<string, unknown>;
  const fields = ["history_days", "audit_days", "console_days", "message_days"];
  if (!fields.every((field) => typeof data[field] === "number" && Number.isSafeInteger(data[field]) && data[field] >= 0)) {
    throw new Error("Retention settings response is invalid.");
  }
  return data as RetentionSettings;
}

export function retentionPurgeResponse(value: unknown): { deleted: number } {
  if (
    !value ||
    typeof value !== "object" ||
    !("deleted" in value) ||
    typeof value.deleted !== "number" ||
    !Number.isSafeInteger(value.deleted) ||
    value.deleted < 0
  ) {
    throw new Error("Retention purge response is invalid.");
  }
  return { deleted: value.deleted };
}
