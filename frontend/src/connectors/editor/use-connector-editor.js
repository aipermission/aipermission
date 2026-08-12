import { useEffect, useEffectEvent, useState } from "react";
import { useAsyncAction } from "../../lib/use-async-action";
import { connectorModelMissingMessage, refreshAfterEditorMutation } from "./editor-support";

export function useConnectorEditor({
  defaultKind,
  firstCredentialID,
  defaultProjectID,
  emptyFormForKind,
  modelForKind,
  onRefresh,
  onOperation,
}) {
  const [drawer, setDrawer] = useState({ open: false, mode: "create", kind: defaultKind, target: null });
  const [deleteDialog, setDeleteDialog] = useState({ open: false, target: null });
  const [form, setForm] = useState(() => emptyFormForKind(defaultKind));
  const { actionState, setActionState, runAction, resetAction } = useAsyncAction();
  const syncCredentialForEffect = useEffectEvent(() => {
    setForm((current) => modelForKind(current.connector_kind)?.syncForm?.({ form: current, firstCredentialID }) || current);
  });

  useEffect(() => {
    syncCredentialForEffect();
  }, [firstCredentialID]);

  function resetForm(kind = defaultKind) {
    setForm({ ...emptyFormForKind(kind, { firstCredentialID }), project_id: defaultProjectID });
  }

  function openCreate(kind = defaultKind) {
    resetAction();
    resetForm(kind);
    setDrawer({ open: true, mode: "create", kind, target: null });
  }

  function openEdit(target, profile) {
    const model = modelForKind(target.connector_kind);
    if (!model?.formFromTarget) {
      setActionState({ state: "error", error: connectorModelMissingMessage(target.connector_kind), message: null });
      return false;
    }
    if ((target.profiles || []).length > 0 && !profile) {
      setActionState({ state: "error", error: "Select a credential profile before editing profile-bound settings.", message: null });
      return false;
    }
    resetAction();
    setForm({ ...model.formFromTarget({ target, profile }), project_id: target.project_id || defaultProjectID });
    setDrawer({ open: true, mode: "edit", kind: target.connector_kind, target });
    return true;
  }

  function closeEditor() {
    setDrawer({ open: false, mode: "create", kind: defaultKind, target: null });
    resetForm(defaultKind);
    resetAction();
  }

  function selectKind(kind) {
    resetAction();
    setForm((current) => ({ ...emptyFormForKind(kind, { firstCredentialID }), project_id: current.project_id || defaultProjectID }));
    setDrawer((current) => ({ ...current, kind }));
  }

  function updateField(field, value) {
    setForm((current) => ({ ...current, [field]: value }));
  }

  async function save(event) {
    event?.preventDefault?.();
    const model = modelForKind(form.connector_kind);
    if (!model?.save) {
      setActionState({ state: "error", error: connectorModelMissingMessage(form.connector_kind), message: null });
      return false;
    }
    const message = drawer.mode === "edit" ? "Connector updated." : "Connector created.";
    const result = await runAction({
      pending: "saving",
      successMessage: message,
      action: async () => {
        await model.save({ mode: drawer.mode, form, target: drawer.target });
        return true;
      },
      onError: (error) => {
        const operation = model.operationFromError?.(error, { mode: drawer.mode, form, target: drawer.target });
        return Boolean(operation?.open && onOperation?.(operation));
      },
    });
    if (result !== true) return false;
    const kind = form.connector_kind;
    setDrawer({ open: false, mode: "create", kind, target: null });
    resetForm(kind);
    await refreshAfterEditorMutation(onRefresh, setActionState, message);
    return true;
  }

  function requestDelete(target) {
    setDeleteDialog({ open: true, target });
  }

  function closeDelete() {
    setDeleteDialog({ open: false, target: null });
  }

  async function remove(removeKey) {
    const target = deleteDialog.target;
    if (!target) return false;
    const model = modelForKind(target.connector_kind);
    if (!model?.deleteTarget) {
      setActionState({ state: "error", error: connectorModelMissingMessage(target.connector_kind), message: null });
      return false;
    }
    const message = "Connector deleted.";
    const result = await runAction({
      pending: "deleting",
      successMessage: message,
      action: async () => {
        await model.deleteTarget({ target, removeKey });
        return true;
      },
    });
    if (result !== true) return false;
    closeDelete();
    await refreshAfterEditorMutation(onRefresh, setActionState, message);
    return true;
  }

  function completeOperation(result, operation) {
    const kind = operation?.connector_kind || operation?.kind || form.connector_kind;
    setDrawer({ open: false, mode: "create", kind, target: null });
    resetForm(kind);
    setActionState({ state: "idle", error: null, message: result?.message || "Connector updated." });
  }

  return {
    drawer,
    deleteDialog,
    form,
    actionState,
    openCreate,
    openEdit,
    closeEditor,
    selectKind,
    updateField,
    save,
    requestDelete,
    closeDelete,
    remove,
    completeOperation,
  };
}
