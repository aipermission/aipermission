import { useEffect, useMemo, useState } from "react";
import { Download, Pause, Play, RefreshCcw, Upload, XCircle } from "lucide-react";
import { formatBytes, formatETA, transferProgress } from "../lib/file-transfer-utils";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Drawer } from "./ui/drawer";
import { Notice } from "./ui/notice";

const activeStatuses = new Set(["pending_approval", "pending", "running", "paused"]);

export function TransferCenter({ open, batches, state, error, onClose, onRefresh, onPause, onResume, onCancel, onApprove, onDecline }) {
  const active = batches.filter((batch) => activeStatuses.has(batch.status));
  const recent = batches.filter((batch) => !activeStatuses.has(batch.status)).slice(0, 8);

  return (
    <Drawer
      open={open}
      title="Transfer Center"
      description="Monitor uploads and downloads started from the UI or MCP."
      onClose={onClose}
      bodyClassName="grid content-start gap-4"
    >
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-sm font-semibold text-stone-950">{active.length} active queue{active.length === 1 ? "" : "s"}</p>
          <p className="text-xs text-stone-500">Closing this panel does not stop transfers.</p>
        </div>
        <Button type="button" variant="outline" className="h-9" onClick={onRefresh} disabled={state === "loading"}>
          <RefreshCcw className="h-4 w-4" />
          Refresh
        </Button>
      </div>

      {error ? <Notice tone="bad">{error}</Notice> : null}
      {state === "loading" && batches.length === 0 ? <Notice>Loading transfer queues...</Notice> : null}

      {active.length > 0 ? (
        <section className="grid gap-3">
          <h3 className="text-xs font-semibold uppercase text-stone-500">Active</h3>
          {active.map((batch) => (
            <TransferBatchCard key={batch.id} batch={batch} onPause={onPause} onResume={onResume} onCancel={onCancel} onApprove={onApprove} onDecline={onDecline} />
          ))}
        </section>
      ) : (
        <div className="rounded-md border border-dashed border-stone-300 bg-stone-50 p-6 text-center text-sm text-stone-500">
          No active transfers.
        </div>
      )}

      {recent.length > 0 ? (
        <section className="grid gap-3">
          <h3 className="text-xs font-semibold uppercase text-stone-500">Recent</h3>
          {recent.map((batch) => (
            <TransferBatchCard key={batch.id} batch={batch} compact />
          ))}
        </section>
      ) : null}
    </Drawer>
  );
}

