import { useEffect, useMemo, useState } from "react";
import { Copy, RefreshCcw, TerminalSquare } from "lucide-react";
import { apiGet, apiPost } from "../../../lib/api";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";

const terminalStatuses = new Set(["completed", "failed", "error", "declined", "stale", "untracked"]);

export function BulkCommandDialog({ open, targets, selectedTarget, onClose, onRefresh }) {
  const [selected, setSelected] = useState({});
  const [command, setCommand] = useState("");
  const [reason, setReason] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [targetQuery, setTargetQuery] = useState("");
  const [runState, setRunState] = useState({ state: "idle", error: null, items: [], parallelism: 3 });
  const [selectedResultID, setSelectedResultID] = useState(null);

  const visibleTargets = useMemo(() => {
    const query = targetQuery.trim().toLowerCase();
    if (!query) return targets;
    return targets.filter((target) => `${target.name} ${target.username} ${target.host}`.toLowerCase().includes(query));
  }, [targets, targetQuery]);
  const selectedIDs = useMemo(
    () => targets.filter((target) => selected[target.id]).map((target) => Number(target.id)),
    [targets, selected]
  );
  const confirmationText = selectedIDs.length > 0 ? `RUN ON ${selectedIDs.length} TARGETS` : "RUN ON 0 TARGETS";
  const canRun = selectedIDs.length > 0 && command.trim() && confirmation === confirmationText && runState.state !== "starting";
  const hasActiveItems = runState.items.some((item) => !terminalStatuses.has(item.status));

  useEffect(() => {
    if (!open) return;
    const initial = {};
    if (selectedTarget?.id) {
      initial[selectedTarget.id] = true;
    }
    setSelected(initial);
    setCommand("");
    setReason("");
    setConfirmation("");
    setTargetQuery("");
    setRunState({ state: "idle", error: null, items: [], parallelism: 3 });
    setSelectedResultID(null);
  }, [open, selectedTarget?.id]);

  useEffect(() => {
    if (!open || !hasActiveItems) return undefined;
    const timer = window.setInterval(() => {
      void refreshRequests();
    }, 2500);
    return () => window.clearInterval(timer);
  }, [open, hasActiveItems, runState.items.map((item) => `${item.request_id}:${item.status}`).join(",")]);

  function toggleTarget(runtimeID) {
    setSelected((current) => ({ ...current, [runtimeID]: !current[runtimeID] }));
    setConfirmation("");
  }

  function setAllTargets(value) {
    const next = {};
    if (value) {
      targets.forEach((target) => {
        next[target.id] = true;
      });
    }
    setSelected(next);
    setConfirmation("");
  }

  function copyConfirmationText() {
    if (typeof navigator === "undefined" || !navigator.clipboard) return;
    void navigator.clipboard.writeText(confirmationText);
  }

  async function startBulkCommand(event) {
    event.preventDefault();
    if (!canRun) return;
    setRunState((current) => ({ ...current, state: "starting", error: null }));
    setSelectedResultID(null);
    try {
      const data = await apiPost("/api/console/bulk-exec", {
        target_ids: selectedIDs,
        command: command.trim(),
        reason: reason.trim(),
        confirmation,
      });
      setRunState({
        state: "running",
        error: null,
        items: (data.items || []).map((item) => ({ ...item, status: item.status || "running" })),
        parallelism: data.parallelism || 3,
      });
      await onRefresh?.();
      window.setTimeout(() => void refreshRequests(), 1000);
    } catch (error) {
      setRunState((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  async function refreshRequests() {
    if (runState.items.length === 0) return;
    try {
      const details = await Promise.all(
        runState.items.map(async (item) => {
          try {
            const detail = await apiGet(`/api/console/command-requests/${item.request_id}`);
            return { ...item, ...detail, request_id: item.request_id };
          } catch (error) {
            return { ...item, status: "error", error: error.message };
          }
        })
      );
      setRunState((current) => ({ ...current, state: details.some((item) => !terminalStatuses.has(item.status)) ? "running" : "done", items: details, error: null }));
      await onRefresh?.();
    } catch (error) {
      setRunState((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  return (
    <Dialog
      open={open}
      title="Bulk command"
      description="Run one shell command across selected SSH connector targets."
      onClose={onClose}
      size="wide"
      className="h-[calc(100vh-100px)] !w-[100vw] !min-w-[1024px] !max-w-[1600px] grid-rows-[auto_minmax(0,1fr)]"
      closeOnOverlay={false}
      autoFocusClose={false}
      bodyClassName="grid min-h-0 gap-4 overflow-hidden"
    >
      <form className="grid h-full min-h-0 gap-4 lg:grid-cols-[minmax(260px,340px)_minmax(0,1fr)]" onSubmit={startBulkCommand}>
        <section className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-3">
          <div className="grid gap-3">
            <div className="flex items-center justify-between gap-2">
              <div>
                <p className="text-sm font-semibold text-stone-950">Targets</p>
                <p className="text-xs text-stone-500">{selectedIDs.length} selected</p>
              </div>
              <div className="flex gap-2">
                <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={() => setAllTargets(true)}>
                  All
                </Button>
                <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={() => setAllTargets(false)}>
                  None
                </Button>
              </div>
            </div>
            <input
              className="h-9 rounded-md border border-stone-300 px-3 text-sm outline-none focus:border-emerald-700"
              value={targetQuery}
              onChange={(event) => setTargetQuery(event.target.value)}
              placeholder="Search targets"
            />
          </div>
          <div className="min-h-0 overflow-auto rounded-md border border-stone-200">
            {visibleTargets.map((server) => (
              <label key={server.id} className="flex cursor-pointer items-start gap-3 border-b border-stone-100 px-3 py-2 last:border-b-0 hover:bg-stone-50">
                <input
                  type="checkbox"
                  className="mt-1 h-4 w-4 accent-emerald-800"
                  checked={Boolean(selected[server.id])}
                  onChange={() => toggleTarget(server.id)}
                />
                <span className="min-w-0">
                  <span className="block truncate text-sm font-semibold text-stone-900">{server.name}</span>
                  <span className="block truncate text-xs text-stone-500">
                    {server.username}@{server.host}:{server.port}
                  </span>
                </span>
              </label>
            ))}
            {visibleTargets.length === 0 ? <p className="px-3 py-6 text-center text-sm text-stone-500">No matching targets.</p> : null}
          </div>
        </section>

        <section className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-4">
          <div className="grid gap-3">
            <label className="grid gap-1 text-sm font-semibold text-stone-700">
              Command
              <textarea
                className="h-32 resize-none rounded-md border border-stone-300 px-3 py-2 font-mono text-sm font-normal outline-none focus:border-emerald-700"
                value={command}
                onChange={(event) => setCommand(event.target.value)}
                placeholder="apt update"
              />
            </label>
            <label className="grid gap-1 text-sm font-semibold text-stone-700">
              Reason
              <input
                className="h-10 rounded-md border border-stone-300 px-3 text-sm font-normal outline-none focus:border-emerald-700"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                placeholder="optional"
              />
            </label>
            <Notice tone="warn" className="flex h-10 items-center overflow-hidden px-3 py-0 text-xs">
              <span className="min-w-0 truncate">
                Type <span className="font-mono font-semibold">{confirmationText}</span>
                <button
                  type="button"
                  className="ml-1 inline-grid h-5 w-5 place-items-center align-[-3px] text-amber-900 hover:text-amber-700"
                  title="Copy confirmation phrase"
                  onClick={copyConfirmationText}
                >
                  <Copy className="h-3.5 w-3.5" />
                </button>{" "}
                before starting. Runs {runState.parallelism} targets at a time.
              </span>
            </Notice>
            <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
              <input
                className="h-10 rounded-md border border-stone-300 px-3 font-mono text-sm outline-none focus:border-emerald-700"
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
                placeholder={confirmationText}
              />
              <Button type="submit" disabled={!canRun}>
                <TerminalSquare className="h-4 w-4" />
                Run selected
              </Button>
            </div>
            {runState.error ? <Notice tone="bad">{runState.error}</Notice> : null}
          </div>

          <section className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-semibold text-stone-950">Results</p>
                <p className="text-xs text-stone-500">{runState.items.length > 0 ? resultSummary(runState.items) : "Results appear here after the command starts."}</p>
              </div>
              <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={refreshRequests} disabled={runState.items.length === 0}>
                <RefreshCcw className="h-3.5 w-3.5" />
                Refresh
              </Button>
            </div>
            {runState.items.length > 0 ? (
              <div className="grid min-h-0 gap-3 lg:grid-cols-[minmax(180px,230px)_minmax(0,1fr)]">
                <div className="min-h-0 overflow-auto rounded-md border border-stone-200">
                  {runState.items.map((item) => (
                    <BulkCommandResultRow
                      key={item.request_id}
                      item={item}
                      selected={selectedResultID === item.request_id}
                      onSelect={() => setSelectedResultID((current) => (current === item.request_id ? null : item.request_id))}
                    />
                  ))}
                </div>
                <BulkCommandResultDetail item={runState.items.find((item) => item.request_id === selectedResultID)} />
              </div>
            ) : (
              <div className="grid min-h-0 place-items-center rounded-md border border-dashed border-stone-300 bg-stone-50 p-6 text-center text-sm text-stone-500">
                Choose targets, enter a command, and run it to see per-target status and output.
              </div>
            )}
          </section>
        </section>
      </form>
    </Dialog>
  );
}

function BulkCommandResultRow({ item, selected, onSelect }) {
  return (
    <button
      type="button"
      className={`grid w-full gap-1 border-b border-stone-100 px-3 py-2 text-left last:border-b-0 hover:bg-stone-50 ${
        selected ? "border-l-4 border-l-emerald-700 bg-emerald-50 pl-2" : "border-l-4 border-l-transparent"
      }`}
      onClick={onSelect}
    >
      <span className="flex min-w-0 items-center justify-between gap-2">
        <span className="truncate text-sm font-semibold text-stone-950">{item.target_name || item.target_name}</span>
        <Badge tone={statusTone(item.status)} className="shrink-0 px-2 py-0.5 text-[11px]">
          {statusLabel(item.status)}
        </Badge>
      </span>
      <span className="flex items-center justify-between gap-2 text-xs text-stone-500">
        <span>#{item.request_id}</span>
        {typeof item.exit_code === "number" ? <span>exit {item.exit_code}</span> : <span>running</span>}
      </span>
    </button>
  );
}

function BulkCommandResultDetail({ item }) {
  if (!item) {
    return (
      <div className="grid min-h-0 place-items-center rounded-md border border-dashed border-stone-300 bg-stone-50 p-5 text-center text-sm text-stone-500">
        Select a result on the left to inspect its captured console output.
      </div>
    );
  }
  const output = item.stdout || item.stderr || item.error || "";
  return (
    <article className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2 rounded-md border border-stone-200 p-3">
      <div className="flex min-w-0 items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-stone-950">{item.target_name || item.target_name}</p>
          <p className="text-xs text-stone-500">Request #{item.request_id}</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {typeof item.exit_code === "number" ? <Badge tone="neutral">exit {item.exit_code}</Badge> : null}
          <Badge tone={statusTone(item.status)}>{statusLabel(item.status)}</Badge>
        </div>
      </div>
      <TerminalBlock surface="log" className="min-h-0 p-3 text-xs">
        {output || "No output captured yet."}
      </TerminalBlock>
    </article>
  );
}

function resultSummary(items) {
  const running = items.filter((item) => !terminalStatuses.has(item.status)).length;
  const failed = items.filter((item) => item.status === "failed" || item.status === "error").length;
  const done = items.length - running;
  if (running > 0) return `${done}/${items.length} finished, ${running} running`;
  if (failed > 0) return `${done}/${items.length} finished, ${failed} failed`;
  return `${done}/${items.length} finished`;
}

function statusLabel(status) {
  if (status === "pending_approval") return "pending";
  if (status === "untracked") return "not tracked";
  return status || "running";
}

function statusTone(status) {
  if (status === "completed") return "good";
  if (status === "running" || status === "pending_approval") return "warn";
  if (status === "failed" || status === "error" || status === "declined" || status === "stale") return "bad";
  return "neutral";
}
