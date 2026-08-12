import { useState } from "react";
import { useAsyncAction } from "../../lib/use-async-action";
import { connectorModelMissingMessage, refreshAfterEditorMutation } from "./editor-support";

export function useCredentialProfileEditor({ defaultKind, targets, emptyStateForKind, modelForKind, onRefresh }) {
  const [drawer, setDrawer] = useState({ open: false, kind: defaultKind, mode: "create", row: null });
  const [formState, setFormState] = useState(() => emptyStateForKind(defaultKind, { targets }));
  const { actionState, setActionState, runAction, resetAction } = useAsyncAction();

  function resetForm(kind = defaultKind) {
    setFormState(emptyStateForKind(kind, { targets }));
  }

  function updateFormState(nextState) {
    setFormState(nextState);
  }

  function openCreate(kind = defaultKind) {
    resetAction();
    resetForm(kind);
    setDrawer({ open: true, kind, mode: "create", row: null });
  }

  function openEdit(row) {
    const model = modelForKind(row.connector_kind);
    if (!model?.credentialStateFromRow) {
      setActionState({ state: "error", error: connectorModelMissingMessage(row.connector_kind), message: null });
      return false;
    }
    resetAction();
    setFormState(model.credentialStateFromRow({ row, targets }));
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
      setActionState({ state: "error", error: connectorModelMissingMessage(drawer.kind), message: null });
      return false;
    }
    const result = await runAction({
      pending: operation === "import" ? "importing" : "saving",
      successMessage: (result) => result.value?.message || "Credential saved.",
      action: async () => {
        const value = await model.saveCredential({ operation, row: drawer.row, formState, targets });
        return { value };
      },
    });
    if (result === undefined) return false;
    const message = result.value?.message || "Credential saved.";
    setDrawer({ open: false, kind: defaultKind, mode: "create", row: null });
    resetForm(defaultKind);
    await refreshAfterEditorMutation(onRefresh, setActionState, message);
    return true;
  }

  async function remove(row) {
    const model = modelForKind(row.connector_kind);
    if (!model?.deleteCredential) {
      setActionState({ state: "error", error: connectorModelMissingMessage(row.connector_kind), message: null });
      return false;
    }
    const result = await runAction({
      pending: "deleting",
      successMessage: "Credential deleted.",
      action: async () => {
        await model.deleteCredential({ row });
        return true;
      },
    });
    if (result !== true) return false;
    await refreshAfterEditorMutation(onRefresh, setActionState, "Credential deleted.");
    return true;
  }

  return {
    drawer,
    formState,
    setFormState: updateFormState,
    actionState,
    openCreate,
    openEdit,
    closeEditor,
    save,
    remove,
  };
}
