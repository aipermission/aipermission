import { ArrowDown, ArrowUp, Trash2 } from "lucide-react";
import { cn } from "../../lib/utils";
import { Button } from "../ui/button";
import { formatBytes, formatETA, pendingBatchItemIDs, transferProgress } from "./file-transfer-utils";

export function QueueSummary({ batch, queue, mode, progress }) {
  const totalSize = batch ? batch.size_bytes : queue.reduce((sum, item) => sum + Number(item.size || 0), 0);
  const totalItems = batch ? batch.total_items : queue.length;
  return (
    <div className="grid gap-3 rounded-md border border-stone-200 bg-white p-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide text-stone-500">Summary</p>
          <p className="mt-1 text-sm font-semibold text-stone-900">{totalItems} item{totalItems === 1 ? "" : "s"}</p>
        </div>
        <span className="rounded-full border border-stone-200 px-2.5 py-1 text-xs font-semibold text-stone-600">{batch?.status || mode}</span>
      </div>
      <div className="grid gap-2 text-sm">
        <div className="flex justify-between gap-3">
          <span className="text-stone-500">Total size</span>
          <span className="font-mono text-stone-800">{formatBytes(totalSize)}</span>
        </div>
        <div className="flex justify-between gap-3">
          <span className="text-stone-500">Progress</span>
          <span className="font-mono text-stone-800">{progress.percent}%</span>
        </div>
        <div className="flex justify-between gap-3">
          <span className="text-stone-500">Speed</span>
          <span className="font-mono text-stone-800">{batch?.bytes_per_second ? `${formatBytes(batch.bytes_per_second)}/s` : "-"}</span>
        </div>
        <div className="flex justify-between gap-3">
          <span className="text-stone-500">ETA</span>
          <span className="font-mono text-stone-800">{formatETA(batch?.eta_seconds)}</span>
        </div>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-stone-100">
        <div className={cn("h-full rounded-full bg-emerald-700 transition-all", batch?.status === "running" ? "animate-pulse" : "")} style={{ width: `${progress.percent}%` }} />
      </div>
    </div>
  );
}

export function QueueList({ mode, queue, batch, active, canEditPausedBatch, onRemove, onMove }) {
  const items = batch?.items || queue;
  const editableIDs = canEditPausedBatch ? pendingBatchItemIDs(batch) : [];
  if (items.length === 0) {
    return (
      <div className="grid min-h-72 place-items-center rounded-md border border-dashed border-stone-300 bg-stone-50 p-8 text-center text-sm text-stone-500">
        {mode === "upload" ? "Add local files to build an upload queue." : "Add remote files to build a download queue."}
      </div>
    );
  }
  return (
    <div className="min-h-0 overflow-hidden rounded-md border border-stone-200 bg-white">
      <div className="max-h-[48vh] overflow-auto">
        {items.map((item, index) => {
          const editableIndex = editableIDs.indexOf(Number(item.id));
          return (
            <QueueRow
              key={item.id || item.path || item.remote_path}
              item={item}
              index={index}
              total={items.length}
              active={active}
              batchMode={Boolean(batch)}
              canEditPausedBatch={canEditPausedBatch}
              canMoveUp={editableIndex > 0}
              canMoveDown={editableIndex >= 0 && editableIndex < editableIDs.length - 1}
              mode={mode}
              onRemove={onRemove}
              onMove={onMove}
            />
          );
        })}
      </div>
    </div>
  );
}

function QueueRow({ item, index, total, active, batchMode, canEditPausedBatch, canMoveUp, canMoveDown, mode, onRemove, onMove }) {
  const name = item.file_name || item.name || item.path || item.remote_path;
  const source = mode === "upload" ? item.remote_path : item.path || item.remote_path;
  const progress = transferProgress(item.status ? item : null);
  const canEdit = !batchMode || (canEditPausedBatch && item.status === "pending");
  return (
    <div className="grid gap-2 border-b border-stone-100 p-3 last:border-b-0">
      <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-stone-900">{name}</p>
          <p className="truncate font-mono text-xs text-stone-500">{source}</p>
        </div>
        <div className="flex items-center gap-1">
          {canEdit ? (
            <>
              <Button type="button" variant="ghost" className="h-8 w-8 px-0" onClick={() => onMove(item.id, -1)} disabled={batchMode ? !canMoveUp : active || index === 0}>
                <ArrowUp className="h-4 w-4" />
              </Button>
              <Button type="button" variant="ghost" className="h-8 w-8 px-0" onClick={() => onMove(item.id, 1)} disabled={batchMode ? !canMoveDown : active || index === total - 1}>
                <ArrowDown className="h-4 w-4" />
              </Button>
              <Button type="button" variant="ghost" className="h-8 w-8 px-0 text-red-700" onClick={() => onRemove(item.id)} disabled={!batchMode && active}>
                <Trash2 className="h-4 w-4" />
              </Button>
            </>
          ) : (
            <span className="rounded-full border border-stone-200 px-2 py-1 text-xs font-semibold text-stone-600">{item.status}</span>
          )}
        </div>
      </div>
      <div className="flex items-center justify-between gap-3 text-xs text-stone-500">
        <span>{formatBytes(item.size || item.size_bytes || 0)}</span>
        {batchMode ? <span>{formatETA(item.eta_seconds)}</span> : <span>#{index + 1}</span>}
      </div>
      {batchMode ? (
        <div className="h-1.5 overflow-hidden rounded-full bg-stone-100">
          <div className={cn("h-full rounded-full bg-emerald-700 transition-all", item.status === "running" ? "animate-pulse" : "")} style={{ width: `${progress.percent}%` }} />
        </div>
      ) : null}
      {item.error ? <p className="text-xs text-red-700">{item.error}</p> : null}
    </div>
  );
}
