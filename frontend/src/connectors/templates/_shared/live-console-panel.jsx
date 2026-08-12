import { LoaderCircle, RefreshCcw, TerminalSquare, XCircle } from "lucide-react";
import { Button } from "../../../components/ui/button";

export function LiveConsolePanel({
  children,
  subject,
  subjectRef,
  emptyMessage,
  selectedRuntimeTarget,
  sessionLive,
  pending,
  theme,
  mutedClass,
  borderClass,
  warning,
  onStart,
  onEnd,
}) {
  const light = theme === "light";
  const subjectLabel = subject ? `${subject[0].toUpperCase()}${subject.slice(1)}` : "";
  if (!subject) {
    return (
      <div
        className={`grid h-full min-h-0 place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${borderClass} ${mutedClass}`}
      >
        {emptyMessage}
      </div>
    );
  }
  if (sessionLive) {
    return (
      <div className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass}`}>
        <div className={`flex min-w-0 items-center justify-between gap-3 border-b px-3 py-2 ${borderClass}`}>
          <div className="min-w-0">
            <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">{subject} console</p>
            <p className="truncate font-mono text-xs">{subjectRef}</p>
          </div>
          <Button
            type="button"
            variant="outline"
            className="h-8 px-2 text-xs"
            onClick={onEnd}
            title={`Close this ${subject} console session`}
          >
            <XCircle className="h-3.5 w-3.5" />
            End
          </Button>
        </div>
        <div className="h-full min-h-0 overflow-hidden">{children}</div>
      </div>
    );
  }
  if (pending) {
    return (
      <div className={`grid h-full min-h-0 place-items-center rounded-lg border border-dashed p-8 text-center ${borderClass}`}>
        <div className="grid max-w-md gap-3">
          <LoaderCircle className="mx-auto h-6 w-6 animate-spin text-emerald-500" />
          <h3 className={`text-base font-semibold ${light ? "text-stone-950" : "text-white"}`}>Connecting {subject} console</h3>
          <p className={`text-sm leading-6 ${mutedClass}`}>
            Opening an interactive shell inside <span className="font-mono">{subjectRef}</span>.
          </p>
        </div>
      </div>
    );
  }
  return (
    <div className={`grid h-full min-h-0 place-items-center rounded-lg border border-dashed p-8 text-center ${borderClass}`}>
      <div className="grid max-w-md gap-4">
        <div
          className={`mx-auto flex h-12 w-12 items-center justify-center rounded-full border ${light ? "border-stone-200 bg-stone-100" : "border-stone-600 bg-stone-800"}`}
        >
          <TerminalSquare className={`h-6 w-6 ${light ? "text-stone-600" : "text-stone-300"}`} />
        </div>
        <div className="grid gap-2">
          <h3 className={`text-base font-semibold ${light ? "text-stone-950" : "text-white"}`}>No active {subject} console</h3>
          <p className={`text-sm leading-6 ${mutedClass}`}>
            Start an interactive shell inside <span className="font-mono">{subjectRef}</span>. It uses the shared live terminal surface.
          </p>
          {warning ? <p className="text-xs text-amber-500">{warning}</p> : null}
          {!selectedRuntimeTarget ? (
            <p className="text-xs text-red-500">
              This profile does not have a live runtime surface yet. Save the connector once, then retry.
            </p>
          ) : null}
        </div>
        <Button type="button" className="mx-auto" onClick={onStart} disabled={!selectedRuntimeTarget}>
          <RefreshCcw className="h-4 w-4" />
          Start {subjectLabel} Console
        </Button>
      </div>
    </div>
  );
}
