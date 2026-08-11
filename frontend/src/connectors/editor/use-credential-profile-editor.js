import { useState } from "react";
import { useAsyncAction } from "../../lib/use-async-action";

export function useCredentialProfileEditor({ defaultKind, targets, emptyStateForKind, modelForKind, onRefresh }) {
  const [drawer, setDrawer] = useState({ open: false, kind: defaultKind, mode: "create", row: null });
  const [formState, setFormState] = useState(() => emptyStateForKind(defaultKind, { targets }));
  const [dirty, setDirty] = useState(false);
  const { actionState, setActionState, runAction, resetAction } = useAsyncAction();

  function resetForm(kind = defaultKind) {
    setFormState(emptyStateForKind(kind, { targets }));
    setDirty(false);
  }

  function updateFormState(nextState) {
    setFormState(nextState);
    setDirty(true);
  }

  function openCreate(kind = defaultKind) {
    resetAction();
    resetForm(kind);
    setDrawer({ open: true, kind, mode: "create", row: null });
  }

  function openEdit(row) {
    const model = modelForKind(row.connector_kind);
    if (!model?.credentialStateFromRow) {
      setActionState({ state: "error", error: `Connector model not found for ${row.connector_kind}.`, message: null });
      return false;
    }
    resetAction();
    setFormState(model.credentialStateFromRow({ row, targets }));
    setDirty(false);
    setDrawer({ open: true, kind: row.connector_kind, mode: "edit", row });
    return true;
  }

  function closeEditor() {
    setDrawer({ open: false, kind: defaultKind, mode: "create", row: null });
    resetForm(defaultKind);
    resetAction();
  }

  async function save(event, operation) {
    event?.preventDefault?.();
    const model = modelForKind(drawer.kind);
    if (!model?.saveCredential) {
      setActionState({ state: "error", error: `Connector model not found for ${drawer.kind}.`, message: null });
      return false;
    }
    const result = await runAction({
      pending: operation === "import" ? "importing" : "saving",
      successMessage: (value) => value?.message || "Credential saved.",
      action: async () => {
        const value = await model.saveCredential({ operation, row: drawer.row, formState, targets });
        setDrawer({ open: false, kind: defaultKind, mode: "create", row: null });
        resetForm(defaultKind);
        await onRefresh?.();
        return value;
      },
    });
    return result !== undefined;
  }

  async function remove(row) {
    const model = modelForKind(row.connector_kind);
    if (!model?.deleteCredential) {
      setActionState({ state: "error", error: `Connector model not found for ${row.connector_kind}.`, message: null });
      return false;
    }
    const result = await runAction({
      pending: "deleting",
      successMessage: "Credential deleted.",
      action: async () => {
        await model.deleteCredential({ row });
        await onRefresh?.();
        return true;
      },
    });
    return result === true;
  }

  return {
    drawer,
    formState,
    setFormState: updateFormState,
    dirty,
    actionState,
    openCreate,
    openEdit,
    closeEditor,
    save,
    remove,
  };
}
