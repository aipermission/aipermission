import { describe, expect, it, vi } from "vitest";
import { apiPut } from "../lib/api";
import { enabledProjectIDsForVisibility, updateTokenProjectVisibility } from "../lib/project-scopes";

vi.mock("../lib/api", () => ({ apiPut: vi.fn() }));

describe("updateTokenProjectVisibility", () => {
  const projects = [{ project_id: 3, enabled: false }];

  it("updates one project without changing the others", () => {
    const scopes = [
      { project_id: 1, enabled: true },
      { project_id: 2, enabled: false },
      { project_id: 3, enabled: true },
    ];
    expect(enabledProjectIDsForVisibility(scopes, 2, true)).toEqual([1, 2, 3]);
    expect(enabledProjectIDsForVisibility(scopes, 1, false)).toEqual([3]);
  });

  it("returns a validated scope snapshot", async () => {
    apiPut.mockResolvedValueOnce({
      items: [{ project_id: 3, project_name: "My Project", project_slug: "my-project", enabled: true }],
      revision: "scope-2",
    });

    await expect(updateTokenProjectVisibility(7, projects, 3, true, { expectedRevision: "scope-1" })).resolves.toMatchObject({
      items: [{ project_id: 3, enabled: true }],
      revision: "scope-2",
    });
    expect(apiPut).toHaveBeenCalledWith("/api/tokens/7/project-scopes", { enabled_project_ids: [3], expected_revision: "scope-1" }, {});
  });

  it("rejects a malformed response without committing a scope change", async () => {
    apiPut.mockResolvedValueOnce({ items: [{ project_id: 3, enabled: true }], revision: "scope-2" });

    await expect(updateTokenProjectVisibility(7, projects, 3, true)).rejects.toThrow("Invalid project scope response from gateway.");
  });
});
