import { apiGet } from "./api";

export async function loadProjectOptions(guard, setProjects) {
  const request = guard.begin("projects");
  setProjects((current) => ({ ...current, state: "loading", error: null }));
  try {
    const data = await apiGet("/api/projects", { signal: request.signal });
    if (request.isCurrent()) setProjects({ state: "ready", data: data.items || [], error: null });
  } catch (error) {
    if (request.isCurrent()) setProjects({ state: "error", data: [], error: error.message });
  } finally {
    request.complete();
  }
}
