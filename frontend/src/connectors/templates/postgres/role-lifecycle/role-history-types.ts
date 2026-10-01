export const roleLifecycleStatuses = ["provision_intent", "provisioned", "cleanup_intent", "cleaned", "rolled_back"] as const;
export type RoleLifecycleStatus = (typeof roleLifecycleStatuses)[number];

export type RoleHistoryAnchor = {
  target_id: number;
  context_digest: string;
  target_digest: string;
  admin_profile_id: number;
  cluster_id: string;
  database_oid: number;
  database_name: string;
  successor_oid: number;
  successor_name: string;
};
export type RoleHistoryEntry = {
  resource_id: string;
  record: {
    version: 1;
    intent: { anchor: RoleHistoryAnchor; role_name: string; operation_id: string };
    role_oid: number;
    generation: string;
    status: RoleLifecycleStatus;
  };
};
export type RoleHistoryPage = {
  target_id: number;
  entries: RoleHistoryEntry[];
  has_more: boolean;
  next_after_resource_id: string;
};
