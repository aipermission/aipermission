import type { components } from "../../../types/generated-openapi";

export type AuditEntry = components["schemas"]["AuditEntry"] & { target_ref?: string };
export type AuditPage = Omit<components["schemas"]["AuditPage"], "items"> & { items: AuditEntry[] };

function invalid() {
  return new Error("Invalid audit response from gateway.");
}
function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  return value as Record<string, unknown>;
}
function integer(value: unknown, minimum = 0): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= minimum;
}

export function auditEntryResponse(value: unknown, expectedID?: number): AuditEntry {
  const row = record(value);
  if (!integer(row.id, 1) || (expectedID !== undefined && row.id !== expectedID) || !integer(row.event_version)) throw invalid();
  for (const key of ["actor_type", "action", "lifecycle_phase", "payload_json", "created_at"]) {
    if (typeof row[key] !== "string") throw invalid();
  }
  for (const key of ["token_id", "project_id", "runtime_id", "target_id", "profile_id", "action_request_id"]) {
    if (row[key] !== undefined && !integer(row[key], 1)) throw invalid();
  }
  for (const key of ["token_name", "project_name", "connector_kind", "target_name", "target_ref"]) {
    if (row[key] !== undefined && typeof row[key] !== "string") throw invalid();
  }
  return row as AuditEntry;
}

export function auditPageResponse(value: unknown): AuditPage {
  const page = record(value);
  if (
    !Array.isArray(page.items) ||
    !integer(page.total) ||
    !integer(page.offset) ||
    !integer(page.limit, 1) ||
    (page.next_offset !== undefined && (!integer(page.next_offset) || page.next_offset <= page.offset))
  )
    throw invalid();
  return { ...page, items: page.items.map((row) => auditEntryResponse(row)) } as AuditPage;
}
