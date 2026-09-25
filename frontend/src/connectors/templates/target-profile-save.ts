import { apiPost, apiPut } from "../../lib/api.js";

type TargetProfilePayload = {
  projectID?: string | number | null;
  targetPayload: Record<string, unknown>;
  profilePayload: Record<string, unknown>;
};

export async function createTargetWithProfile({ projectID, targetPayload, profilePayload }: TargetProfilePayload) {
  const target = await apiPost("/api/connector-targets/with-profile", {
    target: { ...targetPayload, project_id: Number(projectID) || 0 },
    profile: profilePayload,
  });
  return { target, profile: target.profiles?.[0] || null };
}

export async function updateTargetWithProfile({
  targetID,
  projectID,
  targetPayload,
  profileID,
  profilePayload,
}: TargetProfilePayload & { targetID: string | number; profileID: string | number }) {
  if (!targetID || !profileID) throw new Error("Connector target profile is not loaded.");
  const target = await apiPut(`/api/connector-targets/${targetID}/with-profile/${profileID}`, {
    target: { ...targetPayload, project_id: Number(projectID) || 0 },
    profile: profilePayload,
  });
  return { target, profile: target.profiles?.[0] || null };
}
