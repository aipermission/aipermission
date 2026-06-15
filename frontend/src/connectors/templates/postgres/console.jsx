import { Database, RefreshCcw, XCircle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { apiPost } from "../../../lib/api";

export function PostgresConnectorConsoleTemplate({ target, approvals, theme, session, onRefreshActivity }) {
  const [selectedID, setSelectedID] = useState(null);
  const [sql, setSQL] = useState("");
  const [maxRows, setMaxRows] = useState(100);
  const [runState, setRunState] = useState({ state: "idle", error: "" });
  const panelClass = theme === "light" ? "bg-white text-stone-900" : "bg-[#1e1e1e] text-stone-100";
  const mutedClass = theme === "light" ? "text-stone-500" : "text-stone-400";
  const borderClass = theme === "light" ? "border-stone-200" : "border-stone-700";
  const subtlePanelClass = theme === "light" ? "bg-stone-50" : "bg-[#252526]";
  const inputClass = theme === "light" ? "border-stone-300 bg-white text-stone-900 placeholder:text-stone-400" : "border-stone-700 bg-[#1a1a1a] text-stone-100 placeholder:text-stone-500";
  const hoverClass = theme === "light" ? "hover:bg-stone-50" : "hover:bg-stone-800/60";
  const activeSession = session || { active: false, startedAt: "" };
  const rawItems = useMemo(() => (approvals?.data || []).filter((item) => item.target_ref === target.ref), [approvals?.data, target.ref]);
  const items = useMemo(() => {
    if (!activeSession.active) return [];
    const startedAt = new Date(activeSession.startedAt).getTime();
    return rawItems.filter((item) => {
      const createdAt = new Date(item.created_at).getTime();
      return Number.isFinite(createdAt) && createdAt >= startedAt - 1000;
    });
  }, [rawItems, activeSession.active, activeSession.startedAt]);
  const selected = useMemo(() => {
    if (selectedID) {
      const exact = items.find((item) => Number(item.id) === Number(selectedID));
      if (exact) return exact;
    }
    return items[0] || null;
  }, [items, selectedID]);

  useEffect(() => {
    setSelectedID(null);
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  async function runQuery(event) {
    event.preventDefault();
    if (!activeSession.active || !sql.trim()) return;
    setRunState({ state: "running", error: "" });
    try {
      const item = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: "query_readonly",
        input: {
          sql,
          max_rows: Number(maxRows) || 100,
        },
        reason: "manual Postgres console query",
      });
      setSelectedID(item.request_id || null);
      setSQL("");
      setRunState({ state: "idle", error: "" });
      await onRefreshActivity?.();
    } catch (error) {
      setRunState({ state: "error", error: error.message || "Query failed." });
    }
  }

  return (
    <div className={`grid min-h-0 grid-rows-[minmax(0,1fr)_auto_auto] ${panelClass}`}>
      <div className="grid min-h-0 gap-4 overflow-hidden p-4 lg:grid-cols-[320px_minmax(0,1fr)]">
        <section className={`grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass}`}>
          <div className={`border-b px-4 py-3 ${borderClass} ${subtlePanelClass}`}>
            <h4 className="text-sm font-semibold">Session requests</h4>
            <p className={`mt-1 text-xs ${mutedClass}`}>
              {activeSession.active ? `${items.length} request${items.length === 1 ? "" : "s"} since ${formatConnectorTime(activeSession.startedAt)}.` : "Session ended. Start a new session to watch new requests here."}
            </p>
          </div>
          <div className={`min-h-0 overflow-y-auto divide-y ${theme === "light" ? "divide-stone-200" : "divide-stone-700"}`}>
            {items.map((item) => {
              const active = selected && Number(selected.id) === Number(item.id);
              return (
                <button
                  key={item.id}
                  type="button"
                  className={`grid w-full gap-1 px-4 py-3 text-left transition ${active ? "bg-emerald-950 text-white" : hoverClass}`}
                  onClick={() => setSelectedID(active ? null : item.id)}
                >
                  <span className="flex min-w-0 items-center justify-between gap-2">
                    <span className="truncate font-mono text-xs font-semibold">{item.action_name}</span>
                    <ActivityStatusBadge status={item.status} />
                  </span>
                  <span className={`truncate text-xs ${active ? "text-emerald-100" : mutedClass}`}>{item.reason || formatConnectorTime(item.created_at)}</span>
                </button>
              );
            })}
            {items.length === 0 ? <p className={`px-4 py-5 text-sm ${mutedClass}`}>{activeSession.active ? "No requests in this session yet." : "No active Postgres session."}</p> : null}
          </div>
        </section>

        <section className={`grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass}`}>
          {selected ? (
            <>
              <header className={`border-b px-4 py-3 ${borderClass} ${subtlePanelClass}`}>
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <h4 className="truncate text-sm font-semibold">Request #{selected.id}</h4>
                    <p className={`mt-1 truncate text-xs ${mutedClass}`}>
                      {selected.action_name} / {formatConnectorTime(selected.created_at)}
                    </p>
                  </div>
                  <ActivityStatusBadge status={selected.status} />
                </div>
                {selected.reason ? <p className={`mt-2 text-xs ${mutedClass}`}>Reason: {selected.reason}</p> : null}
                {selected.error ? <Notice tone="bad">{selected.error}</Notice> : null}
              </header>
              <div className="grid min-h-0 gap-3 overflow-hidden p-3 xl:grid-cols-2">
                <ActivityBlock title="Input" value={selected.input || {}} />
                <ActivityBlock title="Output" value={selected.output ?? selected.display_text ?? {}} />
              </div>
            </>
          ) : (
            <div className={`grid place-items-center p-6 text-sm ${mutedClass}`}>
              Select a session request to inspect input and output. Completed requests remain available in History.
            </div>
          )}
        </section>
      </div>

      <div className={`border-t px-4 py-2 text-xs ${borderClass} ${mutedClass}`}>
        <span className="inline-flex min-w-0 items-center gap-2">
          <Database className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">{targetEndpoint(target)}</span>
        </span>
      </div>
      <form className={`grid gap-2 border-t p-3 ${borderClass} ${subtlePanelClass}`} onSubmit={runQuery}>
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="text-xs font-semibold">SQL</p>
            <p className={`truncate text-xs ${mutedClass}`}>{activeSession.active ? "Run bounded read-only SQL through this credential profile." : "No active Postgres session. Start a session before running SQL."}</p>
          </div>
          <label className="flex shrink-0 items-center gap-2 text-xs font-semibold">
            Max rows
            <input
              type="number"
              min="1"
              max="1000"
              className={`h-8 w-20 rounded-md border px-2 outline-none ${inputClass}`}
              value={maxRows}
              onChange={(event) => setMaxRows(event.target.value)}
              disabled={!activeSession.active || runState.state === "running"}
            />
          </label>
        </div>
        <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_auto]">
          <textarea
            className={`min-h-20 rounded-md border px-3 py-2 font-mono text-xs outline-none ${inputClass}`}
            value={sql}
            onChange={(event) => setSQL(event.target.value)}
            placeholder="select now();"
            disabled={!activeSession.active || runState.state === "running"}
          />
          <Button type="submit" className="h-full min-h-10 px-5" disabled={!activeSession.active || !sql.trim() || runState.state === "running"}>
            {runState.state === "running" ? "Running" : "Run SQL"}
          </Button>
        </div>
        {runState.error ? <Notice tone="bad">{runState.error}</Notice> : null}
      </form>
    </div>
  );
}

