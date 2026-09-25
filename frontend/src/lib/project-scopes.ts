import { apiPut } from "./api.js";
import { tokenProjectScopes } from "./gateway-contracts/security-contracts.js";

type ProjectVisibility = { project_id: number; enabled: boolean };
type UpdateOptions = { expectedRevision?: string; signal?: AbortSignal };

export async function updateTokenProjectVisibility(
  tokenID: number,
  projects: readonly ProjectVisibility[],
  projectID: number,
  enabled: boolean,
  options: UpdateOptions = {},
) {
  const { expectedRevision, ...requestOptions } = options;
  const response = await apiPut(
    `/api/tokens/${tokenID}/project-scopes`,
    {
      enabled_project_ids: enabledProjectIDsForVisibility(projects, projectID, enabled),
      expected_revision: expectedRevision,
    },
    requestOptions,
  );
  const items = tokenProjectScopes(response);
  return { items, revision: response.revision as string };
}

export function enabledProjectIDsForVisibility(projects: readonly ProjectVisibility[], projectID: number, enabled: boolean): number[] {
  return projects
    .filter((project) => (Number(project.project_id) === Number(projectID) ? enabled : Boolean(project.enabled)))
    .map((project) => project.project_id);
}
