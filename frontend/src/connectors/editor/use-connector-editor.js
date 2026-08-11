import { useEffect, useEffectEvent, useState } from "react";

const idleState = { state: "idle", error: null, message: "" };

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
  const [actionState, setActionState] = useState(idleState);
  const [dirty, setDirty] = useState(false);
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
    setActionState(idleState);
    resetForm(kind);
    setDrawer({ open: true, mode: "create", kind, target: null });
    setDirty(false);
  }

  function openEdit(target, profile) {
    const model = modelForKind(target.connector_kind);
    if (!model?.formFromTarget) {
      setActionState({ state: "error", error: `Connector model not found for ${target.connector_kind}.`, message: "" });
      return false;
    }
    if ((target.profiles || []).length > 0 && !profile) {
      setActionState({ state: "error", error: "Select a credential profile before editing profile-bound settings.", message: "" });
      return false;
    }
    setActionState(idleState);
    setForm({ ...model.formFromTarget({ target, profile }), project_id: target.project_id || defaultProjectID });
    setDrawer({ open: true, mode: "edit", kind: target.connector_kind, target });
    setDirty(false);
    return true;
  }

  function closeEditor() {
    const kind = form.connector_kind || defaultKind;
    setDrawer({ open: false, mode: "create", kind: defaultKind, target: null });
    resetForm(kind);
    setActionState(idleState);
    setDirty(false);
  }

  function selectKind(kind) {
    setActionState(idleState);
    setForm((current) => ({ ...emptyFormForKind(kind, { firstCredentialID }), project_id: current.project_id || defaultProjectID }));
    setDrawer((current) => ({ ...current, kind }));
    setDirty(true);
  }

  function updateField(field, value) {
    setForm((current) => ({ ...current, [field]: value }));
    setDirty(true);
  }

  async function save(event) {
    event?.preventDefault?.();
    const model = modelForKind(form.connector_kind);
    if (!model?.save) {
      setActionState({ state: "error", error: `Connector model not found for ${form.connector_kind}.`, message: "" });
      return false;
    }
    setActionState({ state: "saving", error: null, message: "" });
    try {
      await model.save({ mode: drawer.mode, form, target: drawer.target });
      const message = drawer.mode === "edit" ? "Connector updated." : "Connector created.";
      const kind = form.connector_kind;
      setDrawer({ open: false, mode: "create", kind, target: null });
      resetForm(kind);
      setDirty(false);
      setActionState({ state: "idle", error: null, message });
      await onRefresh?.();
      return true;
    } catch (error) {
      const operation = model.operationFromError?.(error, { mode: drawer.mode, form, target: drawer.target });
      if (operation?.open && onOperation?.(operation)) {
        setActionState(idleState);
        return false;
      }
      setActionState({ state: "error", error: error.message, message: "" });
      return false;
    }
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
      setActionState({ state: "error", error: `Connector model not found for ${target.connector_kind}.`, message: "" });
      return false;
    }
    setActionState({ state: "deleting", error: null, message: "" });
    try {
      await model.deleteTarget({ target, removeKey });
      closeDelete();
      setActionState({ state: "idle", error: null, message: "Connector deleted." });
      await onRefresh?.();
      return true;
    } catch (error) {
      setActionState({ state: "error", error: error.message, message: "" });
      return false;
    }
  }

  function completeOperation(result, operation) {
    const kind = operation?.connector_kind || operation?.kind || form.connector_kind;
    setDrawer({ open: false, mode: "create", kind, target: null });
    resetForm(kind);
    setDirty(false);
    setActionState({ state: "idle", error: null, message: result?.message || "Connector updated." });
  }

  return {
    drawer,
    deleteDialog,
    form,
    actionState,
    dirty,
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
