import { useState } from "react";
import { normalizeConnectorOutput } from "./sql-console-data";
import { SQLOutputBlock } from "./sql-result-output";

type ExecutionOutputProps = { value: unknown; theme: "dark" | "light"; filenamePrefix: string };

export function SQLExecutionOutput({ value, theme, filenamePrefix }: ExecutionOutputProps) {
  const output = normalizeConnectorOutput(value);
  const results = Array.isArray(output.result_sets) ? output.result_sets : [];
  const commands = Array.isArray(output.commands) ? output.commands.map(normalizeConnectorOutput) : [];
  const notices = Array.isArray(output.notices) ? output.notices.map(String) : [];
  const [selection, setSelection] = useState<number | "summary">(0);
  const selected = typeof selection === "number" && selection < results.length ? selection : "summary";
  const result = selected === "summary" ? output : normalizeConnectorOutput(results[selected]);
  return (
    <div className="flex h-full min-w-0 flex-col gap-2">
      <div className="flex min-w-0 shrink-0 flex-wrap items-center gap-2 text-xs" aria-label="Execution summary">
        <span>
          {String(output.row_count ?? "-")} {output.row_count === 1 ? "row" : "rows"} returned
        </span>
        <span>{String(output.result_set_count ?? "-")} result sets</span>
        <span>
          {String(output.completed_batches ?? "-")} completed {output.completed_batches === 1 ? "batch" : "batches"}
        </span>
        {output.truncated === true ? <span className="text-amber-600">Returned data truncated</span> : null}
        <span>
          Row cap: {String(output.max_rows ?? "-")}; byte cap: {String(output.max_bytes ?? "-")}
        </span>
        <span>Transactions: {String(output.transaction_control ?? "-")}</span>
        {output.retry_safe === false ? <span>Not retry-safe</span> : null}
      </div>
      <div className="grid min-w-0 shrink-0 gap-2">
        {commands.length || notices.length ? (
          <details className="min-w-0 text-xs" open={results.length === 0}>
            <summary className="cursor-pointer">
              Commands ({commands.length}) / Notices ({notices.length})
            </summary>
            <ul className="mt-1 max-h-24 overflow-auto break-words font-mono">
              {commands.map((command, index) => (
                <li key={`command-${index}`}>
                  {String(command.tag ?? "")} / Reported rows: {String(command.reported_rows ?? "-")}
                </li>
              ))}
              {notices.map((notice, index) => (
                <li key={`notice-${index}`}>{notice}</li>
              ))}
            </ul>
          </details>
        ) : null}
        <label className="flex min-w-0 flex-wrap items-center gap-2 text-xs font-semibold">
          Result
          <select
            className={`h-8 min-w-0 max-w-full rounded-md border px-2 ${theme === "light" ? "border-stone-300 bg-white" : "border-stone-700 bg-stone-900"}`}
            value={selected}
            onChange={(event) => setSelection(event.target.value === "summary" ? "summary" : Number(event.target.value))}
          >
            {results.map((value, index) => {
              const item = normalizeConnectorOutput(value);
              return (
                <option key={index} value={index}>
                  Result {index + 1} ({String(item.row_count ?? "-")} {item.row_count === 1 ? "row" : "rows"}
                  {item.truncated === true ? ", truncated" : ""})
                </option>
              );
            })}
            <option value="summary">Full execution JSON</option>
          </select>
        </label>
      </div>
      <div className="min-h-48 flex-1">
        <SQLOutputBlock
          title={selected === "summary" ? "Execution" : `Result ${selected + 1}`}
          value={result}
          theme={theme}
          filenamePrefix={selected === "summary" ? filenamePrefix : `${filenamePrefix}-result-${selected + 1}`}
        />
      </div>
    </div>
  );
}
