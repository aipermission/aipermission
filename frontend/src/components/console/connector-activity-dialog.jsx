import { useMemo, useState } from "react";
import { RefreshCcw } from "lucide-react";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { TerminalBlock } from "../ui/terminal-block";

export function ConnectorActivityDialog({ open, approvals, onRefresh, onClose }) {
  const items = approvals?.data || [];
  const [selectedID, setSelectedID] = useState(null);
  const selected = useMemo(() => {
    if (selectedID) return items.find((item) => Number(item.id) === Number(selectedID)) || items[0] || null;
    return items[0] || null;
  }, [items, selectedID]);

  return (
    <Dialog
      open={open}
      title="Connector activity"
      description="Recent structured connector requests, including always-run requests that do not appear in the SSH terminal."
      onClose={onClose}
      size="wide"
      className="h-[calc(100vh-100px)] !w-[85vw] !min-w-[1024px] !max-w-[1440px] grid-rows-[auto_minmax(0,1fr)]"
      bodyClassName="min-h-0 overflow-hidden"
    >
      <div className="grid h-full min-h-0 gap-3 grid-cols-[360px_minmax(0,1fr)]">
        <aside className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border border-stone-200 bg-white">
          <div className="flex items-center justify-between gap-2 border-b border-stone-200 bg-stone-50 px-3 py-2">
            <div>
              <p className="text-xs font-semibold uppercase text-stone-500">Recent requests</p>
              <p className="text-xs text-stone-500">{items.length} shown</p>
            </div>
            <Button type="button" variant="outline" className="h-8 px-2" onClick={onRefresh} disabled={approvals?.state === "loading"}>
              <RefreshCcw className="h-3.5 w-3.5" />
            </Button>
          </div>
          <div className="min-h-0 overflow-y-auto">
            {items.map((item) => (
              <button
                key={item.id}
                type="button"
                className={`grid w-full gap-1 border-b border-stone-200 px-3 py-2 text-left transition ${
                  selected && Number(selected.id) === Number(item.id) ? "bg-emerald-950 text-white" : "bg-white hover:bg-stone-50"
                }`}
                onClick={() => setSelectedID(item.id)}
              >
                <div className="flex min-w-0 items-center justify-between gap-2">
                  <span className="truncate text-sm font-semibold">{item.target_name || item.target_ref}</span>
                  <ActivityBadge status={item.status} />
                </div>
                <span className="truncate font-mono text-xs opacity-80">{item.action_name}</span>
                <span className="truncate text-xs opacity-75">
                  {item.connector_kind} / {item.profile_label || "profile"} / {formatTime(item.created_at)}
                </span>
              </button>
            ))}
            {items.length === 0 ? <p className="p-3 text-sm text-stone-500">No connector activity yet.</p> : null}
          </div>
        </aside>

        <section className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border border-stone-200 bg-white">
          {selected ? (
            <>
              <header className="border-b border-stone-200 bg-stone-50 px-4 py-3">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <h3 className="truncate text-sm font-semibold text-stone-950">
                      Request #{selected.id} · {selected.target_name || selected.target_ref}
                    </h3>
                    <p className="mt-1 text-xs text-stone-500">
                      {selected.connector_kind} / {selected.profile_label || "profile"} / {selected.action_name}
                    </p>
                  </div>
                  <ActivityBadge status={selected.status} />
                </div>
                {selected.reason ? <p className="mt-2 text-xs text-stone-500">Reason: {selected.reason}</p> : null}
                {selected.error ? <p className="mt-2 text-xs font-semibold text-red-700">{selected.error}</p> : null}
              </header>
              <div className="grid min-h-0 gap-3 overflow-hidden p-3 lg:grid-cols-2">
                <ActivityBlock title="Input" value={selected.input || {}} />
                <ActivityBlock title="Output" value={selected.output ?? selected.display_text ?? {}} />
              </div>
            </>
          ) : (
            <div className="grid place-items-center text-sm text-stone-500">Select a connector request to inspect input and output.</div>
          )}
        </section>
      </div>
    </Dialog>
  );
}

function ActivityBlock({ title, value }) {
  return (
    <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
      <p className="text-xs font-semibold uppercase text-stone-500">{title}</p>
      <TerminalBlock className="min-h-0 overflow-auto text-xs">{formatJSON(value)}</TerminalBlock>
    </div>
  );
}

function ActivityBadge({ status }) {
  const tone = status === "completed" ? "good" : status === "failed" || status === "error" || status === "stale" ? "bad" : status === "approval_pending" || status === "running" ? "warn" : "neutral";
  return <Badge tone={tone}>{status}</Badge>;
}

function formatJSON(value) {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value ?? {}, null, 2);
  } catch {
    return String(value);
  }
}

function formatTime(value) {
  if (!value) return "-";
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(value));
}
