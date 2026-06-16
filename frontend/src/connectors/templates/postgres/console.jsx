import { Database, RefreshCcw, TerminalSquare, XCircle } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { apiPost } from "../../../lib/api";

export function PostgresConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const [selectedID, setSelectedID] = useState(null);
  const [sql, setSQL] = useState("");
  const [maxRows, setMaxRows] = useState(100);
  const [runState, setRunState] = useState({ state: "idle", error: "" });
  const [metadata, setMetadata] = useState({ state: "idle", tables: [], error: "" });
  const [editorFocusTick, setEditorFocusTick] = useState(0);
  const [resultView, setResultView] = useState(false);
  const metadataSessionRef = useRef("");
  const metadataRowsRef = useRef([]);
  const columnMetadataRequestsRef = useRef(new Set());
  const panelClass = theme === "light" ? "bg-white text-stone-900" : "bg-[#1e1e1e] text-stone-100";
  const mutedClass = theme === "light" ? "text-stone-500" : "text-stone-400";
  const borderClass = theme === "light" ? "border-stone-200" : "border-stone-700";
  const subtlePanelClass = theme === "light" ? "bg-stone-50" : "bg-[#252526]";
  const inputClass = theme === "light" ? "border-stone-300 bg-white text-stone-900 placeholder:text-stone-400" : "border-stone-700 bg-[#1a1a1a] text-stone-100 placeholder:text-stone-500";
  const hoverClass = theme === "light" ? "hover:bg-stone-50" : "hover:bg-stone-800/60";
  const activeSession = session || { active: false, startedAt: "" };
  const rawItems = useMemo(() => (approvals?.data || []).filter((item) => item.target_ref === target.ref), [approvals?.data, target.ref]);
  const items = useMemo(() => {
    if (!activeSession.active) return [];
    const startedAt = new Date(activeSession.startedAt).getTime();
    return rawItems.filter((item) => {
      if (isAutocompleteMetadataRequest(item)) return false;
      const createdAt = new Date(item.created_at).getTime();
      return Number.isFinite(createdAt) && createdAt >= startedAt - 1000;
    });
  }, [rawItems, activeSession.active, activeSession.startedAt]);
  const selected = useMemo(() => {
    if (selectedID) {
      const exact = items.find((item) => Number(item.id) === Number(selectedID));
      if (exact) return exact;
    }
    return items[0] || null;
  }, [items, selectedID]);

  useEffect(() => {
    setSelectedID(null);
    setResultView(false);
    columnMetadataRequestsRef.current = new Set();
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  useEffect(() => {
    metadataRowsRef.current = metadata.tables;
  }, [metadata.tables]);

  useEffect(() => {
    if (!activeSession.active) {
      metadataSessionRef.current = "";
      setMetadata({ state: "idle", tables: [], error: "" });
      return undefined;
    }
    const sessionKey = `${target.ref}:${activeSession.startedAt || "active"}`;
    if (metadataSessionRef.current === sessionKey) return undefined;
    metadataSessionRef.current = sessionKey;
    let canceled = false;
    setMetadata({ state: "loading", tables: [], error: "" });
    apiPost("/api/connector-actions/local-run", {
      target_ref: target.ref,
      action_name: "query_readonly",
      input: {
        sql: `SELECT table_schema, table_name, column_name, data_type
FROM information_schema.columns
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
  AND table_schema NOT LIKE 'pg_toast%'
ORDER BY table_schema, table_name, ordinal_position`,
        max_rows: 1000,
      },
      reason: "load Postgres console autocomplete",
    })
      .then(async (item) => {
        if (canceled) return;
        setMetadata({ state: "ready", tables: extractTableSuggestions(item.output), error: "" });
        await onRefreshActivity?.();
      })
      .catch((error) => {
        if (canceled) return;
        setMetadata({ state: "error", tables: [], error: error.message || "Could not load metadata suggestions." });
      });
    return () => {
      canceled = true;
    };
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  useEffect(() => {
    if (!activeSession.active || !sql.trim()) return undefined;
    const references = referencedTablesFromSQL(sql);
    const missing = references
      .filter((reference) => reference.table && !metadataHasColumns(metadataRowsRef.current, reference))
      .filter((reference) => {
        const key = tableReferenceKey(reference);
        if (columnMetadataRequestsRef.current.has(key)) return false;
        columnMetadataRequestsRef.current.add(key);
        return true;
      })
      .slice(0, 4);
    if (missing.length === 0) return undefined;
    let canceled = false;
    const timer = window.setTimeout(() => {
      for (const reference of missing) {
        apiPost("/api/connector-actions/local-run", {
          target_ref: target.ref,
          action_name: "describe_table",
          input: {
            schema: reference.schema || "",
            table: reference.table,
          },
          reason: "load Postgres console autocomplete",
        })
          .then(async (item) => {
            if (canceled) return;
            const nextRows = mergeMetadataRows(metadataRowsRef.current, extractTableSuggestions(item.output));
            metadataRowsRef.current = nextRows;
            setMetadata((current) => ({ ...current, state: "ready", tables: nextRows, error: "" }));
            await onRefreshActivity?.();
          })
          .catch(() => {
            columnMetadataRequestsRef.current.delete(tableReferenceKey(reference));
          });
      }
    }, 250);
    return () => {
      canceled = true;
      window.clearTimeout(timer);
    };
  }, [activeSession.active, sql, target.ref]);

  async function runQuery(event) {
    event?.preventDefault?.();
    if (!activeSession.active || !sql.trim()) return;
    setRunState({ state: "running", error: "" });
    try {
      const item = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: "query_readonly",
        input: {
          sql,
          max_rows: Number(maxRows) || 100,
        },
        reason: "manual Postgres console query",
      });
      setSelectedID(item.request_id || null);
      setRunState({ state: "idle", error: "" });
      await onRefreshActivity?.();
    } catch (error) {
      setRunState({ state: "error", error: error.message || "Query failed." });
    } finally {
      setEditorFocusTick((current) => current + 1);
    }
  }

  if (!activeSession.active) {
    return (
      <div className={`grid min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
        <PostgresNoSessionPlaceholder target={target} theme={theme} onNewSession={onNewStructuredSession} />
        <PostgresEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      </div>
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] ${panelClass}`}>
      <form className={`grid gap-2 border-b p-3 ${borderClass} ${subtlePanelClass}`} onSubmit={runQuery}>
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="text-xs font-semibold">SQL</p>
            <p className={`truncate text-xs ${mutedClass}`}>{metadataStatusText(metadata)}</p>
          </div>
          <label className="flex shrink-0 items-center gap-2 text-xs font-semibold">
            Max rows
            <input
              type="number"
              min="1"
              max="1000"
              className={`h-8 w-20 rounded-md border px-2 outline-none ${inputClass}`}
              value={maxRows}
              onChange={(event) => setMaxRows(event.target.value)}
              disabled={!activeSession.active || runState.state === "running"}
            />
          </label>
        </div>
        <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_auto]">
          <PostgresSQLEditor
            value={sql}
            onChange={setSQL}
            onSubmit={runQuery}
            focusSignal={editorFocusTick}
            theme={theme}
            tables={metadata.tables}
            disabled={!activeSession.active || runState.state === "running"}
          />
          <Button type="submit" className="h-full min-h-10 px-5" disabled={!activeSession.active || !sql.trim() || runState.state === "running"}>
            {runState.state === "running" ? "Running" : "Run SQL (Ctrl+Enter)"}
          </Button>
        </div>
        {runState.error ? <Notice tone="bad">{runState.error}</Notice> : null}
      </form>

      <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)] gap-4 overflow-hidden p-4 ${resultView ? "grid-cols-1" : "lg:grid-cols-[320px_minmax(0,1fr)]"}`}>
        {!resultView ? (
          <section className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass}`}>
            <div className={`border-b px-4 py-3 ${borderClass} ${subtlePanelClass}`}>
              <h4 className="text-sm font-semibold">Session requests</h4>
              <p className={`mt-1 text-xs ${mutedClass}`}>
                {activeSession.active ? `${items.length} request${items.length === 1 ? "" : "s"} since ${formatConnectorTime(activeSession.startedAt)}.` : "Session ended. Start a new session to watch new requests here."}
              </p>
            </div>
            <div className={`min-h-0 overflow-y-auto divide-y ${theme === "light" ? "divide-stone-200" : "divide-stone-700"}`}>
              {items.map((item) => {
                const active = selected && Number(selected.id) === Number(item.id);
                return (
                  <button
                    key={item.id}
                    type="button"
                    className={`grid w-full gap-1 px-4 py-3 text-left transition ${active ? "bg-emerald-950 text-white" : hoverClass}`}
                    onClick={() => setSelectedID(active ? null : item.id)}
                  >
                    <span className="flex min-w-0 items-center justify-between gap-2">
                      <span className="truncate font-mono text-xs font-semibold">{item.action_name}</span>
                      <ActivityStatusBadge status={item.status} />
                    </span>
                    <span className={`truncate text-xs ${active ? "text-emerald-100" : mutedClass}`}>{item.reason || formatConnectorTime(item.created_at)}</span>
                  </button>
                );
              })}
              {items.length === 0 ? <p className={`px-4 py-5 text-sm ${mutedClass}`}>{activeSession.active ? "No requests in this session yet." : "No active Postgres session."}</p> : null}
            </div>
          </section>
        ) : null}

        <section className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass}`}>
          {selected ? (
            <>
              <header className={`border-b px-4 py-3 ${borderClass} ${subtlePanelClass}`}>
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <h4 className="truncate text-sm font-semibold">Request #{selected.id}</h4>
                    <p className={`mt-1 truncate text-xs ${mutedClass}`}>
                      {selected.action_name} / {formatConnectorTime(selected.created_at)}
                    </p>
                  </div>
                  <ActivityStatusBadge status={selected.status} />
                </div>
                <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
                  {selected.reason ? <p className={`min-w-0 flex-1 truncate text-xs ${mutedClass}`}>Reason: {selected.reason}</p> : <span />}
                  <ResultViewToggle checked={resultView} onChange={setResultView} theme={theme} />
                </div>
                {selected.error ? <Notice tone="bad">{selected.error}</Notice> : null}
              </header>
              <div className={`h-full min-h-0 overflow-hidden p-3 ${resultView ? "" : "grid gap-3 xl:grid-cols-2"}`}>
                {!resultView ? (
                  <>
                    <ActivityBlock title="Input" value={selected.input || {}} />
                    <ActivityBlock title="Output" value={selected.output ?? selected.display_text ?? {}} />
                  </>
                ) : (
                  <PostgresOutputBlock title="Rows" value={selected.output ?? selected.display_text ?? {}} theme={theme} />
                )}
              </div>
            </>
          ) : (
            <div className={`grid h-full min-h-0 place-items-center p-6 text-sm ${mutedClass}`}>
              Select a session request to inspect input and output. Completed requests remain available in History.
            </div>
          )}
        </section>
      </div>

      <div className={`border-t px-4 py-2 text-xs ${borderClass} ${mutedClass}`}>
        <span className="inline-flex min-w-0 items-center gap-2">
          <Database className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">{targetEndpoint(target)}</span>
        </span>
      </div>
    </div>
  );
}

function PostgresNoSessionPlaceholder({ target, theme, onNewSession }) {
  const light = theme === "light";
  return (
    <div className={`grid h-full min-h-0 place-items-center p-6 ${light ? "text-stone-700" : "text-stone-200"}`}>
      <div className="grid max-w-md gap-4 text-center">
        <div className={`mx-auto flex h-12 w-12 items-center justify-center rounded-full border ${light ? "border-stone-200 bg-stone-100" : "border-stone-600 bg-stone-800"}`}>
          <TerminalSquare className={`h-6 w-6 ${light ? "text-stone-600" : "text-stone-300"}`} />
        </div>
        <div className="grid gap-2">
          <h3 className={`text-base font-semibold ${light ? "text-stone-950" : "text-white"}`}>No active Postgres session</h3>
          <p className={`text-sm leading-6 ${light ? "text-stone-600" : "text-stone-400"}`}>Start a Postgres session before running SQL against {target.name}.</p>
        </div>
        <Button type="button" className="mx-auto" onClick={() => onNewSession?.()}>
          <RefreshCcw className="h-4 w-4" />
          New Session
        </Button>
      </div>
    </div>
  );
}

function PostgresEndpointFooter({ target, borderClass, mutedClass }) {
  return (
    <div className={`border-t px-4 py-2 text-xs ${borderClass} ${mutedClass}`}>
      <span className="inline-flex min-w-0 items-center gap-2">
        <Database className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{targetEndpoint(target)}</span>
      </span>
    </div>
  );
}

function PostgresSQLEditor({ value, onChange, onSubmit, focusSignal, theme, tables, disabled }) {
  const containerRef = useRef(null);
  const editorRef = useRef(null);
  const changeRef = useRef(null);
  const providerRef = useRef(null);
  const submitRef = useRef(onSubmit);
  const tablesRef = useRef(tables);
  const [monaco, setMonaco] = useState(null);

  useEffect(() => {
    submitRef.current = onSubmit;
  }, [onSubmit]);

  useEffect(() => {
    tablesRef.current = tables;
  }, [tables]);

  useEffect(() => {
    let canceled = false;
    loadMonaco().then((monacoInstance) => {
      if (canceled || !containerRef.current) return;
      setMonaco(monacoInstance);
      providerRef.current = monacoInstance.languages.registerCompletionItemProvider("sql", {
        triggerCharacters: [".", " ", "\""],
        provideCompletionItems(model, position) {
          return { suggestions: postgresCompletionItems(monacoInstance, tablesRef.current, model, position) };
        },
      });
      const editor = monacoInstance.editor.create(containerRef.current, {
        value: value || "",
        language: "sql",
        theme: postgresEditorTheme(monacoInstance, theme),
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
        readOnly: disabled,
        domReadOnly: disabled,
        padding: { top: 8, bottom: 8 },
      });
      editorRef.current = editor;
      editor.addCommand(monacoInstance.KeyMod.CtrlCmd | monacoInstance.KeyCode.Enter, () => submitRef.current?.());
      changeRef.current = editor.onDidChangeModelContent(() => {
        onChange(editor.getValue());
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
    monaco.editor.setTheme(postgresEditorTheme(monaco, theme));
  }, [monaco, theme]);

  useEffect(() => {
    editorRef.current?.updateOptions({ readOnly: disabled, domReadOnly: disabled });
  }, [disabled]);

  useEffect(() => {
    if (!focusSignal) return;
    window.setTimeout(() => editorRef.current?.focus(), 0);
  }, [focusSignal]);

  return <div ref={containerRef} className={`min-h-28 overflow-visible rounded-md border ${theme === "light" ? "border-stone-300 bg-stone-50" : "border-stone-700 bg-[#252526]"}`} />;
}

export function PostgresConnectorToolbarActionsTemplate({ theme, structuredSession, onNewStructuredSession, onEndStructuredSession }) {
  const buttonClass = `h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`;
  const active = Boolean(structuredSession?.active);
  return (
    <>
      <Button type="button" variant="ghost" className={buttonClass} onClick={onNewStructuredSession} disabled={active} title="Start a fresh Postgres activity session">
        <RefreshCcw className="h-3.5 w-3.5" />
        New Session
      </Button>
      <Button type="button" variant="ghost" className={buttonClass} onClick={onEndStructuredSession} disabled={!active} title="End the current Postgres activity session">
        <XCircle className="h-3.5 w-3.5" />
        End Session
      </Button>
    </>
  );
}

function ResultViewToggle({ checked, onChange, theme }) {
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

function ActivityBlock({ title, value }) {
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
      <p className="text-xs font-semibold uppercase text-stone-500">{title}</p>
      <TerminalBlock className="min-h-0 overflow-auto text-xs">{formatJSON(value)}</TerminalBlock>
    </div>
  );
}

function PostgresOutputBlock({ title, value, theme }) {
  const normalized = normalizeConnectorOutput(value);
  const columns = Array.isArray(normalized?.columns) ? normalized.columns.map((item) => String(item)) : [];
  const rows = Array.isArray(normalized?.rows) ? normalized.rows : [];
  const tableText = rowsToClipboardText(columns, rows);
  if (columns.length > 0) {
    return (
      <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
        <div className="flex items-center justify-between gap-3">
          <p className="text-xs font-semibold uppercase text-stone-500">{title}</p>
          <CopyButton value={tableText} variant="outline" className="h-8 px-2 text-xs" iconClassName="h-3.5 w-3.5" />
        </div>
        <div className={`min-h-0 overflow-auto rounded-md border font-mono text-xs ${theme === "light" ? "border-stone-200 bg-white" : "border-stone-700 bg-[#1a1a1a]"}`}>
          <table className="min-w-full border-separate border-spacing-0 select-text">
            <thead className={theme === "light" ? "bg-stone-100 text-stone-600" : "bg-stone-900 text-stone-300"}>
              <tr>
                {columns.map((column) => (
                  <th key={column} className={`sticky top-0 border-b px-3 py-2 text-left font-semibold ${theme === "light" ? "border-stone-200 bg-stone-100" : "border-stone-700 bg-stone-900"}`}>
                    {column}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, rowIndex) => (
                <tr key={rowIndex} className={theme === "light" ? "odd:bg-white even:bg-stone-50" : "odd:bg-[#1a1a1a] even:bg-[#202020]"}>
                  {columns.map((column) => (
                    <td key={column} className={`max-w-[420px] whitespace-pre-wrap border-b px-3 py-2 align-top ${theme === "light" ? "border-stone-100 text-stone-900" : "border-stone-800 text-stone-100"}`}>
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
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
      <p className="text-xs font-semibold uppercase text-stone-500">{title}</p>
      <TerminalBlock className="min-h-0 overflow-auto text-xs">{formatJSON(value)}</TerminalBlock>
    </div>
  );
}

function ActivityStatusBadge({ status }) {
  const tone = status === "completed" ? "good" : status === "failed" || status === "error" || status === "stale" ? "bad" : status === "approval_pending" || status === "running" ? "warn" : "neutral";
  return <Badge tone={tone}>{status}</Badge>;
}

function formatConnectorTime(value) {
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

function targetEndpoint(target) {
  if (!target) return "-";
  const host = target.config?.host || "host";
  const port = target.config?.port || 5432;
  const database = target.config?.database || "database";
  return `${host}:${port}/${database}`;
}

function metadataStatusText(metadata) {
  if (metadata.state === "loading") return "Loading metadata suggestions for autocomplete...";
  if (metadata.state === "error") return `Autocomplete metadata unavailable: ${metadata.error}`;
  if (metadata.state === "ready") {
    return metadata.tables.length > 0
      ? `${metadata.tables.length} metadata suggestion${metadata.tables.length === 1 ? "" : "s"} loaded. Run bounded read-only SQL through this credential profile.`
      : "No metadata suggestions found. Run bounded read-only SQL through this credential profile.";
  }
  return "Run bounded read-only SQL through this credential profile.";
}

function extractTableSuggestions(output) {
  const normalized = normalizeConnectorOutput(output);
  const rows = Array.isArray(normalized?.rows) ? normalized.rows : [];
  return rows
    .map((row) => ({
      schema: cleanCompletionValue(row.table_schema || row.schema),
      table: cleanCompletionValue(row.table_name || row.table),
      column: cleanCompletionValue(row.column_name || row.column),
      dataType: cleanCompletionValue(row.data_type || row.type),
      type: cleanCompletionValue(row.table_type || row.type),
    }))
    .filter((row) => row.schema && row.table);
}

function mergeMetadataRows(current, incoming) {
  const merged = [];
  const seen = new Set();
  for (const item of [...(current || []), ...(incoming || [])]) {
    const key = [normalizeSQLName(item.schema), normalizeSQLName(item.table), normalizeSQLName(item.column), normalizeSQLName(item.dataType || item.type)].join(".");
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(item);
  }
  return merged;
}

function metadataHasColumns(rows, reference) {
  return (rows || []).some((item) => item.column && tableMatchesReference(item, reference));
}

function cleanCompletionValue(value) {
  return String(value || "").trim();
}

function normalizeConnectorOutput(output) {
  if (typeof output !== "string") return output || {};
  try {
    return JSON.parse(output);
  } catch {
    return {};
  }
}

function isAutocompleteMetadataRequest(item) {
  return item?.reason === "load Postgres console autocomplete";
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

function postgresEditorTheme(monaco, theme) {
  const dark = theme !== "light";
  const name = dark ? "aipermission-postgres-dark" : "aipermission-postgres-light";
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
      "editorLineHighlightBorder": "#00000000",
      "editorLineHighlightBackground": "#00000000",
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

const SQL_KEYWORDS = [
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

function postgresCompletionItems(monaco, tables, model, position) {
  const word = model.getWordUntilPosition(position);
  const range = {
    startLineNumber: position.lineNumber,
    endLineNumber: position.lineNumber,
    startColumn: word.startColumn,
    endColumn: word.endColumn,
  };
  const suggestions = SQL_KEYWORDS.map((keyword) => ({
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

function referencedTablesFromSQL(sql) {
  const cleaned = stripSQLStringsAndComments(sql);
  const references = [];
  const pattern = /\b(?:from|join)\s+((?:"[^"]+"|[a-zA-Z_][\w$]*)(?:\s*\.\s*(?:"[^"]+"|[a-zA-Z_][\w$]*))?)(?:\s+(?:as\s+)?("[^"]+"|[a-zA-Z_][\w$]*))?/gi;
  for (const match of cleaned.matchAll(pattern)) {
    const nameParts = splitSQLQualifiedName(match[1]);
    const alias = cleanSQLIdentifier(match[2] || "");
    const reference = {
      schema: nameParts.length > 1 ? nameParts[0] : "",
      table: nameParts.length > 1 ? nameParts[1] : nameParts[0],
      alias: isSQLAlias(alias) ? alias : "",
    };
    if (reference.table) references.push(reference);
  }
  return references;
}

function stripSQLStringsAndComments(sql) {
  return String(sql || "")
    .replace(/'([^']|'')*'/g, " ")
    .replace(/"([^"]|"")*"/g, (match) => match)
    .replace(/--.*$/gm, " ")
    .replace(/\/\*[\s\S]*?\*\//g, " ");
}

function splitSQLQualifiedName(value) {
  return String(value || "")
    .split(".")
    .map((part) => cleanSQLIdentifier(part))
    .filter(Boolean);
}

function cleanSQLIdentifier(value) {
  const trimmed = String(value || "").trim();
  if (trimmed.startsWith('"') && trimmed.endsWith('"')) {
    return trimmed.slice(1, -1).replaceAll('""', '"');
  }
  return trimmed;
}

function isSQLAlias(value) {
  if (!value) return false;
  return !new Set(["where", "join", "left", "right", "inner", "outer", "full", "cross", "on", "group", "order", "limit", "offset", "union", "having"]).has(normalizeSQLName(value));
}

function normalizeSQLName(value) {
  return String(value || "").trim().toLowerCase();
}

function tableReferenceKey(reference) {
  return `${normalizeSQLName(reference.schema)}.${normalizeSQLName(reference.table)}`;
}

function tableMatchesReference(item, reference) {
  if (!item || !reference) return false;
  const tableMatches = normalizeSQLName(item.table) === normalizeSQLName(reference.table);
  if (!tableMatches) return false;
  if (reference.schema && normalizeSQLName(item.schema) !== normalizeSQLName(reference.schema)) return false;
  return true;
}

function matchingReferencesForQualifier(qualifier, references, metadataRows) {
  const normalized = normalizeSQLName(qualifier);
  const matches = references.filter((reference) => normalizeSQLName(reference.alias) === normalized || normalizeSQLName(reference.table) === normalized);
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
  const match = prefix.match(/((?:"[^"]+"|[a-zA-Z_][\w$]*))\.\s*(?:"[^"]*"|[a-zA-Z_][\w$]*)?$/);
  return match ? cleanSQLIdentifier(match[1]) : "";
}

function isTableCompletionContext(model, position) {
  const prefix = model.getLineContent(position.lineNumber).slice(0, position.column - 1).toLowerCase();
  return /\b(from|join)\s+(?:"[^"]*"|[a-z_][\w$]*)?$/i.test(prefix);
}

function quoteSQLIdentifier(value) {
  if (/^[a-z_][a-z0-9_]*$/.test(value)) return value;
  return `"${String(value).replaceAll('"', '""')}"`;
}
