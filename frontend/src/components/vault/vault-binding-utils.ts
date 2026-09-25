type VaultBinding = {
  source_project_id: number;
  target_id: number;
  profile_id: number;
  binding_revision?: number;
};

type BindingSelection = {
  data: VaultBinding[];
  source_project_id: string | number;
  target_id: string | number;
  profile_id: string | number;
};

export function selectedBinding(state: BindingSelection): VaultBinding | null {
  return (
    state.data.find(
      (item) =>
        Number(item.source_project_id) === Number(state.source_project_id) &&
        Number(item.target_id) === Number(state.target_id) &&
        Number(item.profile_id) === Number(state.profile_id),
    ) || null
  );
}
