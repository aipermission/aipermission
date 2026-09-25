import { expect, it, vi } from "vitest";
import { apiPost, apiPut } from "../../lib/api.js";
import { createTargetWithProfile, updateTargetWithProfile } from "./target-profile-save";

vi.mock("../../lib/api.js", () => ({ apiPost: vi.fn(), apiPut: vi.fn() }));

it("creates a target and first profile with a numeric project scope", async () => {
  vi.mocked(apiPost).mockResolvedValueOnce({ id: 7, profiles: [{ id: 8 }] });
  const result = await createTargetWithProfile({
    projectID: "4",
    targetPayload: { name: "example" },
    profilePayload: { label: "default" },
  });
  expect(vi.mocked(apiPost)).toHaveBeenCalledWith("/api/connector-targets/with-profile", {
    target: { name: "example", project_id: 4 },
    profile: { label: "default" },
  });
  expect(result.profile).toEqual({ id: 8 });
});

it("rejects a profile update without both resource identifiers", async () => {
  await expect(updateTargetWithProfile({ targetID: 7, profileID: 0, targetPayload: {}, profilePayload: {} })).rejects.toThrow(
    "Connector target profile is not loaded.",
  );
  expect(vi.mocked(apiPut)).not.toHaveBeenCalled();
});
