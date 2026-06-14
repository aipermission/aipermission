import { RefreshCcw, TerminalSquare } from "lucide-react";
import { Button } from "../ui/button";

export function NoLiveSession({ target, lastSession, onNewSession, theme = "dark" }) {
  const closedAt = lastSession?.closed_at || lastSession?.updated_at || lastSession?.created_at;
  const light = theme === "light";
  return (
    <div className={`grid h-full min-h-0 place-items-center p-6 ${light ? "text-stone-700" : "text-stone-200"}`}>
      <div className="grid max-w-md gap-4 text-center">
        <div className={`mx-auto flex h-12 w-12 items-center justify-center rounded-full border ${light ? "border-stone-200 bg-stone-100" : "border-stone-600 bg-stone-800"}`}>
          <TerminalSquare className={`h-6 w-6 ${light ? "text-stone-600" : "text-stone-300"}`} />
        </div>
        <div className="grid gap-2">
          <h3 className={`text-base font-semibold ${light ? "text-stone-950" : "text-white"}`}>No active shell session</h3>
          <p className={`text-sm leading-6 ${light ? "text-stone-600" : "text-stone-400"}`}>
            {lastSession
              ? `The last ${target.name} session is ${lastSession.status || "closed"} and cannot accept input anymore.`
              : `Start a shell session before sending commands to ${target.name}.`}
          </p>
          {closedAt ? <p className="text-xs text-stone-500">Last session: {formatSessionTime(closedAt)}</p> : null}
        </div>
        <Button type="button" className="mx-auto" onClick={onNewSession}>
          <RefreshCcw className="h-4 w-4" />
          New Session
        </Button>
      </div>
    </div>
  );
}

function formatSessionTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}
