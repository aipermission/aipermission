import { Download } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { downloadBlob, downloadJSON } from "../../../lib/api";
import { normalizeConnectorOutput } from "./sql-console-data";

export function ActivityBlock({ title, value }) {
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
      <p className="text-xs font-semibold uppercase text-stone-500">{title}</p>
      <TerminalBlock className="min-h-0 overflow-auto text-xs">{formatJSON(value)}</TerminalBlock>
    </div>
  );
}

export function SQLOutputBlock({ title, value, theme, filenamePrefix }) {
  const normalized = normalizeConnectorOutput(value);
  const columns = Array.isArray(normalized?.columns) ? normalized.columns.map(String) : [];
  const rows = Array.isArray(normalized?.rows) ? normalized.rows : [];
  if (columns.length === 0) return <JSONResult title={title} value={value} filenamePrefix={filenamePrefix} />;
  const jsonValue = normalized || value || {};
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
      <ResultActions title={title} columns={columns} rows={rows} jsonValue={jsonValue} filenamePrefix={filenamePrefix} />
      <div
        className={`min-h-0 overflow-auto rounded-md border font-mono text-xs ${theme === "light" ? "border-stone-200 bg-white" : "border-stone-700 bg-[#1a1a1a]"}`}
      >
        <table className="min-w-full border-separate border-spacing-0 select-text">
          <thead className={theme === "light" ? "bg-stone-100 text-stone-600" : "bg-stone-900 text-stone-300"}>
            <tr>
              {columns.map((column) => (
                <th
                  key={column}
                  className={`sticky top-0 border-b px-3 py-2 text-left font-semibold ${theme === "light" ? "border-stone-200 bg-stone-100" : "border-stone-700 bg-stone-900"}`}
                >
                  {column}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, rowIndex) => (
              <tr key={rowIndex} className={theme === "light" ? "odd:bg-white even:bg-stone-50" : "odd:bg-[#1a1a1a] even:bg-[#202020]"}>
                {columns.map((column) => (
                  <td
                    key={column}
                    className={`max-w-[420px] whitespace-pre-wrap border-b px-3 py-2 align-top ${theme === "light" ? "border-stone-100 text-stone-900" : "border-stone-800 text-stone-100"}`}
                  >
                    {formatCell(row?.[column])}
                  </td>
                ))}
              </tr>
            ))}
            {rows.length === 0 ? (
              <tr>
                <td className="px-3 py-4 text-stone-500" colSpan={Math.max(columns.length, 1)}>
                  No rows returned.
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ResultActions({ title, columns, rows, jsonValue, filenamePrefix }) {
  const tableText = rowsToClipboardText(columns, rows);
  const csvText = rowsToCSVText(columns, rows);
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <p className="text-xs font-semibold uppercase text-stone-500">{title}</p>
      <div className="flex flex-wrap justify-end gap-2">
        <CopyButton value={tableText} variant="outline" className="h-8 px-2 text-xs" iconClassName="h-3.5 w-3.5" title="Copy rows as TSV">
          TSV
        </CopyButton>
        <CopyButton
          value={formatJSON(jsonValue)}
          variant="outline"
          className="h-8 px-2 text-xs"
          iconClassName="h-3.5 w-3.5"
          title="Copy result JSON"
        >
          JSON
        </CopyButton>
        <Button
          type="button"
          variant="outline"
          className="h-8 px-2 text-xs"
          title="Download rows as CSV"
          onClick={() => downloadText(csvText, `${filenamePrefix}.csv`, "text/csv")}
        >
          <Download className="h-3.5 w-3.5" />
          CSV
        </Button>
        <Button
          type="button"
          variant="outline"
          className="h-8 px-2 text-xs"
          title="Download result JSON"
          onClick={() => downloadJSON(jsonValue, `${filenamePrefix}.json`)}
        >
          <Download className="h-3.5 w-3.5" />
          JSON
        </Button>
      </div>
    </div>
  );
}

function JSONResult({ title, value, filenamePrefix }) {
  const jsonText = formatJSON(value);
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs font-semibold uppercase text-stone-500">{title}</p>
        <div className="flex justify-end gap-2">
          <CopyButton value={jsonText} variant="outline" className="h-8 px-2 text-xs" iconClassName="h-3.5 w-3.5" title="Copy JSON">
            JSON
          </CopyButton>
          <Button
            type="button"
            variant="outline"
            className="h-8 px-2 text-xs"
            title="Download JSON"
            onClick={() => downloadJSON(value || {}, `${filenamePrefix}.json`)}
          >
            <Download className="h-3.5 w-3.5" />
            JSON
          </Button>
        </div>
      </div>
      <TerminalBlock className="min-h-0 overflow-auto text-xs">{jsonText}</TerminalBlock>
    </div>
  );
}

function formatJSON(value) {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value ?? {}, null, 2);
  } catch {
    return String(value);
  }
}

function formatCell(value) {
  if (value === null || value === undefined) return "NULL";
  return typeof value === "object" ? JSON.stringify(value) : String(value);
}

function rowsToClipboardText(columns, rows) {
  return [
    columns.join("\t"),
    ...rows.map((row) => columns.map((column) => formatCell(row?.[column]).replaceAll("\t", " ")).join("\t")),
  ].join("\n");
}

function rowsToCSVText(columns, rows) {
  return [columns.map(csvCell).join(","), ...rows.map((row) => columns.map((column) => csvCell(formatCell(row?.[column]))).join(","))].join(
    "\n",
  );
}

function csvCell(value) {
  const text = String(value ?? "");
  return /[",\n\r]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

function downloadText(text, filename, type) {
  downloadBlob(new Blob([text], { type }), filename);
}