function TransferBatchCard({ batch, compact = false, onPause, onResume, onCancel, onApprove, onDecline }) {
  const progress = transferProgress(batch);
  const active = activeStatuses.has(batch.status);
  const approvalMode = batch.status === "pending_approval";
  const Icon = batch.direction === "upload" ? Upload : Download;
  const pendingItems = useMemo(() => (batch.items || []).filter((item) => item.status === "pending_approval"), [batch.items]);
  const pendingItemKey = pendingItems.map((item) => item.id).join(",");
  const [selectedItems, setSelectedItems] = useState(() => new Set(pendingItems.map((item) => item.id)));
  const [note, setNote] = useState("");

  useEffect(() => {
    setSelectedItems(new Set(pendingItems.map((item) => item.id)));
    setNote("");
  }, [batch.id, pendingItemKey]);

  function toggleItem(itemID) {
    setSelectedItems((current) => {
      const next = new Set(current);
      if (next.has(itemID)) {
        next.delete(itemID);
      } else {
        next.add(itemID);
      }
      return next;
    });
  }

  function approveSelected() {
    onApprove?.(batch.id, Array.from(selectedItems), note);
  }

  function declineAll() {
    onDecline?.(batch.id, note);
  }

  return (
    <article className="grid gap-3 rounded-md border border-stone-200 bg-white p-4 shadow-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <Icon className="h-4 w-4 text-stone-500" />
            <p className="truncate text-sm font-semibold text-stone-950">
              {batch.target_name || `Target profile #${batch.runtime_id}`} - {batch.direction}
            </p>
            <Badge tone={statusTone(batch.status)}>{batch.status}</Badge>
            <Badge tone={batch.source === "mcp" ? "warn" : "neutral"}>{batch.source}</Badge>
          </div>
          <p className="mt-1 text-xs text-stone-500">
            {batchProgressSummary(batch)} - {formatBytes(batch.transferred_bytes)} transferred
          </p>
        </div>
        {!compact && active && !approvalMode ? (
          <div className="flex shrink-0 items-center gap-1">
            {batch.status === "running" ? (
              <Button type="button" variant="ghost" className="h-8 w-8 px-0" onClick={() => onPause?.(batch.id)} title="Pause">
                <Pause className="h-4 w-4" />
              </Button>
            ) : null}
            {batch.status === "paused" ? (
              <Button type="button" variant="ghost" className="h-8 w-8 px-0" onClick={() => onResume?.(batch.id)} title="Resume">
                <Play className="h-4 w-4" />
              </Button>
            ) : null}
            <Button type="button" variant="ghost" className="h-8 w-8 px-0 text-red-700" onClick={() => onCancel?.(batch.id)} title="Cancel">
              <XCircle className="h-4 w-4" />
            </Button>
          </div>
        ) : null}
      </div>

      <div className="grid gap-1.5">
        <div className="h-2 overflow-hidden rounded-full bg-stone-100">
          <div className={`h-full rounded-full bg-emerald-700 transition-all ${active ? "animate-pulse" : ""}`} style={{ width: `${progress.percent}%` }} />
        </div>
        <div className="flex items-center justify-between gap-3 text-xs text-stone-500">
          <span>{progress.percent}%</span>
          <span>{batch.bytes_per_second ? `${formatBytes(batch.bytes_per_second)}/s` : "-"}</span>
          <span>ETA {formatETA(batch.eta_seconds)}</span>
        </div>
      </div>

      {batch.error ? <p className="text-xs text-red-700">{batch.error}</p> : null}
      {!compact && approvalMode ? (
        <div className="grid gap-3 rounded-md border border-amber-300 bg-amber-50 p-3">
          <div>
            <p className="text-sm font-semibold text-amber-950">Local approval required</p>
            <p className="text-xs text-amber-800">Select the files AIPermission may transfer. Unchecked files are rejected with the note below.</p>
          </div>
          <div className="max-h-48 overflow-auto rounded-md border border-amber-200 bg-white">
            {pendingItems.map((item) => (
              <label key={item.id} className="grid cursor-pointer grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 border-b border-amber-100 px-3 py-2 last:border-b-0">
                <input
                  type="checkbox"
                  className="h-4 w-4 accent-emerald-700"
                  checked={selectedItems.has(item.id)}
                  onChange={() => toggleItem(item.id)}
                />
                <span className="truncate font-mono text-xs text-stone-700">{item.remote_path || item.file_name}</span>
                <span className="text-xs text-stone-500">{formatBytes(item.size_bytes)}</span>
              </label>
            ))}
          </div>
          <label className="grid gap-1">
            <span className="text-xs font-semibold uppercase text-amber-900">Note for AI</span>
            <textarea
              value={note}
              onChange={(event) => setNote(event.target.value)}
              rows={2}
              className="min-h-16 resize-y rounded-md border border-amber-200 bg-white px-3 py-2 text-sm text-stone-950 outline-none focus:border-emerald-600"
              placeholder="Optional approval or rejection note."
            />
          </label>
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={declineAll}>
              Decline all
            </Button>
            <Button type="button" onClick={approveSelected} disabled={selectedItems.size === 0}>
              Approve selected ({selectedItems.size})
            </Button>
          </div>
        </div>
      ) : null}
      {!compact && batch.items?.length ? (
        <div className="max-h-40 overflow-auto rounded-md border border-stone-200">
          {batch.items.slice(0, 20).map((item) => (
            <div key={item.id} className="grid grid-cols-[minmax(0,1fr)_auto] gap-2 border-b border-stone-100 px-3 py-2 last:border-b-0">
              <span className="truncate font-mono text-xs text-stone-600">{item.remote_path || item.file_name}</span>
              <Badge tone={statusTone(item.status)}>{item.status}</Badge>
            </div>
          ))}
        </div>
      ) : null}
    </article>
  );
}

function batchProgressSummary(batch) {
  const completed = Number(batch.completed_items || 0);
  const canceled = Number(batch.canceled_items || 0);
  const failed = Number(batch.failed_items || 0);
  const total = Number(batch.total_items || 0);
  const processed = completed + canceled + failed;
  const parts = [`${processed}/${total} processed`, `${completed} completed`];
  if (canceled > 0) parts.push(`${canceled} canceled`);
  if (failed > 0) parts.push(`${failed} failed`);
  return parts.join(", ");
}

function statusTone(status) {
  if (status === "completed") return "good";
  if (status === "failed") return "bad";
  if (status === "canceled" || status === "paused") return "warn";
  if (status === "pending_approval") return "warn";
  return "neutral";
}
