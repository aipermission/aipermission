import { apiDelete, apiPost, apiPut } from "../../lib/api";

export async function createTargetWithProfile({ targetPayload, profilePayload }) {
  const target = await apiPost("/api/connector-targets", targetPayload);
  try {
    const profile = await apiPost(`/api/connector-targets/${target.id}/profiles`, profilePayload);
    return { target, profile };
  } catch (error) {
    await apiDelete(`/api/connector-targets/${target.id}`).catch(() => {});
    throw error;
  }
}

export async function updateTargetWithProfile({ targetID, targetPayload, profileID, profilePayload, previousTarget }) {
  if (!targetID || !profileID) throw new Error("Connector target profile is not loaded.");
  const target = await apiPut(`/api/connector-targets/${targetID}`, targetPayload);
  try {
    const profile = await apiPut(`/api/connector-targets/${targetID}/profiles/${profileID}`, profilePayload);
    return { target, profile };
  } catch (error) {
    if (previousTarget) {
      await apiPut(`/api/connector-targets/${targetID}`, {
        name: previousTarget.name,
        config: previousTarget.config,
      }).catch(() => {});
    }
    throw error;
  }
}
