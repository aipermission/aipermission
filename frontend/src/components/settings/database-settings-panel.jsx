import { Edit3, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { apiPost } from "../../lib/api";
import { useAsyncAction } from "../../lib/use-async-action";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { CopyButton } from "../ui/copy-button";
import { Dialog } from "../ui/dialog";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";
import { PasswordSettingsPanel } from "./password-settings-panel";

export function DatabaseSettingsPanel({ databaseName }) {
  const [renameName, setRenameName] = useState(databaseName);
  const [renamePassword, setRenamePassword] = useState("");
  const [deleteName, setDeleteName] = useState("");
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deletePassword, setDeletePassword] = useState("");
  const deletePasswordRef = useRef(null);
  const { actionState: renameState, runAction: runRenameAction } = useAsyncAction();
  const { actionState: deleteState, runAction: runDeleteAction } = useAsyncAction();

  useEffect(() => setRenameName(databaseName), [databaseName]);
  useEffect(() => {
    if (!deleteDialogOpen) return;
    window.setTimeout(() => deletePasswordRef.current?.focus(), 0);
  }, [deleteDialogOpen]);

  async function renameDatabase(event) {
    event.preventDefault();
    const result = await runRenameAction({
      pending: "saving",
      successMessage: "Database renamed. Unlock it again to continue.",
      action: async () => {
        try {
          return await apiPost("/api/databases/rename", { database_name: renameName.trim(), current_password: renamePassword });
        } finally {
          setRenamePassword("");
        }
      },
    });
    if (result !== undefined) window.setTimeout(() => window.location.reload(), 800);
  }

  async function deleteDatabase(event) {
    event.preventDefault();
    const result = await runDeleteAction({
      pending: "deleting",
      successMessage: "Database deleted.",
      action: () => apiPost("/api/databases/delete", { confirm_name: deleteName, current_password: deletePassword }),
    });
    if (result !== undefined) window.setTimeout(() => window.location.reload(), 800);
  }

  function closeDeleteDialog() {
    if (deleteState.state === "deleting") return;
    setDeleteDialogOpen(false);
    setDeletePassword("");
  }

  return (
    <>
      <PasswordSettingsPanel />
      <Card>
        <CardHeader>
          <CardTitle>Rename</CardTitle>
          <CardDescription>Rename the current database. Confirm its password now, then unlock it again after rename.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4" onSubmit={renameDatabase}>
            <Field>
              Database name
              <Input value={renameName} onChange={(event) => setRenameName(event.target.value)} required />
            </Field>
            <Field>
              Current database password
              <Input
                type="password"
                value={renamePassword}
                onChange={(event) => setRenamePassword(event.target.value)}
                autoComplete="current-password"
                required
              />
            </Field>
            <Button
              type="submit"
              variant="outline"
              disabled={renameState.state === "saving" || renameName.trim() === databaseName || !renamePassword}
            >
              <Edit3 className="h-4 w-4" />
              {renameState.state === "saving" ? "Renaming..." : "Rename database"}
            </Button>
            {renameState.message ? <Notice tone="good">{renameState.message}</Notice> : null}
            {renameState.state === "error" ? <Notice tone="bad">{renameState.error}</Notice> : null}
          </form>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Delete</CardTitle>
          <CardDescription>This permanently removes the current database file from the local Docker volume.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4" onSubmit={(event) => (event.preventDefault(), setDeleteDialogOpen(true))}>
            <Notice tone="bad">
              <span className="flex flex-nowrap items-center gap-2 text-xs">
                <span className="min-w-0 shrink">Take a backup first. To confirm deletion, type the database name exactly:</span>
                <CopyButton value={databaseName} variant="outline" className="h-7 shrink-0 px-2 text-xs" iconClassName="h-3.5 w-3.5">
                  <span className="max-w-40 truncate">{databaseName}</span>
                </CopyButton>
              </span>
            </Notice>
            <Field>
              Confirm database name
              <Input value={deleteName} onChange={(event) => setDeleteName(event.target.value)} required />
            </Field>
            <Button type="submit" variant="danger" disabled={deleteState.state === "deleting" || deleteName !== databaseName}>
              <Trash2 className="h-4 w-4" />
              {deleteState.state === "deleting" ? "Deleting..." : "Delete database"}
            </Button>
            {deleteState.message ? <Notice tone="good">{deleteState.message}</Notice> : null}
            {deleteState.state === "error" ? <Notice tone="bad">{deleteState.error}</Notice> : null}
          </form>
        </CardContent>
      </Card>
      <Dialog
        open={deleteDialogOpen}
        title="Delete database"
        description={`This permanently removes ${databaseName} from the local Docker volume.`}
        onClose={closeDeleteDialog}
        size="md"
        autoFocusClose={false}
      >
        <form className="grid gap-4" onSubmit={deleteDatabase}>
          <Notice tone="bad">This cannot be undone. Take a backup first, then enter the current database password.</Notice>
          <div className="rounded-md border border-stone-200 bg-stone-50 px-3 py-2">
            <p className="text-xs font-semibold uppercase text-stone-500">Database name</p>
            <p className="mt-1 truncate text-sm font-semibold text-stone-950">{databaseName}</p>
          </div>
          <Field>
            Current database password
            <Input
              ref={deletePasswordRef}
              type="password"
              value={deletePassword}
              onChange={(event) => setDeletePassword(event.target.value)}
              autoComplete="current-password"
              required
            />
          </Field>
          {deleteState.state === "error" ? <Notice tone="bad">{deleteState.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={closeDeleteDialog} disabled={deleteState.state === "deleting"}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant="danger"
              disabled={deleteState.state === "deleting" || deleteName !== databaseName || !deletePassword}
            >
              <Trash2 className="h-4 w-4" />
              {deleteState.state === "deleting" ? "Deleting..." : "Delete permanently"}
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
