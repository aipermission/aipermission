import { Download, Pause, Play, RefreshCcw, Upload, XCircle } from "lucide-react";
import { formatBytes, formatETA, transferProgress } from "./console/file-transfer-utils";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Drawer } from "./ui/drawer";
import { Notice } from "./ui/notice";

const activeStatuses = new Set(["pending", "running", "paused"]);

export function TransferCenter({ open, batches, state, error, onClose, onRefresh, onPause, onResume, onCancel }) {
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
            <TransferBatchCard key={batch.id} batch={batch} onPause={onPause} onResume={onResume} onCancel={onCancel} />
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

function TransferBatchCard({ batch, compact = false, onPause, onResume, onCancel }) {
  const progress = transferProgress(batch);
  const active = activeStatuses.has(batch.status);
  const Icon = batch.direction === "upload" ? Upload : Download;

  return (
    <article className="grid gap-3 rounded-md border border-stone-200 bg-white p-4 shadow-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <Icon className="h-4 w-4 text-stone-500" />
            <p className="truncate text-sm font-semibold text-stone-950">
              {batch.server_name || `Server #${batch.server_id}`} - {batch.direction}
            </p>
            <Badge tone={statusTone(batch.status)}>{batch.status}</Badge>
            <Badge tone={batch.source === "mcp" ? "warn" : "neutral"}>{batch.source}</Badge>
          </div>
          <p className="mt-1 text-xs text-stone-500">
            {batch.completed_items}/{batch.total_items} completed - {formatBytes(batch.transferred_bytes)} / {formatBytes(batch.size_bytes)}
          </p>
        </div>
        {!compact && active ? (
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

function statusTone(status) {
  if (status === "completed") return "good";
  if (status === "failed") return "bad";
  if (status === "canceled" || status === "paused") return "warn";
  return "neutral";
}
