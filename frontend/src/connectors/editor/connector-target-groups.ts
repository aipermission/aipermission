type Project = { id: number; name: string; slug: string; target_count: number };
type Target = { project_id: number; name: string; connector_kind: string; profiles?: { label: string }[] };

export function connectorTargetGroups<P extends Project, T extends Target>(projects: P[], targets: T[], search: string) {
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
