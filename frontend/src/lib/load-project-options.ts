import type { Dispatch, SetStateAction } from "react";
import { apiGet } from "./api";
import { errorMessage } from "./errors";
import type { useRequestGuard } from "./request-guard";
import { projectListResponse, type ProjectSummary } from "./gateway-contracts/project-list-contract";

export type ProjectOption = ProjectSummary;
export type ProjectOptionsState = { state: string; data: ProjectOption[]; error: string | null };

export async function loadProjectOptions(
  guard: ReturnType<typeof useRequestGuard>,
  setProjects: Dispatch<SetStateAction<ProjectOptionsState>>,
): Promise<void> {
  const request = guard.begin("projects");
  setProjects((current) => ({ ...current, state: "loading", error: null }));
  try {
    const data = await apiGet("/api/projects", { signal: request.signal });
    if (request.isCurrent()) setProjects({ state: "ready", data: projectListResponse(data), error: null });
  } catch (error) {
    if (request.isCurrent()) setProjects({ state: "error", data: [], error: errorMessage(error, "Could not load projects.") });
  } finally {
    request.complete();
  }
}
