import { useCallback, useMemo, useState } from "react";
import { getConnectorTemplate } from "../../connectors/templates/registry";
import type { ConsoleConnectorViewOptions, ConsoleOperation } from "./console-connector-view-types";

const closedOperation: Readonly<ConsoleOperation> = Object.freeze({
  open: false,
  connector_kind: "",
  type: "",
  state: "idle",
  error: null,
});

export function useConsoleConnectorView({ resolveTemplate = getConnectorTemplate, selectedTarget }: ConsoleConnectorViewOptions) {
  const [activityOpen, setActivityOpen] = useState(false);
  const [operation, setOperation] = useState<ConsoleOperation>(closedOperation);
  const selectedTemplate = useMemo(
    () => (selectedTarget ? resolveTemplate(selectedTarget.connector_kind) : null),
    [resolveTemplate, selectedTarget],
  );
  const OperationTemplate = useMemo(
    () => (operation.connector_kind ? resolveTemplate(operation.connector_kind)?.Operations || null : null),
    [operation.connector_kind, resolveTemplate],
  );
  const openOperation = useCallback((nextOperation: unknown) => {
    if (!nextOperation || typeof nextOperation !== "object" || Array.isArray(nextOperation)) return false;
    if (!("open" in nextOperation) || nextOperation.open !== true) return false;
    if (!("connector_kind" in nextOperation) || typeof nextOperation.connector_kind !== "string" || !nextOperation.connector_kind)
      return false;
    const type = "type" in nextOperation ? nextOperation.type : undefined;
    const state = "state" in nextOperation ? nextOperation.state : undefined;
    const error = "error" in nextOperation ? nextOperation.error : undefined;
    if (type !== undefined && typeof type !== "string") return false;
    if (state !== undefined && typeof state !== "string") return false;
    if (error !== undefined && error !== null && typeof error !== "string") return false;
    setOperation({
      ...nextOperation,
      open: true,
      connector_kind: nextOperation.connector_kind,
      ...(typeof type === "string" ? { type } : {}),
      ...(typeof state === "string" ? { state } : {}),
      ...(typeof error === "string" || error === null ? { error } : {}),
    });
    return true;
  }, []);

  return {
    activityOpen,
    closeActivity: () => setActivityOpen(false),
    Console: selectedTemplate?.Console || null,
    openActivity: () => setActivityOpen(true),
    openOperation,
    operation,
    OperationTemplate,
    setOperation,
    ToolbarActions: selectedTemplate?.ToolbarActions || null,
  };
}
