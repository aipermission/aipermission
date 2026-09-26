import type { components } from "../../../types/generated-openapi";
import { connectorActionItems, type ConnectorPermissionAction } from "./connector-catalog-contract";

export type InventoryProfile = Omit<components["schemas"]["ConnectorCredentialProfile"], "actions"> & {
  actions?: ConnectorPermissionAction[];
};
export type InventoryTarget = Omit<components["schemas"]["ConnectorTarget"], "profiles"> & { profiles?: InventoryProfile[] };

function invalid() {
  return new Error("Invalid connector inventory from gateway.");
}
function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  return value as Record<string, unknown>;
}
function positiveID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

export function connectorInventoryResponse(value: unknown): InventoryTarget[] {
  const response = record(value);
  if (!Array.isArray(response.items)) throw invalid();
  return response.items.map((value) => {
    const target = record(value);
    if (!positiveID(target.id) || !positiveID(target.project_id)) throw invalid();
    for (const key of ["connector_kind", "name", "project_name", "project_slug", "status", "created_at", "updated_at"]) {
      if (typeof target[key] !== "string") throw invalid();
    }
    if (target.ref !== undefined && typeof target.ref !== "string") throw invalid();
    if (target.config !== undefined) record(target.config);
    if (target.profiles !== undefined && !Array.isArray(target.profiles)) throw invalid();
    const profiles = Array.isArray(target.profiles)
      ? target.profiles.map((profile) => inventoryProfileResponse(profile, target.id, target.connector_kind))
      : undefined;
    if (profiles && new Set(profiles.map((profile) => profile.id)).size !== profiles.length) throw invalid();
    return { ...target, profiles } as InventoryTarget;
  });
}

function inventoryProfileResponse(value: unknown, targetID: unknown, connectorKind: unknown): InventoryProfile {
  const profile = record(value);
  if (
    !positiveID(profile.id) ||
    profile.target_id !== targetID ||
    profile.connector_kind !== connectorKind ||
    typeof profile.vault_session_supported !== "boolean"
  )
    throw invalid();
  for (const key of ["connector_kind", "kind", "label", "created_at", "updated_at"]) if (typeof profile[key] !== "string") throw invalid();
  for (const key of ["ref", "risk_label"]) if (profile[key] !== undefined && typeof profile[key] !== "string") throw invalid();
  for (const key of ["runtime_id", "transfer_runtime_id"]) if (profile[key] !== undefined && !positiveID(profile[key])) throw invalid();
  if (profile.public !== undefined) record(profile.public);
  const actions = profile.actions === undefined ? undefined : connectorActionItems(profile.actions);
  return { ...profile, actions } as InventoryProfile;
}
