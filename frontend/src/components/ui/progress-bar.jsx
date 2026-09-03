import { cn } from "../../lib/utils";

export function ProgressBar({ value, active = false, compact = false, className }) {
  const normalized = Math.max(0, Math.min(100, Number(value) || 0));
  return (
    <progress
      className={cn("aip-progress w-full", compact ? "h-1.5" : "h-2", active ? "animate-pulse" : "", className)}
      max="100"
      value={normalized}
      aria-label={`${normalized}% complete`}
    />
  );
}
