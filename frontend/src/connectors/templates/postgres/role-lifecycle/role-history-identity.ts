import { objectRecord } from "../../../../lib/api-types";
import type { RoleHistoryAnchor } from "./role-history-types";

export function journalDecimal(value: unknown, maximum: bigint, allowZero = false): value is string {
  return typeof value === "string" && /^(0|[1-9][0-9]{0,19})$/.test(value) && (allowZero || value !== "0") && BigInt(value) <= maximum;
}

export function journalInteger(value: unknown, maximum = Number.MAX_SAFE_INTEGER, allowZero = false): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= (allowZero ? 0 : 1) && value <= maximum;
}

export function journalIdentifier(value: unknown): value is string {
  if (typeof value !== "string" || value.length === 0 || value.length > 63 || value.includes("\0")) return false;
  const bytes = new TextEncoder().encode(value);
  return bytes.length <= 63 && new TextDecoder("utf-8", { ignoreBOM: true }).decode(bytes) === value;
}

export function journalHex(value: unknown, length: number): value is string {
  return typeof value === "string" && value.length === length && /^[a-f0-9]+$/.test(value);
}

export function roleHistoryAnchor(value: unknown, targetID: number): RoleHistoryAnchor {
  const anchor = objectRecord(value);
  if (
    !anchor ||
    anchor.target_id !== targetID ||
    !journalInteger(anchor.admin_profile_id) ||
    !journalHex(anchor.context_digest, 64) ||
    !journalHex(anchor.target_digest, 64) ||
    !journalDecimal(anchor.cluster_id, 18446744073709551615n) ||
    !journalInteger(anchor.database_oid, 4294967295) ||
    !journalIdentifier(anchor.database_name) ||
    !journalInteger(anchor.successor_oid, 4294967295) ||
    !journalIdentifier(anchor.successor_name)
  )
    throw new Error("Invalid Postgres role journal identity.");
  return {
    target_id: targetID,
    admin_profile_id: anchor.admin_profile_id,
    context_digest: anchor.context_digest,
    target_digest: anchor.target_digest,
    cluster_id: anchor.cluster_id,
    database_oid: anchor.database_oid,
    database_name: anchor.database_name,
    successor_oid: anchor.successor_oid,
    successor_name: anchor.successor_name,
  };
}
