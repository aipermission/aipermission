import { Database } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Notice } from "../../../components/ui/notice";
import { apiPost } from "../../../lib/api";
import { requireCompletedConnectorAction } from "./action-result";
import { connectorConsoleTheme } from "./console-theme";
import { useRequestGuard } from "./request-guard";
import { extractTableSuggestions, normalizeConnectorOutput, pendingMetadataReferences, tableReferenceKey } from "./sql-console-data";

import {
  ActivityBlock,
  ActivityStatusBadge,
  ResultViewToggle,
  SQLNoSessionPlaceholder,
  SQLSchemaBrowser,
  SQLEditor,
  SQLEndpointFooter,
  SQLOutputBlock,
  actionInputSQL,
  filteredTableBrowserRows,
  formatConnectorTime,
  isAutocompleteMetadataRequest,
  mergeMetadataRows,
  metadataStatusText,
  normalizeSQLConsoleConfig,
  recentSQLQueries,
  tableBrowserSummary,
} from "./sql-console-support";
export { SQLConnectorToolbarActions } from "./sql-console-support";

export function SQLConnectorConsole({ config, target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const connector = useMemo(() => normalizeSQLConsoleConfig(config), [config]);
  const [selectedID, setSelectedID] = useState(null);
  const [sql, setSQL] = useState("");
  const [maxRows, setMaxRows] = useState(100);
  const [runState, setRunState] = useState({ state: "idle", error: "" });
  const [metadata, setMetadata] = useState({ state: "idle", tables: [], error: "", truncated: false });
  const [editorFocusTick, setEditorFocusTick] = useState(0);
  const [resultView, setResultView] = useState(false);
  const [leftPanel, setLeftPanel] = useState("browser");
  const [browserSearch, setBrowserSearch] = useState("");
  const metadataSessionRef = useRef("");
  const metadataRowsRef = useRef([]);
  const columnMetadataRequestsRef = useRef(new Set());
  const {
    panel: panelClass,
    muted: mutedClass,
    border: borderClass,
    subtlePanel: subtlePanelClass,
    input: inputClass,
    rowHover: hoverClass,
  } = connectorConsoleTheme(theme);
  const activeSession = session || { active: false, startedAt: "" };
  const requestGuard = useRequestGuard(`${target.ref}:${activeSession.startedAt || "inactive"}`);
  const refreshActivityFromEffect = useEffectEvent(() => onRefreshActivity?.());
  const rawItems = useMemo(() => (approvals?.data || []).filter((item) => item.target_ref === target.ref), [approvals?.data, target.ref]);
  const items = useMemo(() => {
    if (!activeSession.active) return [];
    const startedAt = new Date(activeSession.startedAt).getTime();
    return rawItems.filter((item) => {
      if (isAutocompleteMetadataRequest(item, connector.metadataReason)) return false;
      const createdAt = new Date(item.created_at).getTime();
      return Number.isFinite(createdAt) && createdAt >= startedAt - 1000;
    });
  }, [rawItems, activeSession.active, activeSession.startedAt, connector.metadataReason]);
  const recentQueries = useMemo(() => recentSQLQueries(rawItems, connector), [rawItems, connector]);
  const selected = useMemo(() => {
    if (selectedID) {
      const exact = items.find((item) => Number(item.id) === Number(selectedID));
      if (exact) return exact;
    }
    return items[0] || null;
  }, [items, selectedID]);
  const selectedSQL = selected ? actionInputSQL(selected) : "";
  const browserTables = useMemo(() => filteredTableBrowserRows(metadata.tables, browserSearch), [metadata.tables, browserSearch]);

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
      setMetadata({ state: "idle", tables: [], error: "", truncated: false });
      return undefined;
    }
    const sessionKey = `${target.ref}:${activeSession.startedAt || "active"}`;
    if (metadataSessionRef.current === sessionKey) return undefined;
    metadataSessionRef.current = sessionKey;
    const request = requestGuard.begin("metadata");
    setMetadata({ state: "loading", tables: [], error: "", truncated: false });
    apiPost("/api/connector-actions/local-run", {
      target_ref: target.ref,
      action_name: connector.queryAction,
      input: {
        sql: connector.metadataSQL,
        max_rows: connector.metadataMaxRows,
      },
      reason: connector.metadataReason,
    })
      .then(async (response) => {
        if (!request.isCurrent()) return;
        const item = requireCompletedConnectorAction(response, "Could not load metadata suggestions.");
        if (!item) {
          setMetadata({ state: "pending", tables: [], error: "Metadata request is awaiting approval.", truncated: false });
          await refreshActivityFromEffect();
          return;
        }
        const output = normalizeConnectorOutput(item.output);
        setMetadata({ state: "ready", tables: extractTableSuggestions(output), error: "", truncated: Boolean(output?.truncated) });
        await refreshActivityFromEffect();
      })
      .catch((error) => {
        if (!request.isCurrent()) return;
        setMetadata({ state: "error", tables: [], error: error.message || "Could not load metadata suggestions.", truncated: false });
      });
    return () => {
      requestGuard.invalidate("metadata");
    };
  }, [
    target.ref,
    activeSession.active,
    activeSession.startedAt,
    connector.queryAction,
    connector.metadataSQL,
    connector.metadataMaxRows,
    connector.metadataReason,
    requestGuard,
  ]);

  useEffect(() => {
    if (!activeSession.active || !sql.trim()) return undefined;
    const missing = pendingMetadataReferences(sql, metadataRowsRef.current, columnMetadataRequestsRef.current, 4);
    if (missing.length === 0) return undefined;
    const requestKeys = columnMetadataRequestsRef.current;
    const timer = window.setTimeout(() => {
      for (const reference of missing) {
        const requestKey = tableReferenceKey(reference);
        if (requestKeys.has(requestKey)) continue;
        requestKeys.add(requestKey);
        const request = requestGuard.begin(`metadata:${requestKey}`);
        apiPost("/api/connector-actions/local-run", {
          target_ref: target.ref,
          action_name: connector.describeAction,
          input: connector.describeInput(reference),
          reason: connector.metadataReason,
        })
          .then(async (response) => {
            if (columnMetadataRequestsRef.current !== requestKeys || !request.isCurrent()) return;
            const item = requireCompletedConnectorAction(response, "Could not load column metadata.");
            if (!item) {
              requestKeys.delete(requestKey);
              await refreshActivityFromEffect();
              return;
            }
            const nextRows = mergeMetadataRows(metadataRowsRef.current, extractTableSuggestions(item.output));
            metadataRowsRef.current = nextRows;
            setMetadata((current) => ({ ...current, state: "ready", tables: nextRows, error: "" }));
            await refreshActivityFromEffect();
          })
          .catch(() => {
            if (request.isCurrent()) requestKeys.delete(requestKey);
          });
      }
    }, 250);
    return () => {
      window.clearTimeout(timer);
    };
  }, [activeSession.active, sql, target.ref, connector, requestGuard]);

  async function runQuery(event) {
    event?.preventDefault?.();
    if (!activeSession.active || !sql.trim()) return;
    const request = requestGuard.begin("query");
    setRunState({ state: "running", error: "" });
    try {
      const response = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: connector.queryAction,
        input: {
          sql,
          max_rows: Number(maxRows) || 100,
        },
        reason: connector.manualReason,
      });
      if (!request.isCurrent()) return;
      const item = requireCompletedConnectorAction(response, "Query failed.");
      if (!item) {
        setRunState({ state: "idle", error: response.display_text || "Query is awaiting approval." });
        void onRefreshActivity?.();
        return;
      }
      setSelectedID(item.request_id || null);
      setRunState({ state: "idle", error: "" });
      await onRefreshActivity?.();
    } catch (error) {
      if (!request.isCurrent()) return;
      setRunState({ state: "error", error: error.message || "Query failed." });
    } finally {
      if (request.isCurrent()) setEditorFocusTick((current) => current + 1);
    }
  }

  function prepareTableQuery(table) {
    if (!table?.table) return;
    setSQL(connector.tableQuery(table, Math.min(Number(maxRows) || 100, 100)));
    setEditorFocusTick((current) => current + 1);
  }

  function loadRecentQuery(query) {
    if (!query?.sql) return;
    setSQL(query.sql);
    setEditorFocusTick((current) => current + 1);
  }

  if (!activeSession.active) {
    return (
      <div className={`grid min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
        <SQLNoSessionPlaceholder config={connector} target={target} theme={theme} onNewSession={onNewStructuredSession} />
        <SQLEndpointFooter config={connector} target={target} borderClass={borderClass} mutedClass={mutedClass} />
      </div>
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] ${panelClass}`}>
      <form className={`grid gap-2 border-b p-3 ${borderClass} ${subtlePanelClass}`} onSubmit={runQuery}>
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="text-xs font-semibold">SQL</p>
            <p className={`truncate text-xs ${mutedClass}`}>{metadataStatusText(metadata, connector)}</p>
          </div>
          <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
            <CopyButton
              value={sql}
              variant="outline"
              className="h-8 px-2 text-xs"
              iconClassName="h-3.5 w-3.5"
              title="Copy SQL"
              disabled={!sql.trim()}
            >
              SQL
            </CopyButton>
            {recentQueries.length > 0 ? (
              <Button
                type="button"
                variant="outline"
                className="h-8 px-2 text-xs"
                onClick={() => loadRecentQuery(recentQueries[0])}
                title="Load the most recent query"
              >
                Last query
              </Button>
            ) : null}
            <label className="flex items-center gap-2 text-xs font-semibold">
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
        </div>
        <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_auto]">
          <SQLEditor
            value={sql}
            onChange={setSQL}
            onSubmit={runQuery}
            focusSignal={editorFocusTick}
            theme={theme}
            tables={metadata.tables}
            keywords={connector.keywords}
            disabled={!activeSession.active || runState.state === "running"}
          />
          <Button
            type="submit"
            className="h-full min-h-10 px-5"
            disabled={!activeSession.active || !sql.trim() || runState.state === "running"}
          >
            {runState.state === "running" ? "Running" : "Run SQL (Ctrl+Enter)"}
          </Button>
        </div>
        {runState.error ? <Notice tone="bad">{runState.error}</Notice> : null}
        {recentQueries.length > 0 ? (
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className={`shrink-0 text-[11px] font-semibold uppercase ${mutedClass}`}>Recent</span>
            {recentQueries.slice(0, 5).map((query) => (
              <button
                key={`${query.id}:${query.preview}`}
                type="button"
                className={`max-w-64 truncate rounded-full border px-2.5 py-1 text-left font-mono text-[11px] transition ${theme === "light" ? "border-stone-200 bg-white text-stone-700 hover:bg-stone-100" : "border-stone-700 bg-[#1a1a1a] text-stone-200 hover:bg-stone-800"}`}
                title={query.sql}
                onClick={() => loadRecentQuery(query)}
              >
                {query.preview}
              </button>
            ))}
          </div>
        ) : null}
      </form>

      <div
        className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)] gap-4 overflow-hidden p-4 ${resultView ? "grid-cols-1" : "lg:grid-cols-[320px_minmax(0,1fr)]"}`}
      >
        {!resultView ? (
          <section className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass}`}>
            <div className={`border-b px-4 py-3 ${borderClass} ${subtlePanelClass}`}>
              <div className="flex items-center justify-between gap-3">
                <h4 className="text-sm font-semibold">
                  {leftPanel === "browser" ? `${connector.browserLabel} browser` : "Session requests"}
                </h4>
                <div className={`inline-flex rounded-md border p-0.5 text-xs ${borderClass}`}>
                  {["browser", "requests"].map((mode) => (
                    <button
                      key={mode}
                      type="button"
                      className={`rounded px-2 py-1 font-semibold transition ${leftPanel === mode ? "bg-emerald-700 text-white" : `${mutedClass} ${hoverClass}`}`}
                      onClick={() => setLeftPanel(mode)}
                    >
                      {mode === "browser" ? "Browser" : "Requests"}
                    </button>
                  ))}
                </div>
              </div>
              <p className={`mt-1 text-xs ${mutedClass}`}>
                {leftPanel === "browser"
                  ? tableBrowserSummary(metadata, browserTables, connector.browserLabel)
                  : activeSession.active
                    ? `${items.length} request${items.length === 1 ? "" : "s"} since ${formatConnectorTime(activeSession.startedAt)}.`
                    : "Session ended. Start a new session to watch new requests here."}
              </p>
            </div>
            <div
              className={`min-h-0 overflow-hidden ${leftPanel === "requests" ? `divide-y ${theme === "light" ? "divide-stone-200" : "divide-stone-700"}` : ""}`}
            >
              {leftPanel === "browser" ? (
                <SQLSchemaBrowser
                  rows={browserTables}
                  search={browserSearch}
                  onSearch={setBrowserSearch}
                  onPrepareQuery={prepareTableQuery}
                  metadata={metadata}
                  theme={theme}
                  inputClass={inputClass}
                  mutedClass={mutedClass}
                  hoverClass={hoverClass}
                  namespaceLabel={connector.browserLabel}
                />
              ) : (
                <div className={`h-full min-h-0 overflow-y-auto divide-y ${theme === "light" ? "divide-stone-200" : "divide-stone-700"}`}>
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
                        <span className={`truncate text-xs ${active ? "text-emerald-100" : mutedClass}`}>
                          {item.reason || formatConnectorTime(item.created_at)}
                        </span>
                      </button>
                    );
                  })}
                  {items.length === 0 ? (
                    <p className={`px-4 py-5 text-sm ${mutedClass}`}>
                      {activeSession.active ? "No requests in this session yet." : `No active ${connector.label} session.`}
                    </p>
                  ) : null}
                </div>
              )}
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
                  {selected.reason ? (
                    <p className={`min-w-0 flex-1 truncate text-xs ${mutedClass}`}>Reason: {selected.reason}</p>
                  ) : (
                    <span />
                  )}
                  <div className="flex shrink-0 flex-wrap items-center gap-2">
                    {selectedSQL ? (
                      <>
                        <Button
                          type="button"
                          variant="outline"
                          className="h-8 px-2 text-xs"
                          onClick={() => loadRecentQuery({ sql: selectedSQL })}
                        >
                          Load SQL
                        </Button>
                        <CopyButton
                          value={selectedSQL}
                          variant="outline"
                          className="h-8 px-2 text-xs"
                          iconClassName="h-3.5 w-3.5"
                          title="Copy request SQL"
                        >
                          SQL
                        </CopyButton>
                      </>
                    ) : null}
                    <ResultViewToggle checked={resultView} onChange={setResultView} theme={theme} />
                  </div>
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
                  <SQLOutputBlock
                    title="Rows"
                    value={selected.output ?? selected.display_text ?? {}}
                    theme={theme}
                    filenamePrefix={connector.filenamePrefix}
                  />
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
          <span className="truncate">{connector.targetEndpoint(target)}</span>
        </span>
      </div>
    </div>
  );
}
