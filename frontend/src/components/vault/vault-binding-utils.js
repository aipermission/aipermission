export function selectedBinding(state) {
  return (
    state.data.find(
      (item) =>
        Number(item.source_project_id) === Number(state.source_project_id) &&
        Number(item.target_id) === Number(state.target_id) &&
        Number(item.profile_id) === Number(state.profile_id),
    ) || null
  );
}
