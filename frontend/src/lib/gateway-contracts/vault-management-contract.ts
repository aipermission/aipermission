import { vaultItemSummary, type VaultItemSummary } from "./vault-item-list-contract.ts";

export type VaultUsageNote = { location: string; notes: string };
export type VaultManagedItem = VaultItemSummary & {
  source: string;
  secret_type: string;
  value_version: number;
  metadata_revision: number;
  generator_kind?: string;
  expires_at?: string;
  expiry_warning_days?: number;
  last_used_at?: string;
  tags: string[];
  usage_notes: VaultUsageNote[];
};
export type VaultActionState = { state: string; message: string; error: string | null };

export function vaultRevealedValueResponse(value: unknown): string {
  const data = object(value);
  if (typeof data.value !== "string") throw new Error("Invalid Vault value response.");
  return data.value;
}

export function vaultGeneratedPreviewResponse(value: unknown): { value: string; preview_token: string } {
  const data = object(value);
  return { value: vaultRevealedValueResponse(value), preview_token: requiredString(data.preview_token) };
}

export function vaultManagedItemsResponse(value: unknown): { items: VaultManagedItem[]; total: number } {
  const data = object(value);
  const items: unknown = data.items ?? [];
  if (!Array.isArray(items) || !Number.isSafeInteger(data.total) || typeof data.total !== "number" || data.total < 0) {
    throw new Error("Invalid Vault management list response.");
  }
  return { items: items.map(managedItem), total: data.total };
}

function managedItem(value: unknown): VaultManagedItem {
  const summary = vaultItemSummary(value);
  const data = object(value);
  const tags: unknown = data.tags ?? [];
  const notes: unknown = data.usage_notes ?? [];
  if (!Array.isArray(tags) || !tags.every((tag): tag is string => typeof tag === "string") || !Array.isArray(notes)) {
    throw new Error("Invalid Vault metadata arrays.");
  }
  if (
    data.expiry_warning_days !== undefined &&
    (typeof data.expiry_warning_days !== "number" || !Number.isSafeInteger(data.expiry_warning_days) || data.expiry_warning_days < 0)
  ) {
    throw new Error("Invalid Vault expiry warning days.");
  }
  return {
    ...summary,
    source: requiredString(data.source),
    secret_type: requiredString(data.secret_type),
    value_version: revision(data.value_version),
    metadata_revision: revision(data.metadata_revision),
    generator_kind: optionalString(data.generator_kind),
    expires_at: optionalString(data.expires_at),
    expiry_warning_days: data.expiry_warning_days,
    last_used_at: optionalString(data.last_used_at),
    tags,
    usage_notes: notes.map((entry) => {
      const note = object(entry);
      if (typeof note.location !== "string" || typeof note.notes !== "string") throw new Error("Invalid Vault usage note.");
      return { location: note.location, notes: note.notes };
    }),
  };
}

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid Vault management response.");
  return value as Record<string, unknown>;
}
function requiredString(value: unknown): string {
  if (typeof value !== "string" || !value) throw new Error("Invalid Vault metadata field.");
  return value;
}
function optionalString(value: unknown): string | undefined {
  if (value === undefined || typeof value === "string") return value;
  throw new Error("Invalid Vault optional metadata field.");
}
function revision(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value <= 0) throw new Error("Invalid Vault metadata revision.");
  return value;
}
