import { ChevronDown, ChevronRight, Database, Download, RefreshCcw, TerminalSquare, XCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { downloadBlob, downloadJSON } from "../../../lib/api";
import {
  cleanSQLIdentifier,
  normalizeConnectorOutput,
  normalizeSQLName,
  referencedTablesFromSQL,
  tableMatchesReference,
  tableReferenceKey,
} from "./sql-console-data";

export function SQLNoSessionPlaceholder({ config, target, theme, onNewSession }) {
  const light = theme === "light";
  return (
    <div className={`grid h-full min-h-0 place-items-center p-6 ${light ? "text-stone-700" : "text-stone-200"}`}>
      <div className="grid max-w-md gap-4 text-center">
        <div
          className={`mx-auto flex h-12 w-12 items-center justify-center rounded-full border ${light ? "border-stone-200 bg-stone-100" : "border-stone-600 bg-stone-800"}`}
        >
          <TerminalSquare className={`h-6 w-6 ${light ? "text-stone-600" : "text-stone-300"}`} />
        </div>
        <div className="grid gap-2">
          <h3 className={`text-base font-semibold ${light ? "text-stone-950" : "text-white"}`}>No active {config.label} session</h3>
          <p className={`text-sm leading-6 ${light ? "text-stone-600" : "text-stone-400"}`}>
            Start a {config.label} session before running SQL against {target.name}.
          </p>
        </div>
        <Button type="button" className="mx-auto" onClick={() => onNewSession?.()}>
          <RefreshCcw className="h-4 w-4" />
          New Session
        </Button>
      </div>
    </div>
  );
}

export function SQLEndpointFooter({ config, target, borderClass, mutedClass }) {
  return (
    <div className={`border-t px-4 py-2 text-xs ${borderClass} ${mutedClass}`}>
      <span className="inline-flex min-w-0 items-center gap-2">
        <Database className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{config.targetEndpoint(target)}</span>
      </span>
    </div>
  );
}

export function SQLEditor({ value, onChange, onSubmit, focusSignal, theme, tables, keywords, disabled }) {
  const containerRef = useRef(null);
  const editorRef = useRef(null);
  const changeRef = useRef(null);
  const providerRef = useRef(null);
  const submitRef = useRef(onSubmit);
  const onChangeRef = useRef(onChange);
  const tablesRef = useRef(tables);
  const keywordsRef = useRef(keywords);
  const initialValueRef = useRef(value);
  const initialThemeRef = useRef(theme);
  const initialDisabledRef = useRef(disabled);
  const [monaco, setMonaco] = useState(null);

  useEffect(() => {
    submitRef.current = onSubmit;
  }, [onSubmit]);

  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  useEffect(() => {
    tablesRef.current = tables;
  }, [tables]);

  useEffect(() => {
    keywordsRef.current = keywords;
  }, [keywords]);

  useEffect(() => {
    let canceled = false;
    loadMonaco().then((monacoInstance) => {
      if (canceled || !containerRef.current) return;
      setMonaco(monacoInstance);
      providerRef.current = monacoInstance.languages.registerCompletionItemProvider("sql", {
        triggerCharacters: [".", " ", '"'],
        provideCompletionItems(model, position) {
          return { suggestions: sqlCompletionItems(monacoInstance, tablesRef.current, keywordsRef.current, model, position) };
        },
      });
      const editor = monacoInstance.editor.create(containerRef.current, {
        value: initialValueRef.current || "",
        language: "sql",
        theme: sqlEditorTheme(monacoInstance, initialThemeRef.current),
        minimap: { enabled: false },
        automaticLayout: true,
        scrollBeyondLastLine: false,
        wordWrap: "on",
        quickSuggestions: { other: true, comments: false, strings: false },
        quickSuggestionsDelay: 40,
        suggestOnTriggerCharacters: true,
        wordBasedSuggestions: "off",
        tabCompletion: "on",
        acceptSuggestionOnEnter: "on",
        acceptSuggestionOnCommitCharacter: true,
        fixedOverflowWidgets: true,
        suggest: {
          showWords: false,
          snippetsPreventQuickSuggestions: false,
          selectionMode: "always",
        },
        fontSize: 12,
        lineHeight: 18,
        lineNumbers: "on",
        glyphMargin: false,
        folding: false,
        lineDecorationsWidth: 8,
        lineNumbersMinChars: 3,
        overviewRulerLanes: 0,
        hideCursorInOverviewRuler: true,
        renderLineHighlight: "none",
        tabSize: 2,
        readOnly: initialDisabledRef.current,
        domReadOnly: initialDisabledRef.current,
        padding: { top: 8, bottom: 8 },
      });
      editorRef.current = editor;
      editor.addCommand(monacoInstance.KeyMod.CtrlCmd | monacoInstance.KeyCode.Enter, () => submitRef.current?.());
      changeRef.current = editor.onDidChangeModelContent(() => {
        onChangeRef.current(editor.getValue());
      });
    });
    return () => {
      canceled = true;
      providerRef.current?.dispose();
      changeRef.current?.dispose();
      editorRef.current?.dispose();
      providerRef.current = null;
      changeRef.current = null;
      editorRef.current = null;
    };
  }, []);

  useEffect(() => {
    const editor = editorRef.current;
    if (!editor || editor.getValue() === value) return;
    editor.setValue(value || "");
  }, [value]);

  useEffect(() => {
    if (!monaco) return;
    monaco.editor.setTheme(sqlEditorTheme(monaco, theme));
  }, [monaco, theme]);

  useEffect(() => {
    editorRef.current?.updateOptions({ readOnly: disabled, domReadOnly: disabled });
  }, [disabled]);

  useEffect(() => {
    if (!focusSignal) return;
    window.setTimeout(() => editorRef.current?.focus(), 0);
  }, [focusSignal]);

  return (
    <div
      ref={containerRef}
      className={`min-h-28 overflow-visible rounded-md border ${theme === "light" ? "border-stone-300 bg-stone-50" : "border-stone-700 bg-[#252526]"}`}
    />
  );
}

export function SQLConnectorToolbarActions({ label, theme, structuredSession, onNewStructuredSession, onEndStructuredSession }) {
  const buttonClass = `h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`;
  const active = Boolean(structuredSession?.active);
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        className={buttonClass}
        onClick={onNewStructuredSession}
        disabled={active}
        title={`Start a fresh ${label} activity session`}
      >
        <RefreshCcw className="h-3.5 w-3.5" />
        New Session
      </Button>
      <Button
        type="button"
        variant="ghost"
        className={buttonClass}
        onClick={onEndStructuredSession}
        disabled={!active}
        title={`End the current ${label} activity session`}
      >
        <XCircle className="h-3.5 w-3.5" />
        End Session
      </Button>
    </>
  );
}

