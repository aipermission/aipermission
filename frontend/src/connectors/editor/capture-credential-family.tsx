import { useCallback, useEffect, useEffectEvent, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Drawer } from "../../components/ui/drawer";
import { CredentialRow } from "./credential-row";
import { DeleteCredentialDialog, defaultCredentialDeleteDialog } from "./credential-delete-dialog";
import type { CredentialDeleteDialogMetadata } from "./credential-delete-dialog";
import { useCredentialProfileEditor } from "./use-credential-profile-editor";
import type { CredentialFamilyDefinition, CredentialFamilyProps, RegisteredCredentialFamily } from "./credential-family-types";

// The closure owns the native state and row types; the registry sees only UI commands.
export function captureCredentialFamily<
  State extends object,
  Row extends { connector_kind: string },
  Target extends object,
  Operation extends string,
>(definition: CredentialFamilyDefinition<State, Row, Target, Operation>): RegisteredCredentialFamily {
  function Rows({ targets: inventory, credentials, busy, register, onOpen, onStateChange, onRowsChange, refresh }: CredentialFamilyProps) {
    const targets = useMemo(() => definition.decodeTargets(inventory), [inventory]);
    const rows = useMemo(() => definition.rows({ targets, credentials }), [targets, credentials]);
    const [deletion, setDeletion] = useState<{
      open: boolean;
      row: Row | null;
      dialog: CredentialDeleteDialogMetadata | null;
      attempted: boolean;
    }>({
      open: false,
      row: null,
      dialog: null,
      attempted: false,
    });
    const editor = useCredentialProfileEditor<State, Row, Target, Operation>({
      defaultKind: definition.kind,
      targets,
      emptyStateForKind: (_kind, context) => definition.emptyState(context),
      modelForKind: () => definition.model,
      onRefresh: refresh,
    });
    const current = useRef({ editor, onOpen, busy, mounted: false });
    useLayoutEffect(() => {
      current.current = { editor, onOpen, busy, mounted: true };
      return () => {
        current.current.mounted = false;
      };
    }, [editor, onOpen, busy]);
    const reportState = useEffectEvent(() => onStateChange(definition.kind, editor.actionState));
    useEffect(() => {
      reportState();
    }, [editor.actionState]);
    const reportRows = useEffectEvent((count: number | null) => onRowsChange?.(definition.kind, count));
    useEffect(() => {
      reportRows(rows.length);
      return () => reportRows(null);
    }, [rows.length]);
    const closeDelete = useCallback(() => {
      setDeletion({ open: false, row: null, dialog: null, attempted: false });
    }, []);
    const close = useCallback(() => {
      if (!current.current.mounted) return;
      current.current.editor.closeEditor();
      closeDelete();
    }, [closeDelete]);
    const activate = useCallback(() => {
      if (!current.current.mounted || current.current.busy) return false;
      current.current.onOpen(definition.kind);
      return current.current.mounted && !current.current.busy;
    }, []);
    const openCreate = useCallback(() => {
      const active = current.current;
      if (!active.mounted || active.busy || (active.editor.actionState.state !== "idle" && active.editor.actionState.state !== "error"))
        return;
      if (!activate()) return;
      closeDelete();
      current.current.editor.openCreate(definition.kind);
    }, [closeDelete, activate]);
    useEffect(() => {
      register(definition.kind, { openCreate, close });
      return () => register(definition.kind, null);
    }, [register, openCreate, close]);
    function openEdit(row: Row) {
      if (!activate()) return;
      closeDelete();
      editor.openEdit(row);
    }
    function requestDelete(row: Row) {
      if (!activate()) return;
      editor.closeEditor();
      const dialog = definition.deleteDialog?.({ row, targets }) || defaultCredentialDeleteDialog(definition.displayRow(row));
      setDeletion({ open: true, row, dialog, attempted: false });
    }
    async function confirmDelete() {
      if (!deletion.row || !current.current.mounted || current.current.busy) return;
      setDeletion((current) => ({ ...current, attempted: true }));
      if (await editor.remove(deletion.row)) closeDelete();
    }
    const state = editor.actionState;
    const disabled = busy || (state.state !== "idle" && state.state !== "error");
    const formEditor = {
      ...editor,
      save: async (...args: Parameters<typeof editor.save>) => {
        if (!current.current.mounted || current.current.busy) return false;
        return editor.save(...args);
      },
      setFormState: (...args: Parameters<typeof editor.setFormState>) => {
        if (current.current.mounted && !current.current.busy) editor.setFormState(...args);
      },
    };

    return (
      <>
        {rows.map((row) => (
          <CredentialRow
            key={definition.displayRow(row).row_id}
            row={definition.displayRow(row)}
            onEdit={() => openEdit(row)}
            onDelete={() => requestDelete(row)}
            busy={disabled}
            operations={definition.renderOperations?.(row)}
          />
        ))}
        <Drawer
          open={editor.drawer.open}
          title={`${editor.drawer.mode === "edit" ? "Edit" : "Add"} ${definition.label} credential`}
          description={
            editor.drawer.mode === "edit"
              ? "Update the connector credential profile metadata. Secrets are only replaced when you enter a new value."
              : "Choose the connector credential type, then fill the connector-specific profile form."
          }
          onClose={editor.closeEditor}
        >
          {editor.drawer.open ? (
            <fieldset disabled={busy || state.state === "saving" || state.state === "importing"} className="min-w-0">
              {definition.renderForm({ editor: formEditor, targets })}
            </fieldset>
          ) : null}
        </Drawer>
        <DeleteCredentialDialog value={deletion} state={state} disabled={busy} onClose={closeDelete} onDelete={confirmDelete} />
      </>
    );
  }
  return Object.freeze({ kind: definition.kind, Rows });
}
