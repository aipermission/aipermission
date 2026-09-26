import { BackupRestoreDialog } from "./backup-restore-dialog";
import { ProvisionUserDialog } from "./provision-user-dialog";
import type { PostgresOperation, ProvisionOperationProps } from "./operation-types";

const closedOperation: PostgresOperation = { open: false };
type OperationInput = Omit<PostgresOperation, "connector_kind" | "type"> & { connector_kind?: string; type?: string };

export function PostgresConnectorOperationsTemplate({
  value,
  onChange,
  onOperationComplete,
}: {
  value?: OperationInput;
  onChange: (_value: PostgresOperation) => void;
  onOperationComplete?: ProvisionOperationProps["onOperationComplete"];
}) {
  const operation: PostgresOperation =
    value?.connector_kind === "postgres" && (value.type === "provision-user" || value.type === "backup-restore")
      ? { ...value, connector_kind: "postgres", type: value.type }
      : closedOperation;

  function close() {
    onChange({ open: false, connector_kind: "", type: "", state: "idle", error: null });
  }

  return (
    <>
      <ProvisionUserDialog
        value={operation.type === "provision-user" ? operation : closedOperation}
        onClose={close}
        onOperationComplete={onOperationComplete}
      />
      <BackupRestoreDialog value={operation.type === "backup-restore" ? operation : closedOperation} onClose={close} />
    </>
  );
}
