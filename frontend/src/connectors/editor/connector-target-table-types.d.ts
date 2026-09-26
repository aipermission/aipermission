import type { ComponentType } from "react";
import type { InventoryProfile, InventoryTarget } from "../../lib/gateway-contracts/connector-inventory-contract";
import type { CredentialResource, GatewayTarget } from "../../lib/gateway-contracts/core-resource-contracts";
import type { ProjectOptionsState } from "../../lib/load-project-options";
import type { ConnectorCatalogState, ConnectorInventoryState } from "./use-connector-inventory";
import type { ConnectorRecoveryOperation, ConnectorTestState } from "./use-connector-connection-tests";

export type TargetRowActionsProps = {
  target: InventoryTarget;
  profile: InventoryProfile | null;
  onOperation: (_operation: ConnectorRecoveryOperation) => boolean | void;
  onUnderConstruction: (_label: string) => void;
};
type TargetContext = {
  target: InventoryTarget; profile: InventoryProfile | null; runtime?: GatewayTarget; credentials?: CredentialResource[];
};
export type TargetTableTemplate = {
  model: {
    targetEndpoint?: (_context: TargetContext) => string;
    credentialHint?: (_context: TargetContext) => string | null;
    canEdit?: (_context: Pick<TargetContext, "target" | "profile">) => boolean;
    canDelete?: (_context: Pick<TargetContext, "target">) => boolean;
  };
  RowActions?: ComponentType<TargetRowActionsProps> | null;
};
export type ConnectorTargetsTableProps = {
  targets: ConnectorInventoryState;
  projects: ProjectOptionsState["data"];
  search: string;
  collapsedProjects: Record<string, boolean>;
  onSearch: (_value: string) => void;
  onToggleProject: (_projectID: number) => void;
  catalog: ConnectorCatalogState;
  unifiedTargets: GatewayTarget[];
  credentials: CredentialResource[];
  profileSelections: Record<string, string>;
  tests: Record<string, ConnectorTestState>;
  onSelectProfile: (_target: InventoryTarget, _profileID: string) => void;
  onTestConnector: (_target: InventoryTarget, _profile: InventoryProfile | null) => unknown;
  onOperation: TargetRowActionsProps["onOperation"];
  onUnderConstruction: TargetRowActionsProps["onUnderConstruction"];
  onEdit: (_target: InventoryTarget, _profile: InventoryProfile | null) => unknown;
  onDelete: (_target: InventoryTarget) => void;
  resolveTemplate: (_kind: string) => TargetTableTemplate | null;
};
