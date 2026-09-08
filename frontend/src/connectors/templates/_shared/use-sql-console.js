import { useEffect, useMemo, useState } from "react";
import { apiPost } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import { useRequestGuard } from "../../../lib/request-guard";
import { requireCompletedConnectorAction } from "./action-result";
import {
  actionInputSQL,
  filteredTableBrowserRows,
  isAutocompleteMetadataRequest,
  normalizeSQLConsoleConfig,
  recentSQLQueries,
} from "./sql-console-config";
import { useSQLMetadata } from "./use-sql-metadata";

export function useSQLConsole({ config, target, approvals, session, onRefreshActivity }) {
  const connector = useMemo(() => normalizeSQLConsoleConfig(config), [config]);
  const activeSession = useMemo(() => session || { active: false, startedAt: "" }, [session]);
  const [selectedID, setSelectedID] = useState(null);
  const [sql, setSQL] = useState("");
  const [maxRows, setMaxRows] = useState(100);
  const [runState, setRunState] = useState({ state: "idle", error: "" });
  const [editorFocusTick, setEditorFocusTick] = useState(0);
  const [resultView, setResultView] = useState(false);
  const [leftPanel, setLeftPanel] = useState("browser");
  const [browserSearch, setBrowserSearch] = useState("");
  const requestGuard = useRequestGuard(`${target.ref}:${activeSession.startedAt || "inactive"}`);
  const rawItems = useMemo(() => (approvals?.data || []).filter((item) => item.target_ref === target.ref), [approvals?.data, target.ref]);
  const items = useMemo(
    () => sessionItems(rawItems, activeSession, connector.metadataReason),
    [rawItems, activeSession, connector.metadataReason],
  );
  const recentQueries = useMemo(() => recentSQLQueries(rawItems, connector), [rawItems, connector]);
  const selected = useMemo(() => selectActivity(items, selectedID), [items, selectedID]);
  const metadata = useSQLMetadata({ activeSession, connector, onRefreshActivity, requestGuard, sql, targetRef: target.ref });
  const browserTables = useMemo(() => filteredTableBrowserRows(metadata.tables, browserSearch), [metadata.tables, browserSearch]);

  useEffect(() => {
    setSelectedID(null);
    setResultView(false);
    setRunState({ state: "idle", error: "" });
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  async function runQuery(event) {
    event?.preventDefault?.();
    if (!activeSession.active || !sql.trim()) return;
    const request = requestGuard.begin("query");
    setRunState({ state: "running", error: "" });
    try {
      const response = await apiPost(
        "/api/connector-actions/local-run",
        {
          target_ref: target.ref,
          action_name: connector.queryAction,
          input: { sql, max_rows: Number(maxRows) || 100 },
          reason: connector.manualReason,
        },
        { signal: request.signal },
      );
      if (!request.isCurrent()) return;
      const item = requireCompletedConnectorAction(response, "Query failed.");
      if (!item) {
        setRunState({ state: "idle", error: response.display_text || "Query is awaiting approval." });
        void refreshActivitySafely(onRefreshActivity);
        return;
      }
      setSelectedID(item.request_id || null);
      setRunState({ state: "idle", error: "" });
      try {
        await onRefreshActivity?.();
      } catch (error) {
        if (request.isCurrent())
          setRunState({ state: "error", error: `Query completed, but activity refresh failed: ${errorMessage(error)}` });
      }
    } catch (error) {
      if (request.isCurrent()) setRunState({ state: "error", error: errorMessage(error, "Query failed.") });
    } finally {
      if (request.isCurrent()) setEditorFocusTick((current) => current + 1);
      request.complete();
    }
  }

  function loadSQL(value) {
    if (!value) return;
    setSQL(value);
    setEditorFocusTick((current) => current + 1);
  }

  return {
    connector,
    activeSession,
    selectedID,
    setSelectedID,
    selected,
    selectedSQL: selected ? actionInputSQL(selected) : "",
    sql,
    setSQL,
    maxRows,
    setMaxRows,
    runState,
    editorFocusTick,
    resultView,
    setResultView,
    leftPanel,
    setLeftPanel,
    browserSearch,
    setBrowserSearch,
    metadata,
    browserTables,
    items,
    recentQueries,
    runQuery,
    loadSQL,
    prepareTableQuery: (table) => loadSQL(table?.table ? connector.tableQuery(table, Math.min(Number(maxRows) || 100, 100)) : ""),
  };
}

function sessionItems(items, session, metadataReason) {
  if (!session.active) return [];
  const startedAt = new Date(session.startedAt).getTime();
  return items.filter((item) => {
    if (isAutocompleteMetadataRequest(item, metadataReason)) return false;
    const createdAt = new Date(item.created_at).getTime();
    return Number.isFinite(createdAt) && createdAt >= startedAt - 1000;
  });
}

function selectActivity(items, selectedID) {
  if (selectedID) {
    const exact = items.find((item) => Number(item.id) === Number(selectedID));
    if (exact) return exact;
  }
  return items[0] || null;
}

async function refreshActivitySafely(refresh) {
  try {
    await refresh?.();
  } catch {
    /* The pending action remains authoritative. */
  }
}
