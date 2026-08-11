import { Clock, RefreshCcw } from "lucide-react";
import { Button } from "../ui/button";

export function ConsoleRecoveryPanel({ request, now, theme, action, onRestart }) {
  const ageMs = Math.max(0, now - parseTimestamp(request.created_at));
  const showRecoveryHint = ageMs >= 20000;
  const panelClass =
    theme === "light" ? "border-amber-300 bg-amber-50 text-amber-950" : "border-amber-900/70 bg-amber-950/40 text-amber-50";
  const mutedClass = theme === "light" ? "text-amber-900/80" : "text-amber-100/80";
  const commandPreview = firstLine(request.command || request.input?.command || request.action_name || "connector action");

  return (
    <div className={`flex min-h-9 items-center gap-3 border-b px-4 py-2 text-xs ${panelClass}`}>
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <Clock className="h-3.5 w-3.5 shrink-0" />
        <span className="shrink-0 font-semibold">{runningRequestLabel(request)}</span>
        <span className={`shrink-0 rounded-full px-2 py-0.5 ${theme === "light" ? "bg-stone-200 text-stone-700" : "bg-stone-800 text-stone-200"}`}>
          {formatDuration(ageMs)}
        </span>
        {request.token_name ? (
          <span className={`shrink-0 rounded-full px-2 py-0.5 ${theme === "light" ? "bg-emerald-100 text-emerald-800" : "bg-emerald-950 text-emerald-100"}`}>
            {request.token_name}
          </span>
        ) : null}
        <span className={`min-w-0 truncate font-mono ${mutedClass}`}>{commandPreview}</span>
        {showRecoveryHint ? <span className="shrink-0 font-medium">Looks stuck? Restart opens a fresh console session.</span> : null}
      </div>
      {action.error ? <span className="max-w-80 truncate text-red-300">{action.error}</span> : null}
      <Button
        type="button"
        variant="outline"
        className={`h-7 shrink-0 px-2 text-xs ${
          theme === "light"
            ? "border-amber-400 bg-amber-100 text-amber-950 hover:bg-amber-200"
            : "border-amber-700 bg-amber-950/70 text-amber-50 hover:bg-amber-900/70"
        }`}
        onClick={onRestart}
        disabled={action.state === "running"}
        title="Close the gateway-owned persistent console session and let the next command open a fresh one"
      >
        <RefreshCcw className="h-3.5 w-3.5" />
        {action.state === "running" ? "Restarting..." : "Restart"}
      </Button>
    </div>
  );
}

function runningRequestLabel(request) {
  if (request?.action_name) return "Connector action running";
  if (request?.source === "manual") return "Manual command running";
  if (request?.source === "mcp") return "AI command running";
  return "Command running";
}

function firstLine(value) {
  const line = String(value || "").split(/\r?\n/, 1)[0].trim();
  return line.length <= 90 ? line : `${line.slice(0, 87)}...`;
}

function parseTimestamp(value) {
  const parsed = Date.parse(value || "");
  return Number.isNaN(parsed) ? Date.now() : parsed;
}

function formatDuration(ms) {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m ${seconds}s`;
  return `${seconds}s`;
}
