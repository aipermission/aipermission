import { EmptySessionState } from "./empty-session-state";

type LastSession = { status?: string; closed_at?: string; updated_at?: string; created_at?: string };

export function NoLiveSession({
  target,
  lastSession,
  onNewSession,
  theme = "dark",
}: {
  target: { name: string };
  lastSession?: LastSession | null;
  onNewSession: () => void;
  theme?: "dark" | "light";
}) {
  const closedAt = lastSession?.closed_at || lastSession?.updated_at || lastSession?.created_at;
  return (
    <EmptySessionState
      title="No active shell session"
      description={
        lastSession
          ? `The last ${target.name} session is ${lastSession.status || "closed"} and cannot accept input anymore.`
          : `Start a shell session before sending commands to ${target.name}.`
      }
      detail={closedAt ? `Last session: ${formatSessionTime(closedAt)}` : ""}
      onStart={onNewSession}
      theme={theme}
    />
  );
}

function formatSessionTime(value: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}
