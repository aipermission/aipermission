import type { Dispatch, SetStateAction } from "react";
import { apiGet } from "./api";
import { errorMessage } from "./errors";
import type { useRequestGuard } from "./request-guard";

export type ProjectOption = { id: number; slug: string; name?: string; [field: string]: unknown };
export type ProjectOptionsState = { state: string; data: ProjectOption[]; error: string | null };

export async function loadProjectOptions(
  guard: ReturnType<typeof useRequestGuard>,
  setProjects: Dispatch<SetStateAction<ProjectOptionsState>>,
): Promise<void> {
  const request = guard.begin("projects");
  setProjects((current) => ({ ...current, state: "loading", error: null }));
  try {
    const data = await apiGet("/api/projects", { signal: request.signal });
    if (request.isCurrent()) setProjects({ state: "ready", data: data.items || [], error: null });
  } catch (error) {
    if (request.isCurrent()) setProjects({ state: "error", data: [], error: errorMessage(error, "Could not load projects.") });
  } finally {
    request.complete();
  }
}
