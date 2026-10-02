import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { components } from "../../../../types/generated-openapi";
import { apiPost } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import { useRequestGuard } from "../../../lib/request-guard";
import { connectorActionResultResponse, requireCompletedConnectorAction } from "./action-result";
import {
  actionInputSQL,
  filteredTableBrowserRows,
  isAutocompleteMetadataRequest,
  normalizeSQLConsoleConfig,
  recentSQLQueries,
} from "./sql-console-config";
import { useSQLMetadata } from "./use-sql-metadata";
import type { SQLConsoleConfigInput } from "./sql-console-config";
import type { SQLTableReference } from "./sql-console-data";

type ApprovalSummary = components["schemas"]["ConnectorActionApprovalSummary"];
export type SQLActivityItem = Pick<ApprovalSummary, "id" | "action_name" | "target_ref" | "created_at" | "status"> &
  Partial<Pick<ApprovalSummary, "input" | "output" | "error" | "display_text" | "reason">>;
export type SQLActivitySession = { active: boolean; startedAt: string };
export type SQLConsoleProps = {
  config?: SQLConsoleConfigInput;
  target: { ref: string; name: string; config?: { host?: string; port?: number; database?: string } };
  approvals?: { data?: SQLActivityItem[] };
  session?: SQLActivitySession | null;
  onRefreshActivity?: () => unknown;
};
export type SQLConsoleController = ReturnType<typeof useSQLConsole>;
type ActionResponse = Parameters<typeof requireCompletedConnectorAction>[0];

export function useSQLConsole({ config, target, approvals, session, onRefreshActivity }: SQLConsoleProps) {
  const connector = useMemo(() => normalizeSQLConsoleConfig(config), [config]);
  const activeSession = useMemo(() => session || { active: false, startedAt: "" }, [session]);
  const [selectedID, setSelectedID] = useState<number | string | null>(null);
  const [sql, setSQL] = useState("");
  const [maxRows, setMaxRows] = useState<number | string>(100);
  const [runState, setRunState] = useState<{ state: "idle" | "running" | "error"; error: string }>({ state: "idle", error: "" });
  const [editorFocusTick, setEditorFocusTick] = useState(0);
  const [resultView, setResultView] = useState(false);
  const [leftPanel, setLeftPanel] = useState<"browser" | "requests">("browser");
  const [browserSearch, setBrowserSearch] = useState("");
  const requestScope = JSON.stringify([target.ref, activeSession.active, activeSession.startedAt, connector.queryAction]);
  const requestGuard = useRequestGuard(requestScope);
  const queryOwner = useMemo(() => ({ requestScope }), [requestScope]);
  const queryOwnerRef = useRef<object | null>(null);
  const pendingQueryRef = useRef<symbol | null>(null);
  useLayoutEffect(() => {
    queryOwnerRef.current = queryOwner;
    pendingQueryRef.current = null;
    return () => {
      queryOwnerRef.current = null;
      pendingQueryRef.current = null;
      requestGuard.invalidate("query");
    };
  }, [queryOwner, requestGuard]);
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
  }, [requestScope]);

  async function runQuery(event?: { preventDefault?: () => void }): Promise<void> {
    event?.preventDefault?.();
    if (!activeSession.active || !sql.trim() || queryOwnerRef.current !== queryOwner || pendingQueryRef.current !== null) return;
    const token = Symbol("sql-query");
    pendingQueryRef.current = token;
    const request = requestGuard.begin("query");
    setRunState({ state: "running", error: "" });
    try {
      const response: ActionResponse = connectorActionResultResponse(
        await apiPost(
          "/api/connector-actions/local-run",
          {
            target_ref: target.ref,
            action_name: connector.queryAction,
            input: { sql, max_rows: Number(maxRows) || 100 },
            reason: connector.manualReason,
          },
          { signal: request.signal },
        ),
      );
      if (!request.isCurrent()) return;
      const item = requireCompletedConnectorAction(response, "Query failed.");
      if (!item) {
        setRunState({ state: "idle", error: response.display_text || "Query is awaiting approval." });
        void refreshActivitySafely(onRefreshActivity);
        return;
      }
      setSelectedID(item.request_id || null);
      try {
        await onRefreshActivity?.();
        if (request.isCurrent()) setRunState({ state: "idle", error: "" });
      } catch (error) {
        if (request.isCurrent())
          setRunState({ state: "error", error: `Query completed, but activity refresh failed: ${errorMessage(error)}` });
      }
    } catch (error) {
      if (request.isCurrent()) setRunState({ state: "error", error: errorMessage(error, "Query failed.") });
    } finally {
      if (pendingQueryRef.current === token) pendingQueryRef.current = null;
      if (request.isCurrent()) setEditorFocusTick((current) => current + 1);
      request.complete();
    }
  }

  function loadSQL(value: string): void {
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
    prepareTableQuery: (table: SQLTableReference | null) =>
      loadSQL(table?.table ? connector.tableQuery(table, Math.min(Number(maxRows) || 100, 100)) : ""),
  };
}

function sessionItems(items: SQLActivityItem[], session: SQLActivitySession, metadataReason: string): SQLActivityItem[] {
  if (!session.active) return [];
  const startedAt = new Date(session.startedAt).getTime();
  return items.filter((item) => {
    if (isAutocompleteMetadataRequest(item, metadataReason)) return false;
    const createdAt = new Date(item.created_at).getTime();
    return Number.isFinite(createdAt) && createdAt >= startedAt - 1000;
  });
}

function selectActivity(items: SQLActivityItem[], selectedID: number | string | null): SQLActivityItem | null {
  if (selectedID) {
    const exact = items.find((item) => Number(item.id) === Number(selectedID));
    if (exact) return exact;
  }
  return items[0] || null;
}

async function refreshActivitySafely(refresh?: () => unknown): Promise<void> {
  try {
    await refresh?.();
  } catch {
    /* The pending action remains authoritative. */
  }
}
