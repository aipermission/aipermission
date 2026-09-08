import { Database, RefreshCcw, XCircle } from "lucide-react";
import { EmptySessionState } from "../../../components/console/empty-session-state";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";

export function SQLNoSessionPlaceholder({ config, target, theme, onNewSession }) {
  return (
    <EmptySessionState
      title={`No active ${config.label} session`}
      description={`Start a ${config.label} session before running SQL against ${target.name}.`}
      onStart={() => onNewSession?.()}
      theme={theme}
    />
  );
}

export function SQLEndpointFooter({ config, target, borderClass, mutedClass }) {
  return (
    <div className={`border-t px-4 py-2 text-xs ${borderClass} ${mutedClass}`}>
      <span className="inline-flex min-w-0 items-center gap-2">
        <Database className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{config.targetEndpoint(target)}</span>
      </span>
    </div>
  );
}

export function SQLConnectorToolbarActions({ label, theme, structuredSession, onNewStructuredSession, onEndStructuredSession }) {
  const buttonClass = `h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`;
  const active = Boolean(structuredSession?.active);
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        className={buttonClass}
        onClick={onNewStructuredSession}
        disabled={active}
        title={`Start a fresh ${label} activity session`}
      >
        <RefreshCcw className="h-3.5 w-3.5" />
        New Session
      </Button>
      <Button
        type="button"
        variant="ghost"
        className={buttonClass}
        onClick={onEndStructuredSession}
        disabled={!active}
        title={`End the current ${label} activity session`}
      >
        <XCircle className="h-3.5 w-3.5" />
        End Session
      </Button>
    </>
  );
}

export function ResultViewToggle({ checked, onChange, theme }) {
  const light = theme === "light";
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      className={`inline-flex shrink-0 items-center gap-2 rounded-full border px-2 py-1 text-xs font-semibold transition ${checked ? "border-emerald-600 bg-emerald-950 text-emerald-50" : light ? "border-stone-300 bg-white text-stone-600 hover:bg-stone-100" : "border-stone-700 bg-stone-900 text-stone-300 hover:bg-stone-800"}`}
      onClick={() => onChange(!checked)}
    >
      <span>Result View</span>
      <span className={`relative h-4 w-7 rounded-full transition ${checked ? "bg-emerald-500" : light ? "bg-stone-300" : "bg-stone-700"}`}>
        <span className={`absolute top-0.5 h-3 w-3 rounded-full bg-white transition ${checked ? "left-3.5" : "left-0.5"}`} />
      </span>
    </button>
  );
}

export function ActivityStatusBadge({ status }) {
  const tone =
    status === "completed"
      ? "good"
      : ["failed", "error", "stale", "outcome_unknown"].includes(status)
        ? "bad"
        : ["approval_pending", "running"].includes(status)
          ? "warn"
          : "neutral";
  return <Badge tone={tone}>{status}</Badge>;
}

export function formatConnectorTime(value) {
  if (!value) return "-";
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(value));
}
