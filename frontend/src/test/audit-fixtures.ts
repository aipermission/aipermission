import type { AuditEntry } from "../lib/gateway-contracts/audit-resource-contract";

export function auditEntryFixture(overrides: Partial<AuditEntry> = {}): AuditEntry {
  return {
    id: 1,
    event_version: 1,
    actor_type: "user",
    action: "connector.run",
    lifecycle_phase: "completed",
    payload_json: "{}",
    created_at: "2026-09-26T00:00:00Z",
    ...overrides,
  };
}
