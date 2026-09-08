import { BackupRestoreDialog } from "./backup-restore-dialog";
import { ProvisionUserDialog } from "./provision-user-dialog";

const closedOperation = { open: false };

export function PostgresConnectorOperationsTemplate({ value, onChange, onOperationComplete }) {
  const operation = value?.connector_kind === "postgres" ? value : closedOperation;

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
