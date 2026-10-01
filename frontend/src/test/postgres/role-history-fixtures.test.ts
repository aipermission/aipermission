import type { RoleHistoryEntry, RoleHistoryPage } from "../../connectors/templates/postgres/role-lifecycle/role-history-types";

export function roleHistoryFixture(targetID = 1, id = "1"): RoleHistoryEntry {
  return {
    resource_id: id,
    record: {
      version: 1,
      intent: {
        anchor: {
          target_id: targetID,
          context_digest: "a".repeat(64),
          target_digest: "c".repeat(64),
          admin_profile_id: 2,
          cluster_id: "18446744073709551615",
          database_oid: 12,
          database_name: " Main DB ",
          successor_oid: 10,
          successor_name: " Main Admin ",
        },
        role_name: " My Role ",
        operation_id: BigInt(id).toString(16).padStart(32, "0"),
      },
      role_oid: 42,
      generation: "b".repeat(32),
      status: "provisioned",
    },
  };
}

export function roleHistoryPageFixture(targetID = 1, ids: string[] = ["1"]): RoleHistoryPage {
  return {
    target_id: targetID,
    entries: ids.map((id) => roleHistoryFixture(targetID, id)),
    has_more: false,
    next_after_resource_id: "",
  };
}

export function deferredRoleHistoryReply() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: Error) => void;
  const promise = new Promise<unknown>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
