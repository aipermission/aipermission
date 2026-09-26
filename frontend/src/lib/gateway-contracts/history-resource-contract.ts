import type { components } from "../../../types/generated-openapi";
import { connectorActionStatuses } from "./generated-connector-contract";

export type HistoryEntry = components["schemas"]["HistoryEntry"];
export type HistoryLabel = HistoryEntry["labels"][number];
export type HistoryResponse = components["schemas"]["HistoryPage"];
export type HistoryTarget = {
  ref: string;
  connector_kind: string;
  target_name: string;
  last_seen_at: string;
  project_id?: number;
  project_name?: string;
  runtime_id?: number;
  target_id?: number;
  profile_id?: number;
  profile_label?: string;
};
const statuses: ReadonlySet<string> = new Set<HistoryEntry["status"]>([
  ...connectorActionStatuses,
  "expired",
  "pending_approval",
  "pending",
  "paused",
  "untracked",
]);

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  return value as Record<string, unknown>;
}
function invalid() {
  return new Error("Invalid history response from gateway.");
}
function integer(value: unknown, minimum = 0): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= minimum;
}
function optionalString(value: unknown) {
  return value === undefined || typeof value === "string";
}
function optionalID(value: unknown) {
  return value === undefined || integer(value, 1);
}

export function historyLabelsResponse(value: unknown): HistoryLabel[] {
  if (!Array.isArray(value)) throw invalid();
  return value.map((entry) => {
    const row = record(entry);
    if (
      !integer(row.id, 1) ||
      typeof row.name !== "string" ||
      typeof row.color !== "string" ||
      !optionalString(row.created_at) ||
      !optionalString(row.updated_at)
    )
      throw invalid();
    return row as HistoryLabel;
  });
}

export function historyEntryResponse(value: unknown, expectedID?: number): HistoryEntry {
  const row = record(value);
  if (
    !integer(row.id, 1) ||
    (expectedID !== undefined && row.id !== expectedID) ||
    !integer(row.source_ref_id, 1) ||
    typeof row.status !== "string" ||
    !statuses.has(row.status) ||
    typeof row.approval_required !== "boolean"
  )
    throw invalid();
  for (const field of [
    "source_ref_type",
    "connector_kind",
    "activity_type",
    "target_name",
    "source",
    "action_name",
    "title",
    "summary",
    "created_at",
    "updated_at",
  ]) {
    if (typeof row[field] !== "string") throw invalid();
  }
  for (const field of [
    "token_name",
    "project_name",
    "profile_label",
    "preview_json",
    "input_text",
    "input_json",
    "output_text",
    "output_json",
    "error",
    "retry_policy_json",
    "user_note",
    "started_at",
    "completed_at",
  ]) {
    if (!optionalString(row[field])) throw invalid();
  }
  for (const field of ["token_id", "project_id", "runtime_id", "target_id", "profile_id"]) if (!optionalID(row[field])) throw invalid();
  for (const field of ["progress_current", "progress_total", "bytes_done", "bytes_total"]) if (!integer(row[field])) throw invalid();
  if (row.exit_code !== undefined && (typeof row.exit_code !== "number" || !Number.isSafeInteger(row.exit_code))) throw invalid();
  return { ...row, labels: historyLabelsResponse(row.labels) } as HistoryEntry;
}

export function historyPageResponse(value: unknown): HistoryResponse {
  const row = record(value);
  if (
    !Array.isArray(row.items) ||
    !integer(row.limit, 1) ||
    typeof row.has_more !== "boolean" ||
    (row.total !== undefined && !integer(row.total)) ||
    !optionalString(row.next_cursor) ||
    (row.has_more && (typeof row.next_cursor !== "string" || !row.next_cursor)) ||
    (!row.has_more && Boolean(row.next_cursor))
  )
    throw invalid();
  return { ...row, items: row.items.map((item) => historyEntryResponse(item)) } as HistoryResponse;
}

export function historyTargetsResponse(value: unknown): HistoryTarget[] {
  const row = record(value);
  if (!Array.isArray(row.items)) throw invalid();
  return row.items.map((entry) => {
    const target = record(entry);
    for (const field of ["ref", "connector_kind", "target_name", "last_seen_at"]) if (typeof target[field] !== "string") throw invalid();
    for (const field of ["project_id", "runtime_id", "target_id", "profile_id"]) if (!optionalID(target[field])) throw invalid();
    if (!optionalString(target.project_name) || !optionalString(target.profile_label)) throw invalid();
    return target as HistoryTarget;
  });
}
