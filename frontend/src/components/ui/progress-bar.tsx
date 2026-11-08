import { cn } from "../../lib/utils";

type Props = {
  value: number;
  active?: boolean;
  compact?: boolean;
  className?: string;
  label?: string;
  tone?: "normal" | "warning" | "critical" | "muted";
};

export function ProgressBar({ value, active = false, compact = false, className, label, tone = "normal" }: Props) {
  const normalized = Math.max(0, Math.min(100, Number(value) || 0));
  return (
    <progress
      className={cn("aip-progress w-full", compact ? "h-1.5" : "h-2", active ? "animate-pulse" : "", className)}
      max="100"
      value={normalized}
      data-tone={tone}
      aria-label={label ?? `${normalized}% complete`}
    />
  );
}
