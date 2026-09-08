import { EmptySessionState } from "./empty-session-state";

export function NoLiveSession({ target, lastSession, onNewSession, theme = "dark" }) {
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

function formatSessionTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}
