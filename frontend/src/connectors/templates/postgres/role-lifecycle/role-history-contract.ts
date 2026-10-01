import { objectRecord } from "../../../../lib/api-types";
import { journalDecimal, journalHex, journalIdentifier, journalInteger, roleHistoryAnchor } from "./role-history-identity";
import { roleLifecycleStatuses } from "./role-history-types";
import type { RoleHistoryEntry, RoleHistoryPage } from "./role-history-types";

const maximumResourceID = 9223372036854775807n;

export function roleHistoryPage(value: unknown, targetID: number, after = "0"): RoleHistoryPage {
  const page = objectRecord(value);
  if (
    !journalInteger(targetID) ||
    !journalDecimal(after, maximumResourceID, true) ||
    !page ||
    page.target_id !== targetID ||
    !Array.isArray(page.entries) ||
    page.entries.length > 64 ||
    typeof page.has_more !== "boolean" ||
    typeof page.next_after_resource_id !== "string"
  )
    throw new Error("Invalid Postgres role journal page.");
  const entries = page.entries.map((entry) => historyEntry(entry, targetID));
  let previous = BigInt(after);
  const operations = new Set<string>();
  for (const entry of entries) {
    const id = BigInt(entry.resource_id);
    if (id <= previous || operations.has(entry.record.intent.operation_id))
      throw new Error("Duplicate or unordered Postgres role journal.");
    previous = id;
    operations.add(entry.record.intent.operation_id);
  }
  if (
    page.has_more
      ? entries.length !== 64 || page.next_after_resource_id !== entries.at(-1)?.resource_id
      : page.next_after_resource_id !== ""
  )
    throw new Error("Invalid Postgres role journal cursor.");
  return { target_id: targetID, entries, has_more: page.has_more, next_after_resource_id: page.next_after_resource_id };
}

function historyEntry(value: unknown, targetID: number): RoleHistoryEntry {
  const entry = objectRecord(value);
  const record = objectRecord(entry?.record);
  const intent = objectRecord(record?.intent);
  const status = roleLifecycleStatuses.find((status) => status === record?.status);
  if (
    !entry ||
    !journalDecimal(entry.resource_id, maximumResourceID) ||
    !record ||
    record.version !== 1 ||
    !journalHex(record.generation, 32) ||
    !status ||
    !journalInteger(record.role_oid, 4294967295, true) ||
    !intent ||
    !journalIdentifier(intent.role_name) ||
    !journalHex(intent.operation_id, 32)
  )
    throw new Error("Invalid Postgres role journal entry.");
  const anchor = roleHistoryAnchor(intent.anchor, targetID);
  if (
    intent.role_name === anchor.successor_name ||
    record.role_oid === anchor.successor_oid ||
    (record.role_oid === 0 && status !== "provision_intent" && status !== "rolled_back")
  )
    throw new Error("Invalid Postgres role journal role binding.");
  return {
    resource_id: entry.resource_id,
    record: {
      version: 1,
      intent: { anchor, role_name: intent.role_name, operation_id: intent.operation_id },
      role_oid: record.role_oid,
      generation: record.generation,
      status,
    },
  };
}
