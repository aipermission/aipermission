import { cn } from "../../lib/utils";

export function Badge({ className, tone = "neutral", ...props }) {
  const tones = {
    neutral: "border-stone-200 bg-stone-100 text-stone-700 dark-badge-neutral",
    good: "border-emerald-200 bg-emerald-50 text-emerald-800 dark-badge-good",
    warn: "border-amber-200 bg-amber-50 text-amber-800 dark-badge-warn",
    bad: "border-red-200 bg-red-50 text-red-800 dark-badge-bad",
  };
  return (
    <span
      className={cn("inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-semibold", tones[tone], className)}
      {...props}
    />
  );
}

export function CountBadge({ className, children, tone = "red", ...props }) {
  const tones = {
    red: "bg-red-600 text-white dark-count-red",
    stone: "bg-stone-900 text-white dark-count-stone",
    emerald: "bg-emerald-700 text-white dark-count-emerald",
  };
  return (
    <span
      className={cn(
        "inline-block h-[18px] min-w-[18px] shrink-0 rounded-full px-1.5 text-center align-middle text-[10px] font-bold leading-[18px] tabular-nums shadow-sm",
        tones[tone],
        className,
      )}
      {...props}
    >
      {children}
    </span>
  );
}
