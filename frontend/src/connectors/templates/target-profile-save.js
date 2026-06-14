import { apiPost, apiPut } from "../../lib/api";

export async function createTargetWithProfile({ targetPayload, profilePayload }) {
  const target = await apiPost("/api/connector-targets/with-profile", {
    target: targetPayload,
    profile: profilePayload,
  });
  return { target, profile: target.profiles?.[0] || null };
}

export async function updateTargetWithProfile({ targetID, targetPayload, profileID, profilePayload }) {
  if (!targetID || !profileID) throw new Error("Connector target profile is not loaded.");
  const target = await apiPut(`/api/connector-targets/${targetID}/with-profile/${profileID}`, {
    target: targetPayload,
    profile: profilePayload,
  });
  return { target, profile: target.profiles?.[0] || null };
}
