import type { components } from "../../../types/generated-openapi";

type Target = components["schemas"]["ConnectorTarget"];
type Profile = components["schemas"]["ConnectorCredentialProfile"];
export type VaultBindingTarget = Pick<Target, "id" | "name" | "connector_kind"> & {
  profiles: Pick<Profile, "id" | "label" | "vault_session_supported">[];
};
export type VaultManagedBinding = {
  id: number;
  vault_item_id: number;
  source_project_id: number;
  target_id: number;
  profile_id: number;
  binding_revision: number;
  replace_existing: boolean;
  target_name: string;
  profile_label: string;
  source_project_name: string;
  connector_kind: string;
};

export function vaultBindingsResponse(value: unknown, itemID: number): VaultManagedBinding[] {
  return items(value).map((entry) => {
    const data = object(entry);
    if (data.vault_item_id !== itemID || typeof data.replace_existing !== "boolean") throw new Error("Invalid Vault binding identity.");
    return {
      id: id(data.id),
      vault_item_id: itemID,
      source_project_id: id(data.source_project_id),
      target_id: id(data.target_id),
      profile_id: id(data.profile_id),
      binding_revision: id(data.binding_revision),
      replace_existing: data.replace_existing,
      target_name: text(data.target_name),
      profile_label: text(data.profile_label),
      source_project_name: text(data.source_project_name),
      connector_kind: text(data.connector_kind),
    };
  });
}

export function vaultBindingTargetsResponse(value: unknown): VaultBindingTarget[] {
  return items(value).map((entry) => {
    const target = object(entry);
    const profiles: unknown = target.profiles ?? [];
    if (!Array.isArray(profiles)) throw new Error("Invalid Vault binding target profiles.");
    return {
      id: id(target.id),
      name: text(target.name),
      connector_kind: text(target.connector_kind),
      profiles: profiles.map((entry) => {
        const profile = object(entry);
        if (typeof profile.vault_session_supported !== "boolean") throw new Error("Invalid Vault session capability.");
        return { id: id(profile.id), label: text(profile.label), vault_session_supported: profile.vault_session_supported };
      }),
    };
  });
}

function items(value: unknown): unknown[] {
  const data = object(value);
  if (!("items" in data)) throw new Error("Invalid Vault binding collection.");
  const result: unknown = data.items ?? [];
  if (!Array.isArray(result)) throw new Error("Invalid Vault binding collection.");
  return result;
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid Vault binding response.");
  return value as Record<string, unknown>;
}
function id(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value <= 0) throw new Error("Invalid Vault binding ID.");
  return value;
}
function text(value: unknown): string {
  if (typeof value !== "string") throw new Error("Invalid Vault binding label.");
  return value;
}
