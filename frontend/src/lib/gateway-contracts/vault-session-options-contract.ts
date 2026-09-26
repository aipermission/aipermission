import { vaultItemListResponse } from "./vault-item-list-contract.ts";
import type { VaultItemSummary } from "./vault-item-list-contract.ts";

export type VaultDefaultSelection = {
  id: number;
  vault_item_id: number;
  vault_item_name: string;
  source_project_id: number;
  source_project_name: string;
  replace_existing: boolean;
  binding_revision: number;
};
export type VaultSelectionProject = { id: number; name: string };
export type VaultSessionOptions = {
  supported: boolean;
  target_project_id?: number;
  items: VaultItemSummary[];
  defaults: VaultDefaultSelection[];
  projects: VaultSelectionProject[];
};
export type VaultSessionSelection = {
  item_id: number;
  source_project_id: number;
  replace_existing: boolean;
  binding_id?: number;
  binding_revision?: number;
};

export function vaultSessionOptionsResponse(value: unknown): VaultSessionOptions {
  const data = record(value);
  if (!data || typeof data.supported !== "boolean") throw invalidResponse();
  if (data.supported === false) return { supported: false, items: [], defaults: [], projects: [] };
  if (!positiveID(data.target_project_id)) throw invalidResponse();
  const defaults: unknown = data.defaults ?? [];
  const projects: unknown = data.projects ?? [];
  if (!Array.isArray(defaults) || !defaults.every(validDefault) || !Array.isArray(projects) || !projects.every(validProject)) throw invalidResponse();
  const list = vaultItemListResponse({ items: data.items, total: data.total });
  return {
    supported: true,
    target_project_id: data.target_project_id,
    items: list.items,
    defaults: defaults.map(({ id, vault_item_id, vault_item_name, source_project_id, source_project_name, replace_existing, binding_revision }) => ({
      id, vault_item_id, vault_item_name, source_project_id, source_project_name, replace_existing, binding_revision,
    })),
    projects: projects.map(({ id, name }) => ({ id, name })),
  };
}

function validDefault(value: unknown): value is VaultDefaultSelection {
  const data = record(value);
  return !!data && positiveID(data.id) && positiveID(data.vault_item_id) && positiveID(data.source_project_id) && positiveID(data.binding_revision) &&
    typeof data.vault_item_name === "string" && typeof data.source_project_name === "string" && typeof data.replace_existing === "boolean";
}

function validProject(value: unknown): value is VaultSelectionProject {
  const data = record(value);
  return !!data && positiveID(data.id) && typeof data.name === "string";
}

function record(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function positiveID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

function invalidResponse() {
  return new Error("Invalid Vault session options response.");
}
