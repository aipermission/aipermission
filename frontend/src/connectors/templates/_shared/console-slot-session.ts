export function structuredConsoleSlotSession(session: unknown): { active: boolean; startedAt: string } | null {
  if (!session || typeof session !== "object" || Array.isArray(session) || !("active" in session) || !("startedAt" in session)) return null;
  if (typeof session.active !== "boolean" || typeof session.startedAt !== "string") return null;
  return { active: session.active, startedAt: session.startedAt };
}
