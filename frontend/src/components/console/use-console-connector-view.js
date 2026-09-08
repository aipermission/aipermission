import { useCallback, useMemo, useState } from "react";
import { getConnectorTemplate } from "../../connectors/templates/registry";

const closedOperation = Object.freeze({ open: false, connector_kind: "", type: "", state: "idle", error: null });

export function useConsoleConnectorView({ resolveTemplate = getConnectorTemplate, selectedTarget }) {
  const [activityOpen, setActivityOpen] = useState(false);
  const [operation, setOperation] = useState(closedOperation);
  const selectedTemplate = useMemo(
    () => (selectedTarget ? resolveTemplate(selectedTarget.connector_kind) : null),
    [resolveTemplate, selectedTarget],
  );
  const OperationTemplate = useMemo(
    () => (operation.connector_kind ? resolveTemplate(operation.connector_kind)?.Operations || null : null),
    [operation.connector_kind, resolveTemplate],
  );
  const openOperation = useCallback((nextOperation) => {
    if (!nextOperation?.open || !nextOperation.connector_kind) return false;
    setOperation(nextOperation);
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
