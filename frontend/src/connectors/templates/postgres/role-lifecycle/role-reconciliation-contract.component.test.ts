import { expect, it } from "vitest";
import { canCleanupRole, canReconcileRole, confirmedRoleDecision } from "./role-reconciliation-contract";
import { roleHistoryFixture } from "../../../../test/postgres/role-history-fixtures.test";

function receipt() {
  const expected = roleHistoryFixture();
  expected.record.status = "cleanup_intent";
  const confirmed = structuredClone(expected);
  confirmed.record.status = "provisioned";
  confirmed.record.generation = "c".repeat(32);
  return { expected, value: { target_id: 1, entry: confirmed, evidence: "exact_remote_identity_present" } };
}

it.each(["provision_intent", "cleanup_intent"] as const)("confirms exact %s presence with a fresh generation", (status) => {
  const { expected, value } = receipt();
  expected.record.status = status;
  expect(confirmedRoleDecision(value, expected, 1)).toEqual(value.entry);
  expect(expected.record.status).toBe(status);
});

it.each(["provisioned", "cleaned", "rolled_back"] as const)("does not offer a decision for %s", (status) => {
  const { expected, value } = receipt();
  expected.record.status = status;
  expect(canReconcileRole(expected)).toBe(false);
  expect(() => confirmedRoleDecision(value, expected, 1)).toThrow();
});

it("does not offer a decision without a bound role OID", () => {
  const { expected, value } = receipt();
  expected.record.role_oid = 0;
  expect(canReconcileRole(expected)).toBe(false);
  expect(() => confirmedRoleDecision(value, expected, 1)).toThrow();
});

it.each([
  "target",
  "evidence",
  "resource",
  "oid",
  "generation",
  "status",
  "operation",
  "name",
  "context",
  "admin",
  "cluster",
  "databaseOID",
  "databaseName",
  "successorOID",
  "successorName",
])("rejects a receipt with changed %s evidence", (field) => {
  const { expected, value } = receipt();
  const record = value.entry.record;
  const anchor = record.intent.anchor;
  switch (field) {
    case "target":
      value.target_id = 2;
      break;
    case "evidence":
      value.evidence = "absent";
      break;
    case "resource":
      value.entry.resource_id = "2";
      break;
    case "oid":
      record.role_oid++;
      break;
    case "generation":
      record.generation = expected.record.generation;
      break;
    case "status":
      record.status = "cleaned";
      break;
    case "operation":
      record.intent.operation_id = "d".repeat(32);
      break;
    case "name":
      record.intent.role_name = "Another role";
      break;
    case "context":
      anchor.context_digest = "d".repeat(64);
      break;
    case "admin":
      anchor.admin_profile_id++;
      break;
    case "cluster":
      anchor.cluster_id = "1";
      break;
    case "databaseOID":
      anchor.database_oid++;
      break;
    case "databaseName":
      anchor.database_name = "Another database";
      break;
    case "successorOID":
      anchor.successor_oid++;
      break;
    case "successorName":
      anchor.successor_name = "Another admin";
      break;
  }
  expect(() => confirmedRoleDecision(value, expected, 1)).toThrow();
});

it.each([null, {}, { target_id: 1, evidence: "exact_remote_identity_present", entry: {} }])("rejects malformed receipt %j", (value) => {
  expect(() => confirmedRoleDecision(value, receipt().expected, 1)).toThrow();
});

it("accepts only acknowledged cleanup of the exact provisioned identity", () => {
  const { expected, value } = receipt();
  expected.record.status = "provisioned";
  value.entry.record.status = "cleaned";
  value.evidence = "acknowledged_remote_cleanup";
  expect(canCleanupRole(expected)).toBe(true);
  expect(confirmedRoleDecision(value, expected, 1, "cleanup")).toEqual(value.entry);
  expect(() => confirmedRoleDecision(value, expected, 1)).toThrow();
  for (const status of ["provision_intent", "cleanup_intent", "cleaned", "rolled_back"] as const) {
    const wrong = structuredClone(expected);
    wrong.record.status = status;
    expect(canCleanupRole(wrong)).toBe(false);
    expect(() => confirmedRoleDecision(value, wrong, 1, "cleanup")).toThrow();
  }
  const changed = structuredClone(value);
  changed.entry.record.role_oid++;
  expect(() => confirmedRoleDecision(changed, expected, 1, "cleanup")).toThrow();
  changed.entry.record.role_oid--;
  changed.evidence = "exact_remote_identity_present";
  expect(() => confirmedRoleDecision(changed, expected, 1, "cleanup")).toThrow();
});
