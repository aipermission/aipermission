import { useEffect, useRef, useState } from "react";
import { localActionReconciliationEvent } from "../lib/local-action-retry";
import type { ReconciliationDetail } from "../lib/local-action-retry/runtime.ts";
export type { ReconciliationDetail } from "../lib/local-action-retry/runtime.ts";

export function useLocalActionReconciliation() {
  const [value, setValue] = useState<ReconciliationDetail | null>(null);
  const resolverRef = useRef<ReconciliationDetail["resolve"] | null>(null);

  useEffect(() => {
    function requestReconciliation(event: Event) {
      if (!(event instanceof CustomEvent) || !isReconciliationDetail(event.detail)) return;
      event.preventDefault();
      resolverRef.current?.(false);
      resolverRef.current = event.detail.resolve;
      setValue(event.detail);
    }
    window.addEventListener(localActionReconciliationEvent, requestReconciliation);
    return () => {
      window.removeEventListener(localActionReconciliationEvent, requestReconciliation);
      resolverRef.current?.(false);
      resolverRef.current = null;
    };
  }, []);

  function close(confirmed: boolean) {
    resolverRef.current?.(confirmed);
    resolverRef.current = null;
    setValue(null);
  }

  return [value, close] as const;
}

function isReconciliationDetail(value: unknown): value is ReconciliationDetail {
  if (!value || typeof value !== "object") return false;
  const detail = value as Record<string, unknown>;
  return (
    typeof detail.resolve === "function" &&
    (detail.requestID === undefined || detail.requestID === null || typeof detail.requestID === "number") &&
    (detail.assistantHint === undefined || typeof detail.assistantHint === "string") &&
    (detail.operationRef === undefined || typeof detail.operationRef === "string") &&
    (detail.createdAt === undefined || typeof detail.createdAt === "string")
  );
}
