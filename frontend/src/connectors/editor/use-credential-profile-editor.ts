import { useRef, useState } from "react";
import type { SetStateAction } from "react";
import { useAsyncAction } from "../../lib/use-async-action";
import { connectorModelMissingMessage, refreshAfterEditorMutation } from "./editor-support";

type CredentialRow = { connector_kind: string };
type CredentialTarget = { [field: string]: unknown };
type SaveResult = { message?: string } | undefined;
type CredentialModel<FormState, Row, Target, Operation> = {
  credentialStateFromRow?: (_context: { row: Row; targets: Target[] }) => FormState;
  saveCredential?: (_context: {
    operation: Operation;
    row: Row | null;
    formState: FormState;
    targets: Target[];
  }) => SaveResult | Promise<SaveResult>;
  deleteCredential?: (_context: { row: Row }) => unknown | Promise<unknown>;
};
type Props<FormState, Row, Target, Operation> = {
  defaultKind: string;
  targets: readonly Target[];
  emptyStateForKind: (_kind: string, _context: { targets: Target[] }) => FormState;
  modelForKind: (_kind: string) => CredentialModel<FormState, Row, Target, Operation> | null;
  onRefresh?: () => void | Promise<void>;
};
type Drawer<Row> = { open: boolean; kind: string; mode: "create" | "edit"; row: Row | null };

export function useCredentialProfileEditor<
  FormState extends object,
  Row extends CredentialRow,
  Target extends object = CredentialTarget,
  Operation extends string = string,
>({ defaultKind, targets, emptyStateForKind, modelForKind, onRefresh }: Props<FormState, Row, Target, Operation>) {
  const [drawer, setDrawer] = useState<Drawer<Row>>({ open: false, kind: defaultKind, mode: "create", row: null });
  const [formState, setFormState] = useState<FormState>(() => emptyStateForKind(defaultKind, { targets: [...targets] }));
  const { actionState, setActionState, runAction, resetAction } = useAsyncAction();
  const pendingSaveRef = useRef<symbol | null>(null);
  const editorEpochRef = useRef(0);
  const editorEpoch = editorEpochRef.current;

  function resetForm(kind: string = defaultKind) {
    setFormState(emptyStateForKind(kind, { targets: [...targets] }));
  }

  function updateFormState(nextState: SetStateAction<FormState>) {
    if (pendingSaveRef.current !== null || editorEpoch !== editorEpochRef.current) return;
    setFormState(nextState);
  }

  function openCreate(kind: string = defaultKind) {
    editorEpochRef.current += 1;
    pendingSaveRef.current = null;
    resetAction();
    resetForm(kind);
    setDrawer({ open: true, kind, mode: "create", row: null });
  }

  function openEdit(row: Row) {
    const model = modelForKind(row.connector_kind);
    if (!model?.credentialStateFromRow) {
      setActionState({ state: "error", error: connectorModelMissingMessage(row.connector_kind), message: null });
      return false;
    }
    editorEpochRef.current += 1;
    pendingSaveRef.current = null;
    resetAction();
    setFormState(model.credentialStateFromRow({ row, targets: [...targets] }));
    setDrawer({ open: true, kind: row.connector_kind, mode: "edit", row });
    return true;
  }

  function closeEditor() {
    editorEpochRef.current += 1;
    pendingSaveRef.current = null;
    setDrawer({ open: false, kind: defaultKind, mode: "create", row: null });
    resetForm(defaultKind);
    resetAction();
  }

  async function save(event: { preventDefault?: () => void } | null, operation: Operation) {
    event?.preventDefault?.();
    if (pendingSaveRef.current !== null || editorEpoch !== editorEpochRef.current) return false;
    const model = modelForKind(drawer.kind);
    const saveCredential = model?.saveCredential;
    if (!saveCredential) {
      setActionState({ state: "error", error: connectorModelMissingMessage(drawer.kind), message: null });
      return false;
    }
    const saveToken = Symbol("credential-save");
    pendingSaveRef.current = saveToken;
    try {
      const result = await runAction({
        pending: operation === "import" ? "importing" : "saving",
        successMessage: (result) => result.value?.message || "Credential saved.",
        action: async () => {
          const value = await saveCredential.call(model, { operation, row: drawer.row, formState, targets: [...targets] });
          return { value };
        },
      });
      if (result === undefined) return false;
      const message = result.value?.message || "Credential saved.";
      editorEpochRef.current += 1;
      setDrawer({ open: false, kind: defaultKind, mode: "create", row: null });
      resetForm(defaultKind);
      await refreshAfterEditorMutation(onRefresh, setActionState, message);
      return true;
    } finally {
      if (pendingSaveRef.current === saveToken) pendingSaveRef.current = null;
    }
  }

  async function remove(row: Row) {
    if (editorEpoch !== editorEpochRef.current) return false;
    const model = modelForKind(row.connector_kind);
    const deleteCredential = model?.deleteCredential;
    if (!deleteCredential) {
      setActionState({ state: "error", error: connectorModelMissingMessage(row.connector_kind), message: null });
      return false;
    }
    const result = await runAction({
      pending: "deleting",
      successMessage: "Credential deleted.",
      action: async () => {
        await deleteCredential.call(model, { row });
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
