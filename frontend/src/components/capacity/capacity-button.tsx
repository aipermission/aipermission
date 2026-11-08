import { Gauge } from "lucide-react";
import { ProgressBar } from "../ui/progress-bar";
import type { Report } from "./model";

export function CapacityButton({ data, error, onOpen }: { data: Report | null; error: string | null; onOpen: () => void }) {
  const percent = data ? Math.min(100, (data.used / data.limit) * 100) : 0;
  const tone = error ? "muted" : percent >= 90 ? "critical" : percent >= 70 ? "warning" : "normal";
  return (
    <button
      type="button"
      onClick={onOpen}
      className="mb-3 grid h-14 w-full gap-2 rounded-md border border-stone-300 bg-white px-3 py-2 text-sm font-semibold text-stone-800 hover:bg-stone-100"
      title={error || "Console session capacity"}
    >
      <span className="flex min-w-0 items-center gap-2">
        <Gauge className="h-4 w-4 shrink-0" />
        <span>Capacity</span>
        <span className="ml-auto text-xs text-stone-500">
          {error ? "Unavailable" : data ? `${data.used} / ${data.limit}` : "Loading..."}
        </span>
      </span>
      <ProgressBar value={percent} label="Console capacity" tone={tone} compact />
    </button>
  );
}
