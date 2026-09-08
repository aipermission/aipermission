import { useEffect, useEffectEvent, useRef, useState } from "react";
import { apiPost } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import { requireCompletedConnectorAction } from "./action-result";
import { extractTableSuggestions, normalizeConnectorOutput, pendingMetadataReferences, tableReferenceKey } from "./sql-console-data";
import { mergeMetadataRows } from "./sql-console-config";

const emptyMetadata = { state: "idle", tables: [], error: "", truncated: false };

export function useSQLMetadata({ activeSession, connector, onRefreshActivity, requestGuard, sql, targetRef }) {
  const [metadata, setMetadata] = useState(emptyMetadata);
  const metadataRowsRef = useRef([]);
  const columnRequestsRef = useRef(new Set());
  const refreshActivity = useEffectEvent(() => Promise.resolve(onRefreshActivity?.()).catch(() => undefined));

  useEffect(() => {
    columnRequestsRef.current = new Set();
    if (!activeSession.active) setMetadata(emptyMetadata);
  }, [activeSession.active, activeSession.startedAt, targetRef]);

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
        const item = requireCompletedConnectorAction(response, "Could not load metadata suggestions.");
        if (!item) {
          setMetadata({ state: "pending", tables: [], error: "Metadata request is awaiting approval.", truncated: false });
          void refreshActivity();
          return;
        }
        const output = normalizeConnectorOutput(item.output);
        setMetadata({ state: "ready", tables: extractTableSuggestions(output), error: "", truncated: Boolean(output?.truncated) });
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
    if (!activeSession.active || !sql.trim()) return undefined;
    const requests = columnRequestsRef.current;
    const missing = pendingMetadataReferences(sql, metadataRowsRef.current, requests, 4);
    if (missing.length === 0) return undefined;
    const timer = window.setTimeout(() => {
      for (const reference of missing) {
        requestColumnMetadata({
          connector,
          reference,
          requestGuard,
          requests,
          requestSetRef: columnRequestsRef,
          targetRef,
          setMetadata,
          metadataRowsRef,
          refreshActivity,
        });
      }
    }, 250);
    return () => window.clearTimeout(timer);
  }, [activeSession.active, connector, requestGuard, sql, targetRef]);

  return metadata;
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
}) {
  const requestKey = tableReferenceKey(reference);
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
      const item = requireCompletedConnectorAction(response, "Could not load column metadata.");
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

function metadataPayload(targetRef, connector) {
  return {
    target_ref: targetRef,
    action_name: connector.queryAction,
    input: { sql: connector.metadataSQL, max_rows: connector.metadataMaxRows },
    reason: connector.metadataReason,
  };
}
