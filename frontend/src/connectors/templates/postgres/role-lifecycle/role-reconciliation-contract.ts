import { objectRecord } from "../../../../lib/api-types";
import { roleHistoryPage } from "./role-history-contract";
import type { RoleHistoryEntry } from "./role-history-types";

export type RoleDecisionMode = "presence" | "cleanup";

export function canReconcileRole(entry: RoleHistoryEntry) {
  return entry.record.role_oid > 0 && (entry.record.status === "provision_intent" || entry.record.status === "cleanup_intent");
}

export function canCleanupRole(entry: RoleHistoryEntry) {
  return entry.record.role_oid > 0 && entry.record.status === "provisioned";
}

export function canSubmitRoleDecision(entry: RoleHistoryEntry, profileID: number, mode: RoleDecisionMode, confirmedRoleName?: string) {
  if (!Number.isSafeInteger(profileID) || profileID !== entry.record.intent.anchor.admin_profile_id) return false;
  return mode === "cleanup" ? canCleanupRole(entry) && confirmedRoleName === entry.record.intent.role_name : canReconcileRole(entry);
}

export function confirmedRoleDecision(
  value: unknown,
  expected: RoleHistoryEntry,
  targetID: number,
  mode: RoleDecisionMode = "presence",
): RoleHistoryEntry {
  const response = objectRecord(value);
  const cleanup = mode === "cleanup";
  const evidence = cleanup ? "acknowledged_remote_cleanup" : "exact_remote_identity_present";
  if (
    !response ||
    response.target_id !== targetID ||
    response.evidence !== evidence ||
    !(cleanup ? canCleanupRole(expected) : canReconcileRole(expected))
  )
    throw new Error("Unconfirmed Postgres role decision.");
  const confirmed = roleHistoryPage(
    { target_id: targetID, entries: [response.entry], has_more: false, next_after_resource_id: "" },
    targetID,
  ).entries[0]!;
  const snapshot = roleHistoryPage({ target_id: targetID, entries: [expected], has_more: false, next_after_resource_id: "" }, targetID)
    .entries[0]!;
  const want = {
    ...snapshot,
    record: { ...snapshot.record, status: cleanup ? "cleaned" : "provisioned", generation: confirmed.record.generation },
  };
  if (confirmed.record.generation === snapshot.record.generation || JSON.stringify(confirmed) !== JSON.stringify(want))
    throw new Error("Postgres role decision identity changed.");
  return confirmed;
}
