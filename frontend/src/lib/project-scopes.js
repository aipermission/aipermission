import { apiPut } from "./api.js";

export async function updateTokenProjectVisibility(tokenID, projects, projectID, enabled) {
  return apiPut(`/api/tokens/${tokenID}/project-scopes`, {
    enabled_project_ids: enabledProjectIDsForVisibility(projects, projectID, enabled),
  });
}

export function enabledProjectIDsForVisibility(projects, projectID, enabled) {
  return projects
    .filter((project) => Number(project.project_id) === Number(projectID) ? enabled : Boolean(project.enabled))
    .map((project) => project.project_id);
}
