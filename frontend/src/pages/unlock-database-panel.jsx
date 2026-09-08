import { useEffect, useState } from "react";
import { ChevronDown, ExternalLink, LockKeyhole, Trash2 } from "lucide-react";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { Input } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { apiPost } from "../lib/api";

export function UnlockDatabasePanel({ database, unsupported, migrationRequired, onMigrationRequired, onDeleted, runLifecycleMutation }) {
  const [password, setPassword] = useState("");
  const [action, setAction] = useState("unlock");
  const [actionMenuOpen, setActionMenuOpen] = useState(false);
  const [deleteDialog, setDeleteDialog] = useState({ open: false, confirmName: "", state: "idle", error: null });
  const [state, setState] = useState({ state: "idle", error: null });

  useEffect(() => {
    setAction("unlock");
    setActionMenuOpen(false);
  }, [database?.id]);

  async function unlockDatabase(event) {
    event.preventDefault();
    setState({ state: "unlocking", error: null });
    try {
      await runLifecycleMutation(`unlock:${database?.id || ""}`, (signal) =>
        apiPost("/api/unlock", { database_id: database?.id, password }, { signal }),
      );
    } catch (error) {
      if (isMigrationRequiredError(error) && database?.id) onMigrationRequired(database.id);
      setState({ state: "error", error: error.message });
    }
  }

  function openDeleteDialog() {
    if (!password) {
      setState({ state: "error", error: "Enter the database password before deleting this local database." });
      return;
    }
    setState({ state: "idle", error: null });
    setDeleteDialog({ open: true, confirmName: "", state: "idle", error: null });
  }

  function closeDeleteDialog() {
    setDeleteDialog((current) => ({ ...current, open: false }));
    setAction("unlock");
    setActionMenuOpen(false);
  }

  async function deleteLockedDatabase(event) {
    event.preventDefault();
    if (!database) return;
    setDeleteDialog((current) => ({ ...current, state: "deleting", error: null }));
    try {
      await runLifecycleMutation(`delete:${database.id}`, (signal) =>
        apiPost("/api/databases/delete-locked", { database_id: database.id, current_password: password }, { signal }),
      );
      setDeleteDialog({ open: false, confirmName: "", state: "idle", error: null });
      setPassword("");
      onDeleted(database.id);
    } catch (error) {
      setDeleteDialog((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  return (
    <>
      <form className="grid gap-4" onSubmit={unlockDatabase}>
        <div className="grid gap-2">
          <label htmlFor="unlock-database-password" className="text-sm font-semibold text-stone-800">
            Database password
          </label>
          <Input
            id="unlock-database-password"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
          />
        </div>
        {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
        {migrationRequired ? (
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" asChild>
              <a href="http://localhost:3211" target="_blank" rel="noreferrer">
                <ExternalLink className="h-4 w-4" />
                Open migration helper
              </a>
            </Button>
            <Button type="button" variant="danger" onClick={openDeleteDialog}>
              <Trash2 className="h-4 w-4" />
              Delete old local copy
            </Button>
          </div>
        ) : null}
        <UnlockActionControl
          action={action}
          menuOpen={actionMenuOpen}
          databaseAvailable={Boolean(database)}
          deleting={deleteDialog.state === "deleting"}
          unlocking={state.state === "unlocking"}
          unlockDisabled={unsupported || migrationRequired}
          onActionChange={setAction}
          onDelete={openDeleteDialog}
          onMenuOpenChange={setActionMenuOpen}
        />
      </form>
      <DeleteDatabaseDialog
        database={database}
        dialog={deleteDialog}
        onChange={setDeleteDialog}
        onClose={closeDeleteDialog}
        onSubmit={deleteLockedDatabase}
      />
    </>
  );
}

function UnlockActionControl({
  action,
  menuOpen,
  databaseAvailable,
  deleting,
  unlocking,
  unlockDisabled,
  onActionChange,
  onDelete,
  onMenuOpenChange,
}) {
  const deletingAction = action === "delete";
  return (
    <div className="relative">
      <div className="grid grid-cols-[minmax(0,1fr)_44px] overflow-hidden rounded-md">
        <Button
          type={deletingAction ? "button" : "submit"}
          variant={deletingAction ? "danger" : "default"}
          className="rounded-r-none"
          disabled={deletingAction ? !databaseAvailable || deleting : unlocking || unlockDisabled}
          onClick={deletingAction ? onDelete : undefined}
        >
          {deletingAction ? <Trash2 className="h-4 w-4" /> : <LockKeyhole className="h-4 w-4" />}
          {deletingAction ? "Delete this local database" : unlocking ? "Unlocking..." : "Unlock"}
        </Button>
        <Button
          type="button"
          variant={deletingAction ? "danger" : "default"}
          className={`rounded-l-none px-0 ${deletingAction ? "border-l border-red-800" : "border-l border-emerald-800"}`}
          aria-expanded={menuOpen}
          aria-label={deletingAction ? "Choose unlock action" : "Choose database action"}
          title={deletingAction ? "Choose unlock action" : "Choose database action"}
          disabled={!databaseAvailable || unlocking}
          onClick={() => onMenuOpenChange(!menuOpen)}
        >
          <ChevronDown className={`h-4 w-4 transition ${menuOpen ? "rotate-180" : ""}`} />
        </Button>
      </div>
      {menuOpen ? (
        <div className="absolute left-0 right-0 top-full z-20 mt-1 overflow-hidden rounded-md border border-stone-200 bg-white shadow-xl">
          <button
            type="button"
            className={`flex w-full items-center justify-center gap-2 px-3 py-2 text-center text-sm font-semibold text-white ${
              deletingAction ? "bg-emerald-950 hover:bg-emerald-900" : "bg-red-700 hover:bg-red-800"
            }`}
            onClick={() => {
              onActionChange(deletingAction ? "unlock" : "delete");
              onMenuOpenChange(false);
            }}
          >
            {deletingAction ? <LockKeyhole className="h-4 w-4" /> : <Trash2 className="h-4 w-4" />}
            {deletingAction ? "Unlock" : "Delete this local database"}
          </button>
        </div>
      ) : null}
    </div>
  );
}

function DeleteDatabaseDialog({ database, dialog, onChange, onClose, onSubmit }) {
  return (
    <Dialog
      open={dialog.open}
      title="Delete local database"
      description={database ? `Delete ${database.name} from this local gateway.` : ""}
      onClose={onClose}
      closeDisabled={dialog.state === "deleting"}
      closeOnOverlay={false}
      size="md"
    >
      <form className="grid gap-4" onSubmit={onSubmit}>
        <Notice tone="bad">
          This local database will be deleted permanently from this gateway. If you have not migrated or backed it up, its local
          configuration will be lost.
        </Notice>
        <div className="rounded-md border border-stone-200 bg-stone-50 p-3 text-sm text-stone-700">
          <span className="font-semibold text-stone-900">Database:</span> {database?.name || "Unknown"}
        </div>
        <div className="grid gap-2">
          <label htmlFor="delete-database-confirmation" className="text-sm font-semibold text-stone-800">
            Type the database name to confirm
          </label>
          <Input
            id="delete-database-confirmation"
            type="text"
            value={dialog.confirmName}
            onChange={(event) => onChange((current) => ({ ...current, confirmName: event.target.value }))}
            placeholder={database?.name || "Database name"}
            required
          />
        </div>
        {dialog.state === "error" ? <Notice tone="bad">{dialog.error}</Notice> : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={dialog.state === "deleting"}>
            Cancel
          </Button>
          <Button type="submit" variant="danger" disabled={dialog.state === "deleting" || dialog.confirmName !== database?.name}>
            <Trash2 className="h-4 w-4" />
            {dialog.state === "deleting" ? "Deleting..." : "Delete permanently"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function isMigrationRequiredError(error) {
  return error?.status === 409 && /pre-0\.2|non-baseline schema|migration helper/i.test(error?.message || "");
}
