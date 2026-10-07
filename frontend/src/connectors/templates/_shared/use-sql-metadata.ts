import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import type { Dispatch, RefObject, SetStateAction } from "react";
import { apiPost } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import { connectorActionResultResponse, requireCompletedConnectorAction } from "./action-result";
import { extractTableSuggestions, normalizeConnectorOutput, pendingMetadataReferences, tableReferenceKey } from "./sql-console-data";
import { mergeMetadataRows } from "./sql-console-config";
import type { normalizeSQLConsoleConfig } from "./sql-console-config";
import type { createRequestGuard } from "../../../lib/request-guard";
import type { SQLIdentifierPolicy, SQLMetadataRow, SQLReference, SQLTableReference } from "./sql-console-data";

export type SQLMetadataState = {
  state: "idle" | "loading" | "pending" | "ready" | "error";
  tables: SQLMetadataRow[];
  error: string;
  truncated: boolean;
};
type MetadataConnector = ReturnType<typeof normalizeSQLConsoleConfig>;
type RequestGuard = ReturnType<typeof createRequestGuard>;
type MetadataProps = {
  activeSession: { active: boolean; startedAt: string };
  connector: MetadataConnector;
  onRefreshActivity?: () => unknown;
  requestGuard: RequestGuard;
  sql: string;
  targetRef: string;
};
const emptyMetadata: SQLMetadataState = { state: "idle", tables: [], error: "", truncated: false };

export function useSQLMetadata({ activeSession, connector, onRefreshActivity, requestGuard, sql, targetRef }: MetadataProps) {
  const [metadata, setMetadata] = useState(emptyMetadata);
  const owner = useMemo(
    () => ({ targetRef, active: activeSession.active, startedAt: activeSession.startedAt, connector }),
    [targetRef, activeSession.active, activeSession.startedAt, connector],
  );
  const [requestedTable, setRequestedTable] = useState<{ reference: SQLTableReference; owner: object } | null>(null);
  const metadataRowsRef = useRef<SQLMetadataRow[]>([]);
  const columnRequestsRef = useRef(new Set<string>());
  const refreshActivity = useEffectEvent(() =>
    Promise.resolve()
      .then(() => onRefreshActivity?.())
      .catch(() => undefined),
  );
  const loadColumns = useEffectEvent((reference: SQLReference) =>
    requestColumnMetadata({
      connector,
      reference,
      requestGuard,
      requests: columnRequestsRef.current,
      requestSetRef: columnRequestsRef,
      targetRef,
      setMetadata,
      metadataRowsRef,
      refreshActivity,
      identifierPolicy: connector.identifierPolicy,
    }),
  );

  useEffect(() => {
    columnRequestsRef.current = new Set<string>();
    metadataRowsRef.current = [];
    if (!activeSession.active) setMetadata(emptyMetadata);
  }, [activeSession.active, activeSession.startedAt, targetRef, connector]);

  useEffect(() => {
    metadataRowsRef.current = metadata.tables;
  }, [metadata.tables]);

  useEffect(() => {
    if (!activeSession.active) return undefined;
    const request = requestGuard.begin("metadata");
    setMetadata({ state: "loading", tables: [], error: "", truncated: false });
    apiPost("/api/connector-actions/local-run", metadataPayload(targetRef, connector), { signal: request.signal })
      .then((response) => {
        if (!request.isCurrent()) return;
        const item = requireCompletedConnectorAction(connectorActionResultResponse(response), "Could not load metadata suggestions.");
        if (!item) {
          setMetadata({ state: "pending", tables: [], error: "Metadata request is awaiting approval.", truncated: false });
          void refreshActivity();
          return;
        }
        const output = normalizeConnectorOutput(item.output);
        setMetadata((current) => ({
          state: "ready",
          tables: mergeMetadataRows(extractTableSuggestions(output), current.tables),
          error: "",
          truncated: Boolean(output?.truncated),
        }));
        void refreshActivity();
      })
      .catch((error) => {
        if (request.isCurrent())
          setMetadata({ state: "error", tables: [], error: errorMessage(error, "Could not load metadata suggestions."), truncated: false });
      })
      .finally(() => request.complete());
    return () => requestGuard.invalidate("metadata");
  }, [activeSession.active, activeSession.startedAt, connector, requestGuard, targetRef]);

  useEffect(() => {
    if (!activeSession.active || requestedTable?.owner !== owner) return;
    loadColumns({ ...requestedTable.reference, alias: "", schemaQuoted: true, tableQuoted: true, aliasQuoted: false });
  }, [activeSession.active, owner, requestedTable]);

  useEffect(() => {
    if (!activeSession.active || !sql.trim()) return undefined;
    const requests = columnRequestsRef.current;
    const missing = pendingMetadataReferences(sql, metadataRowsRef.current, requests, 4, connector.identifierPolicy);
    if (missing.length === 0) return undefined;
    const timer = window.setTimeout(() => {
      for (const reference of missing) {
        loadColumns(reference);
      }
    }, 250);
    return () => window.clearTimeout(timer);
  }, [activeSession.active, activeSession.startedAt, connector, requestGuard, sql, targetRef]);

  return { metadata, requestTableColumns: (reference: SQLTableReference) => setRequestedTable({ reference, owner }) };
}

function requestColumnMetadata({
  connector,
  reference,
  requestGuard,
  requests,
  requestSetRef,
  targetRef,
  setMetadata,
  metadataRowsRef,
  refreshActivity,
  identifierPolicy,
}: {
  connector: MetadataConnector;
  reference: SQLReference;
  requestGuard: RequestGuard;
  requests: Set<string>;
  requestSetRef: RefObject<Set<string>>;
  targetRef: string;
  setMetadata: Dispatch<SetStateAction<SQLMetadataState>>;
  metadataRowsRef: RefObject<SQLMetadataRow[]>;
  refreshActivity: () => Promise<unknown>;
  identifierPolicy: SQLIdentifierPolicy;
}): void {
  const requestKey = tableReferenceKey(reference, identifierPolicy);
  if (requests.has(requestKey)) return;
  requests.add(requestKey);
  const request = requestGuard.begin(`metadata:${requestKey}`);
  apiPost(
    "/api/connector-actions/local-run",
    {
      target_ref: targetRef,
      action_name: connector.describeAction,
      input: connector.describeInput(reference),
      reason: connector.metadataReason,
    },
    { signal: request.signal },
  )
    .then((response) => {
      if (requestSetRef.current !== requests || !request.isCurrent()) return;
      const item = requireCompletedConnectorAction(connectorActionResultResponse(response), "Could not load column metadata.");
      if (!item) {
        requests.delete(requestKey);
        void refreshActivity();
        return;
      }
      const nextRows = mergeMetadataRows(metadataRowsRef.current, extractTableSuggestions(item.output));
      metadataRowsRef.current = nextRows;
      setMetadata((current) => ({ ...current, state: "ready", tables: nextRows, error: "" }));
      void refreshActivity();
    })
    .catch(() => {
      if (request.isCurrent()) requests.delete(requestKey);
    })
    .finally(() => request.complete());
}

function metadataPayload(targetRef: string, connector: MetadataConnector) {
  return {
    target_ref: targetRef,
    action_name: connector.metadataAction,
    input: connector.metadataInput,
    reason: connector.metadataReason,
  };
}
