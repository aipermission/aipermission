export type VaultItemSummary = {
  id: number;
  name: string;
  owner_project_id: number;
  owner_project_name?: string;
  project_ids?: number[];
  provider?: string;
  environment?: string;
  description?: string;
};
type RawItem = Omit<VaultItemSummary, "project_ids"> & { project_ids?: number[] | null };

export function vaultItemListResponse(value: unknown): { items: VaultItemSummary[]; total: number } {
  if (!value || typeof value !== "object" || Array.isArray(value) || !("items" in value) || !("total" in value)) {
    throw new Error("Invalid Vault item list response.");
  }
  const items: unknown = value.items ?? [];
  if (!Array.isArray(items) || !items.every(validItem) || typeof value.total !== "number" || !Number.isSafeInteger(value.total) || value.total < 0) {
    throw new Error("Invalid Vault item list response.");
  }
  return { items: items.map(projectItem), total: value.total };
}

function validItem(value: unknown): value is RawItem {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  if (!("id" in value) || !("name" in value) || !("owner_project_id" in value)) return false;
  return positiveID(value.id) && typeof value.name === "string" && value.name.length > 0 && positiveID(value.owner_project_id) &&
    ["owner_project_name", "provider", "environment", "description"].every((key) => !(key in value) || typeof Reflect.get(value, key) === "string") &&
    (!("project_ids" in value) || value.project_ids === null || (Array.isArray(value.project_ids) && value.project_ids.every(positiveID)));
}

function projectItem(value: RawItem): VaultItemSummary {
  return {
    id: value.id,
    name: value.name,
    owner_project_id: value.owner_project_id,
    owner_project_name: value.owner_project_name,
    project_ids: value.project_ids ?? [],
    provider: value.provider,
    environment: value.environment,
    description: value.description,
  };
}

export function vaultItemSummary(value: unknown): VaultItemSummary {
  if (!validItem(value)) throw new Error("Invalid Vault item metadata response.");
  return projectItem(value);
}

function positiveID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}