export function PostgresConnectorToolbarActionsTemplate({ theme, structuredSession, onNewStructuredSession, onEndStructuredSession }) {
  const buttonClass = `h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`;
  const active = Boolean(structuredSession?.active);
  return (
    <>
      <Button type="button" variant="ghost" className={buttonClass} onClick={onNewStructuredSession} disabled={active} title="Start a fresh Postgres activity session">
        <RefreshCcw className="h-3.5 w-3.5" />
        New Session
      </Button>
      <Button type="button" variant="ghost" className={buttonClass} onClick={onEndStructuredSession} disabled={!active} title="End the current Postgres activity session">
        <XCircle className="h-3.5 w-3.5" />
        End Session
      </Button>
    </>
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

function ActivityStatusBadge({ status }) {
  const tone = status === "completed" ? "good" : status === "failed" || status === "error" || status === "stale" ? "bad" : status === "approval_pending" || status === "running" ? "warn" : "neutral";
  return <Badge tone={tone}>{status}</Badge>;
}

function formatConnectorTime(value) {
  if (!value) return "-";
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(value));
}

function formatJSON(value) {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value ?? {}, null, 2);
  } catch {
    return String(value);
  }
}

function targetEndpoint(target) {
  if (!target) return "-";
  const host = target.config?.host || "host";
  const port = target.config?.port || 5432;
  const database = target.config?.database || "database";
  return `${host}:${port}/${database}`;
}
