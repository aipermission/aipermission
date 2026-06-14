export const emptySession = {
  transcript: "",
  status: "idle",
  error: null,
};

export function isUnreadMessage(message) {
  return message.direction === "ai_to_user" && !message.consumed_at;
}

export function isLiveConsoleSession(session) {
  return session?.status === "connecting" || session?.status === "connected";
}

export function latestSessionForRuntimeProfile(sessions, runtimeProfileID) {
  return sessions.find((session) => Number(session.runtime_profile_id) === Number(runtimeProfileID)) || null;
}