export function ResultViewToggle({ checked, onChange, theme }) {
  const light = theme === "light";
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      className={`inline-flex shrink-0 items-center gap-2 rounded-full border px-2 py-1 text-xs font-semibold transition ${
        checked
          ? "border-emerald-600 bg-emerald-950 text-emerald-50"
          : light
            ? "border-stone-300 bg-white text-stone-600 hover:bg-stone-100"
            : "border-stone-700 bg-stone-900 text-stone-300 hover:bg-stone-800"
      }`}
      onClick={() => onChange(!checked)}
    >
      <span>Result View</span>
      <span className={`relative h-4 w-7 rounded-full transition ${checked ? "bg-emerald-500" : light ? "bg-stone-300" : "bg-stone-700"}`}>
        <span className={`absolute top-0.5 h-3 w-3 rounded-full bg-white transition ${checked ? "left-3.5" : "left-0.5"}`} />
      </span>
    </button>
  );
}

export function SQLSchemaBrowser({
  rows,
  search,
  onSearch,
  onPrepareQuery,
  metadata,
  theme,
  inputClass,
  mutedClass,
  hoverClass,
  namespaceLabel,
}) {
  const [expandedTables, setExpandedTables] = useState({});
  const grouped = groupTableBrowserRows(rows);
  function toggleTable(table) {
    const key = tableBrowserKey(table);
    setExpandedTables((current) => ({ ...current, [key]: !current[key] }));
  }
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2 p-3">
      <input
        type="search"
        className={`h-9 rounded-md border px-3 text-sm outline-none ${inputClass}`}
        value={search}
        onChange={(event) => onSearch(event.target.value)}
        placeholder={`Search ${namespaceLabel.toLowerCase()}s or tables`}
      />
      <div className="min-h-0 overflow-y-auto">
        {metadata.state === "loading" ? (
          <p className={`px-1 py-3 text-sm ${mutedClass}`}>Loading {namespaceLabel.toLowerCase()} metadata...</p>
        ) : null}
        {metadata.state === "error" ? (
          <Notice tone="bad">{metadata.error || `${namespaceLabel} metadata could not be loaded.`}</Notice>
        ) : null}
        {metadata.state !== "loading" && grouped.length === 0 ? (
          <p className={`px-1 py-3 text-sm ${mutedClass}`}>No tables found for this profile.</p>
        ) : null}
        {grouped.map((group) => (
          <div key={group.schema} className="mb-3">
            <p className={`mb-1 truncate px-1 text-[11px] font-semibold uppercase tracking-wide ${mutedClass}`}>{group.schema}</p>
            <div className={`overflow-hidden rounded-md border ${theme === "light" ? "border-stone-200" : "border-stone-700"}`}>
              {group.tables.map((table) => {
                const key = tableBrowserKey(table);
                const expanded = Boolean(expandedTables[key]);
                const columns = table.columns || [];
                return (
                  <div key={key} className={`border-b last:border-b-0 ${theme === "light" ? "border-stone-100" : "border-stone-800"}`}>
                    <div className={`grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 px-2 py-1.5 transition ${hoverClass}`}>
                      <button
                        type="button"
                        className="flex min-w-0 items-start gap-2 text-left"
                        onClick={() => toggleTable(table)}
                        title={`${expanded ? "Hide" : "Show"} columns for ${table.schema}.${table.table}`}
                      >
                        <span className="mt-0.5 shrink-0">
                          {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                        </span>
                        <span className="min-w-0">
                          <span className="block truncate font-mono text-xs font-semibold">{table.table}</span>
                          <span className={`block truncate text-[11px] ${mutedClass}`}>
                            {table.columnCount} column{table.columnCount === 1 ? "" : "s"}
                          </span>
                        </span>
                      </button>
                      <Button
                        type="button"
                        variant="outline"
                        className="h-7 w-7 px-0"
                        title={`Prepare SELECT query for ${table.schema}.${table.table}`}
                        onClick={() => onPrepareQuery(table)}
                      >
                        <Database className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                    {expanded ? (
                      <div className={`grid gap-1 px-8 pb-2 text-[11px] ${mutedClass}`}>
                        {columns.length > 0 ? (
                          columns.map((column) => (
                            <div
                              className="grid grid-cols-[minmax(0,1fr)_auto] gap-2 rounded px-2 py-1 font-mono"
                              key={`${key}.${column.name}`}
                            >
                              <span className="truncate">{column.name}</span>
                              {column.dataType ? <span className="truncate opacity-75">{column.dataType}</span> : null}
                            </div>
                          ))
                        ) : (
                          <span className="rounded px-2 py-1">No column metadata loaded.</span>
                        )}
                      </div>
                    ) : null}
                  </div>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

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
  const columns = Array.isArray(normalized?.columns) ? normalized.columns.map((item) => String(item)) : [];
  const rows = Array.isArray(normalized?.rows) ? normalized.rows : [];
  const tableText = rowsToClipboardText(columns, rows);
  const csvText = rowsToCSVText(columns, rows);
  const jsonValue = normalized || value || {};
  if (columns.length > 0) {
    return (
      <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-xs font-semibold uppercase text-stone-500">{title}</p>
          <div className="flex flex-wrap justify-end gap-2">
            <CopyButton
              value={tableText}
              variant="outline"
              className="h-8 px-2 text-xs"
              iconClassName="h-3.5 w-3.5"
              title="Copy rows as TSV"
            >
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

export function ActivityStatusBadge({ status }) {
  const tone =
    status === "completed"
      ? "good"
      : status === "failed" || status === "error" || status === "stale" || status === "outcome_unknown"
        ? "bad"
        : status === "approval_pending" || status === "running"
          ? "warn"
          : "neutral";
  return <Badge tone={tone}>{status}</Badge>;
}

export function formatConnectorTime(value) {
  if (!value) return "-";
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(value));
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
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function rowsToClipboardText(columns, rows) {
  const lines = [columns.join("\t")];
  for (const row of rows) {
    lines.push(columns.map((column) => formatCell(row?.[column]).replaceAll("\t", " ")).join("\t"));
  }
  return lines.join("\n");
}

function rowsToCSVText(columns, rows) {
  const lines = [columns.map(csvCell).join(",")];
  for (const row of rows) {
    lines.push(columns.map((column) => csvCell(formatCell(row?.[column]))).join(","));
  }
  return lines.join("\n");
}

function csvCell(value) {
  const text = String(value ?? "");
  if (/[",\n\r]/.test(text)) {
    return `"${text.replaceAll('"', '""')}"`;
  }
  return text;
}

function downloadText(text, filename, type) {
  downloadBlob(new Blob([text], { type }), filename);
}

export function normalizeSQLConsoleConfig(config = {}) {
  const label = String(config.label || "SQL").trim() || "SQL";
  const defaultPort = Number(config.defaultPort) || 0;
  const defaultDatabase = String(config.defaultDatabase || "database");
  return {
    label,
    queryAction: String(config.queryAction || "query_readonly"),
    describeAction: String(config.describeAction || "describe_table"),
    metadataSQL: String(config.metadataSQL || ""),
    metadataMaxRows: Math.max(1, Number(config.metadataMaxRows) || 5000),
    metadataReason: String(config.metadataReason || `load ${label} console autocomplete`),
    manualReason: String(config.manualReason || `manual ${label} console query`),
    browserLabel: String(config.browserLabel || "Schema"),
    filenamePrefix: String(config.filenamePrefix || `${label.toLowerCase()}-result`),
    keywords: [...new Set([...DEFAULT_SQL_KEYWORDS, ...(config.keywords || [])].map((item) => String(item).toLowerCase()))],
    targetEndpoint:
      config.targetEndpoint ||
      ((target) => {
        if (!target) return "-";
        const host = target.config?.host || "host";
        const port = target.config?.port || defaultPort || "port";
        const database = target.config?.database || defaultDatabase;
        return `${host}:${port}/${database}`;
      }),
    tableQuery:
      config.tableQuery ||
      ((table, maxRows) => `SELECT *\nFROM ${quoteSQLIdentifier(table.schema)}.${quoteSQLIdentifier(table.table)}\nLIMIT ${maxRows};`),
    describeInput:
      config.describeInput ||
      ((reference) => ({
        schema: reference.schema || "",
        table: reference.table,
      })),
  };
}

export function metadataStatusText(metadata, config) {
  if (metadata.state === "loading") return "Loading metadata suggestions for autocomplete...";
  if (metadata.state === "error") return `Autocomplete metadata unavailable: ${metadata.error}`;
  if (metadata.state === "ready") {
    return metadata.tables.length > 0
      ? `${metadata.tables.length} metadata suggestion${metadata.tables.length === 1 ? "" : "s"} loaded${metadata.truncated ? "; metadata limit reached" : ""}. Run bounded read-only ${config.label} SQL through this credential profile.`
      : "No metadata suggestions found. Run bounded read-only SQL through this credential profile.";
  }
  return "Run bounded read-only SQL through this credential profile.";
}

export function recentSQLQueries(items, config) {
  const seen = new Set();
  return [...(items || [])]
    .filter((item) => item?.action_name === config.queryAction && !isAutocompleteMetadataRequest(item, config.metadataReason))
    .sort((left, right) => safeTimestamp(right.created_at) - safeTimestamp(left.created_at))
    .map((item) => ({ id: item.id, sql: actionInputSQL(item), createdAt: item.created_at }))
    .filter((item) => {
      if (!item.sql || seen.has(item.sql)) return false;
      seen.add(item.sql);
      return true;
    })
    .slice(0, 10)
    .map((item) => ({ ...item, preview: sqlPreview(item.sql) }));
}

function sqlPreview(sql) {
  const compact = String(sql || "")
    .replace(/\s+/g, " ")
    .trim();
  if (compact.length <= 64) return compact;
  return `${compact.slice(0, 61)}...`;
}

export function actionInputSQL(item) {
  const input = typeof item?.input === "string" ? parseJSON(item.input) : item?.input;
  return String(input?.sql || "").trim();
}

function safeTimestamp(value) {
  const parsed = Date.parse(value || "");
  return Number.isFinite(parsed) ? parsed : 0;
}

export function tableBrowserSummary(metadata, rows, browserLabel) {
  if (metadata.state === "loading") return `Loading visible ${browserLabel.toLowerCase()}s and tables...`;
  if (metadata.state === "error") return `${browserLabel} metadata is unavailable. You can still run read-only SQL.`;
  if (rows.length === 0) return "No visible tables found for this profile.";
  return `${rows.length} visible table${rows.length === 1 ? "" : "s"}${metadata.truncated ? "; metadata limit reached" : ""}. Select one to prepare a read-only query.`;
}

export function mergeMetadataRows(current, incoming) {
  const merged = [];
  const seen = new Set();
  for (const item of [...(current || []), ...(incoming || [])]) {
    const key = [
      normalizeSQLName(item.schema),
      normalizeSQLName(item.table),
      normalizeSQLName(item.column),
      normalizeSQLName(item.dataType || item.type),
      item.position || "",
    ].join(".");
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(item);
  }
  return merged;
}

export function filteredTableBrowserRows(rows, search) {
  const terms = normalizeSQLName(search).split(/\s+/).filter(Boolean);
  const tables = uniqueTableBrowserRows(rows || []);
  if (terms.length === 0) return tables;
  return tables.filter((row) => {
    const haystack = normalizeSQLName(`${row.schema} ${row.table}`);
    return terms.every((term) => haystack.includes(term));
  });
}

function uniqueTableBrowserRows(rows) {
  const byTable = new Map();
  const seenColumns = new Set();
  for (const row of rows) {
    if (!row.schema || !row.table) continue;
    const key = `${normalizeSQLName(row.schema)}.${normalizeSQLName(row.table)}`;
    const current = byTable.get(key) || {
      schema: row.schema,
      table: row.table,
      type: row.type || "table",
      columnCount: 0,
      columns: [],
    };
    if (row.column) {
      const columnKey = `${key}.${normalizeSQLName(row.column)}`;
      if (!seenColumns.has(columnKey)) {
        seenColumns.add(columnKey);
        current.columns.push({ name: row.column, dataType: row.dataType || "", position: row.position || current.columns.length + 1 });
      }
    }
    byTable.set(key, current);
  }
  return Array.from(byTable.values())
    .map((table) => ({
      ...table,
      columnCount: (table.columns || []).length,
      columns: [...(table.columns || [])].sort((a, b) => (a.position || 0) - (b.position || 0) || a.name.localeCompare(b.name)),
    }))
    .sort((a, b) => {
      const schemaCompare = a.schema.localeCompare(b.schema);
      return schemaCompare || a.table.localeCompare(b.table);
    });
}

function parseJSON(value) {
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
}

function tableBrowserKey(table) {
  return `${normalizeSQLName(table.schema)}.${normalizeSQLName(table.table)}`;
}

function groupTableBrowserRows(rows) {
  const bySchema = new Map();
  for (const row of rows) {
    const schema = row.schema || "public";
    const group = bySchema.get(schema) || { schema, tables: [] };
    group.tables.push(row);
    bySchema.set(schema, group);
  }
  return Array.from(bySchema.values());
}

export function isAutocompleteMetadataRequest(item, metadataReason) {
  return item?.reason === metadataReason;
}

let monacoPromise = null;

function loadMonaco() {
  if (!monacoPromise) {
    monacoPromise = import("monaco-editor/esm/vs/editor/editor.worker?worker").then((workerModule) => {
      if (typeof window !== "undefined") {
        window.MonacoEnvironment = {
          getWorker() {
            return new workerModule.default();
          },
        };
      }
      return Promise.all([
        import("monaco-editor/esm/vs/basic-languages/sql/sql.contribution"),
        import("monaco-editor/esm/vs/editor/contrib/suggest/browser/suggestController.js"),
        import("monaco-editor/esm/vs/editor/editor.api"),
      ]).then(([, , monaco]) => monaco);
    });
  }
  return monacoPromise;
}

function sqlEditorTheme(monaco, theme) {
  const dark = theme !== "light";
  const name = dark ? "aipermission-sql-dark" : "aipermission-sql-light";
  monaco.editor.defineTheme(name, {
    base: dark ? "vs-dark" : "vs",
    inherit: true,
    rules: [],
    colors: {
      "editor.background": dark ? "#252526" : "#fafaf9",
      "editorGutter.background": dark ? "#252526" : "#fafaf9",
      "editorLineNumber.foreground": dark ? "#78716c" : "#a8a29e",
      "editorCursor.foreground": dark ? "#e7e5e4" : "#1c1917",
      "editor.selectionBackground": dark ? "#064e3b" : "#bbf7d0",
      editorLineHighlightBorder: "#00000000",
      editorLineHighlightBackground: "#00000000",
      "editorIndentGuide.background1": "#00000000",
      "editorIndentGuide.activeBackground1": "#00000000",
      "editorSuggestWidget.background": dark ? "#252526" : "#ffffff",
      "editorSuggestWidget.border": dark ? "#44403c" : "#d6d3d1",
      "editorSuggestWidget.foreground": dark ? "#e7e5e4" : "#292524",
      "editorSuggestWidget.selectedBackground": dark ? "#064e3b" : "#dcfce7",
      "editorSuggestWidget.highlightForeground": dark ? "#6ee7b7" : "#047857",
    },
  });
  return name;
}

const DEFAULT_SQL_KEYWORDS = [
  "select",
  "from",
  "where",
  "join",
  "left join",
  "inner join",
  "group by",
  "order by",
  "limit",
  "with",
  "explain",
  "show",
  "count",
  "distinct",
  "having",
  "union",
  "case",
  "when",
  "then",
  "else",
  "end",
  "true",
  "false",
  "null",
];

function sqlCompletionItems(monaco, tables, keywords, model, position) {
  const word = model.getWordUntilPosition(position);
  const range = {
    startLineNumber: position.lineNumber,
    endLineNumber: position.lineNumber,
    startColumn: word.startColumn,
    endColumn: word.endColumn,
  };
  const suggestions = keywords.map((keyword) => ({
    label: keyword.toUpperCase(),
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: keyword,
    sortText: `2_${keyword}`,
    range,
  }));
  const seenSchemas = new Set();
  const seenTables = new Set();
  const tableReferences = referencedTablesFromSQL(model.getValue());
  const dotReference = dotReferenceBeforePosition(model, position);
  const columnReferences = dotReference ? matchingReferencesForQualifier(dotReference, tableReferences, tables) : tableReferences;
  const inTableContext = isTableCompletionContext(model, position);
  const seenColumns = new Set();
  for (const item of tables || []) {
    if (item.schema && !seenSchemas.has(item.schema)) {
      seenSchemas.add(item.schema);
      suggestions.push({
        label: item.schema,
        kind: monaco.languages.CompletionItemKind.Module,
        insertText: quoteSQLIdentifier(item.schema),
        detail: "schema",
        sortText: `1_schema_${item.schema}`,
        range,
      });
    }
    const tableKey = `${item.schema}.${item.table}`;
    if (!seenTables.has(tableKey)) {
      seenTables.add(tableKey);
      suggestions.push({
        label: item.table,
        kind: monaco.languages.CompletionItemKind.Class,
        insertText: quoteSQLIdentifier(item.table),
        detail: item.schema,
        documentation: item.type || "table",
        sortText: `0_table_${item.table}`,
        range,
      });
      suggestions.push({
        label: tableKey,
        kind: monaco.languages.CompletionItemKind.Class,
        insertText: `${quoteSQLIdentifier(item.schema)}.${quoteSQLIdentifier(item.table)}`,
        detail: item.type || "table",
        sortText: `0_full_${tableKey}`,
        range,
      });
    }
    if (!inTableContext && item.column && columnReferences.some((reference) => tableMatchesReference(item, reference))) {
      const columnKey = `${tableKey}.${item.column}`;
      if (!seenColumns.has(columnKey)) {
        seenColumns.add(columnKey);
        suggestions.push({
          label: item.column,
          kind: monaco.languages.CompletionItemKind.Field,
          insertText: quoteSQLIdentifier(item.column),
          detail: `${tableKey}${item.dataType ? ` / ${item.dataType}` : ""}`,
          sortText: `0_column_${item.column}_${columnKey}`,
          range,
        });
      }
    }
  }
  return suggestions;
}

function matchingReferencesForQualifier(qualifier, references, metadataRows) {
  const normalized = normalizeSQLName(qualifier);
  const matches = references.filter(
    (reference) => normalizeSQLName(reference.alias) === normalized || normalizeSQLName(reference.table) === normalized,
  );
  if (matches.length > 0) return matches;
  const metadataMatches = [];
  const seen = new Set();
  for (const item of metadataRows || []) {
    if (normalizeSQLName(item.table) !== normalized) continue;
    const reference = { schema: item.schema || "", table: item.table || "", alias: "" };
    const key = tableReferenceKey(reference);
    if (seen.has(key)) continue;
    seen.add(key);
    metadataMatches.push(reference);
  }
  return metadataMatches;
}

function dotReferenceBeforePosition(model, position) {
  const prefix = model.getLineContent(position.lineNumber).slice(0, position.column - 1);
  const match = prefix.match(/((?:"[^"]+"|`[^`]+`|[a-zA-Z_][\w$]*))\.\s*(?:"[^"]*"|`[^`]*`|[a-zA-Z_][\w$]*)?$/);
  return match ? cleanSQLIdentifier(match[1]) : "";
}

function isTableCompletionContext(model, position) {
  const prefix = model
    .getLineContent(position.lineNumber)
    .slice(0, position.column - 1)
    .toLowerCase();
  return /\b(from|join)\s+(?:"[^"]*"|`[^`]*`|[a-z_][\w$]*)?$/i.test(prefix);
}

function quoteSQLIdentifier(value) {
  if (/^[a-z_][a-z0-9_]*$/.test(value)) return value;
  return `"${String(value).replaceAll('"', '""')}"`;
}
