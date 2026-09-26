import type { VaultDefaultSelection, VaultSessionSelection } from "./gateway-contracts/vault-session-options-contract.ts";

type DefaultBinding = { id: number; vault_item_id: number; source_project_id: number };

export function selectionFromDefaultBinding(binding: VaultDefaultSelection): VaultSessionSelection {
  return {
    item_id: binding.vault_item_id,
    source_project_id: binding.source_project_id,
    replace_existing: binding.replace_existing,
    binding_id: binding.id,
    binding_revision: binding.binding_revision,
  };
}

export function preferredDefaultBindings<Binding extends DefaultBinding>(
  defaults: Binding[] | null | undefined,
  preferredProjectID: string | number | null | undefined,
): Binding[] {
  const preferred = Number(preferredProjectID);
  const sorted = [...(defaults || [])].sort((left, right) => {
    const leftPreferred = Number(left.source_project_id) === preferred ? 0 : 1;
    const rightPreferred = Number(right.source_project_id) === preferred ? 0 : 1;
    return (
      leftPreferred - rightPreferred ||
      Number(left.source_project_id) - Number(right.source_project_id) ||
      Number(left.id) - Number(right.id)
    );
  });
  const seen = new Set<number>();
  return sorted.filter((binding) => {
    const itemID = Number(binding.vault_item_id);
    if (seen.has(itemID)) return false;
    seen.add(itemID);
    return true;
  });
}
