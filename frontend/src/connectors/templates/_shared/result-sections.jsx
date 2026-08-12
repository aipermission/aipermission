import { CopyButton } from "../../../components/ui/copy-button";
import { Input } from "../../../components/ui/form";

export function ConnectorResultHeader({ title, subtitle, copyValue, search, onSearch, inputClass, searchPlaceholder = "Search" }) {
  return (
    <div className="mb-2 flex items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">{title}</p>
        {subtitle ? <p className="truncate text-xs text-stone-500">{subtitle}</p> : null}
      </div>
      {onSearch || copyValue ? (
        <div className="flex min-w-0 items-center justify-end gap-2">
          {onSearch ? (
            <Input
              className={`h-8 w-56 text-xs ${inputClass || ""}`}
              value={search}
              onChange={(event) => onSearch(event.target.value)}
              placeholder={searchPlaceholder}
            />
          ) : null}
          {copyValue ? <CopyButton value={copyValue} variant="outline" className="h-8 px-2 text-xs" /> : null}
        </div>
      ) : null}
    </div>
  );
}

export function DarkSummaryGrid({ rows, columns = "md:grid-cols-2 xl:grid-cols-3" }) {
  return (
    <div className="min-h-0 overflow-auto rounded-md border border-stone-700 bg-[#1a1a1a] p-3">
      <div className={`grid gap-2 ${columns}`}>
        {rows.map(({ label, value }) => (
          <div key={label} className="min-w-0 rounded border border-stone-700 bg-[#202020] p-2">
            <p className="text-[10px] font-semibold uppercase tracking-wide text-stone-500">{label}</p>
            <p className="mt-1 whitespace-pre-wrap break-words font-mono text-xs text-stone-100">{String(value)}</p>
          </div>
        ))}
      </div>
    </div>
  );
}
