export const emptySession = {
  transcript: "",
  status: "idle",
  error: null,
};

export function isUnreadMessage(message: { direction: string; consumed_at?: string | null }): boolean {
  return message.direction === "ai_to_user" && !message.consumed_at;
}

export function isLiveConsoleSession(session: { status?: string } | null | undefined): boolean {
  return session?.status === "connecting" || session?.status === "connected";
}

export function latestSessionForRuntime<Session extends { runtime_id?: string | number }>(
  sessions: Session[],
  runtimeID: string | number,
): Session | null {
  return sessions.find((session) => Number(session.runtime_id) === Number(runtimeID)) || null;
}
