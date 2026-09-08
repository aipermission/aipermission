export function connectorTargetGroups(projects, targets, search) {
  const query = search.trim().toLowerCase();
  return projects
    .map((project) => {
      const projectMatches =
        query &&
        [project.name, project.slug].some((value) =>
          String(value || "")
            .toLowerCase()
            .includes(query),
        );
      return {
        project,
        targets: targets.filter(
          (target) =>
            target.project_id === project.id &&
            (!query ||
              projectMatches ||
              [target.name, target.connector_kind, ...(target.profiles || []).map((profile) => profile.label)].some((value) =>
                String(value || "")
                  .toLowerCase()
                  .includes(query),
              )),
        ),
      };
    })
    .filter((group) => group.targets.length > 0 || (!query && group.project.target_count > 0));
}
