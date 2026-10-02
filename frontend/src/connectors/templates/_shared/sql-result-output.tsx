import { Download } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { downloadBlob, downloadJSON } from "../../../lib/api";
import { normalizeConnectorOutput } from "./sql-console-data";

type ActivityBlockProps = { title: string; value: unknown };
type SQLRow = Record<string, unknown>;
type ResultExportProps = ActivityBlockProps & { filenamePrefix: string };
type ResultActionsProps = {
  title: string;
  columns: string[];
  rows: SQLRow[];
  jsonValue: unknown;
  filenamePrefix: string;
};

export function ActivityBlock({ title, value }: ActivityBlockProps) {
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
      <p className="text-xs font-semibold uppercase text-stone-500">{title}</p>
      <TerminalBlock className="min-h-0 overflow-auto text-xs">{formatJSON(value)}</TerminalBlock>
    </div>
  );
}

export function SQLOutputBlock({ title, value, theme, filenamePrefix }: ResultExportProps & { theme: "dark" | "light" }) {
  const normalized = normalizeConnectorOutput(value);
  const columns = Array.isArray(normalized?.columns) ? normalized.columns.map(String) : [];
  const rows = Array.isArray(normalized?.rows) ? normalized.rows.map(normalizeConnectorOutput) : [];
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

function ResultActions({ title, columns, rows, jsonValue, filenamePrefix }: ResultActionsProps) {
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

function JSONResult({ title, value, filenamePrefix }: ResultExportProps) {
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

function formatJSON(value: unknown, space = 2): string {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value ?? {}, null, space) ?? String(value);
  } catch {
    return String(value);
  }
}

function formatCell(value: unknown): string {
  if (value === null || value === undefined) return "NULL";
  return typeof value === "object" ? formatJSON(value, 0) : String(value);
}

function rowsToClipboardText(columns: string[], rows: SQLRow[]): string {
  return [columns.map(tsvCell).join("\t"), ...rows.map((row) => columns.map((column) => tsvCell(row?.[column])).join("\t"))].join("\n");
}

function rowsToCSVText(columns: string[], rows: SQLRow[]): string {
  return [columns.map(csvCell).join(","), ...rows.map((row) => columns.map((column) => csvCell(row?.[column])).join(","))].join("\n");
}

function csvCell(value: unknown): string {
  const text = spreadsheetCell(value);
  return /[",\n\r]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

function tsvCell(value: unknown): string {
  return spreadsheetCell(value).replace(/[\t\r\n]/g, " ");
}

function spreadsheetCell(value: unknown): string {
  const text = formatCell(value);
  // Spreadsheet text exports must not turn untrusted strings into formulas.
  if (typeof value === "number") return text;
  let start = 0;
  while (start < text.length && (text.charCodeAt(start) <= 31 || /[\s\p{White_Space}]/u.test(text[start]))) start += 1;
  return start < text.length && "=+-@".includes(text[start]) ? `'${text}` : text;
}

function downloadText(text: string, filename: string, type: string): void {
  downloadBlob(new Blob([text], { type }), filename);
}
