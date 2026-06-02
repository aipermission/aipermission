import { Clock3, Download, Edit3, KeyRound, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { apiDownload, apiGet, apiPost, apiPut } from "../lib/api";
import { useAsyncAction } from "../lib/use-async-action";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { CopyButton } from "../components/ui/copy-button";
import { Dialog } from "../components/ui/dialog";
import { Field, Input } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { isValidDatabasePassword } from "../lib/password";

const emptyState = { state: "idle", error: null, message: null };

export function SettingsPage() {
  const [database, setDatabase] = useState({ state: "loading", data: null, error: null });
  const { actionState: backupState, runAction: runBackupAction } = useAsyncAction(emptyState);
  const { actionState: passwordState, runAction: runPasswordAction } = useAsyncAction(emptyState);
  const { actionState: renameState, runAction: runRenameAction } = useAsyncAction(emptyState);
  const { actionState: deleteState, runAction: runDeleteAction } = useAsyncAction(emptyState);
  const { actionState: retentionState, runAction: runRetentionAction } = useAsyncAction(emptyState);
  const { actionState: purgeState, runAction: runPurgeAction } = useAsyncAction(emptyState);
  const [retention, setRetention] = useState({
    state: "loading",
    data: { history_days: 0, audit_days: 0, console_days: 0, message_days: 0 },
    error: null,
  });
  const [passwordForm, setPasswordForm] = useState({ current_password: "", new_password: "", confirm_password: "" });
  const [renameName, setRenameName] = useState("");
  const [deleteName, setDeleteName] = useState("");
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deletePassword, setDeletePassword] = useState("");
  const deletePasswordRef = useRef(null);

  async function loadDatabase() {
    try {
      const data = await apiGet("/api/unlock/status");
      setDatabase({ state: "ready", data, error: null });
      setRenameName(data.database_name || "");
    } catch (error) {
      setDatabase({ state: "error", data: null, error: error.message });
    }
  }

  async function loadRetention() {
    try {
      const data = await apiGet("/api/settings/retention");
      setRetention({ state: "ready", data, error: null });
    } catch (error) {
      setRetention((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  useEffect(() => {
    void loadDatabase();
    void loadRetention();
  }, []);

  const databaseName = database.data?.database_name || "Unknown";
  const newPasswordValid = isValidDatabasePassword(passwordForm.new_password);

  async function downloadDatabase() {
    await runBackupAction({
      pending: "downloading",
      successMessage: "Encrypted database downloaded.",
      action: () => apiDownload("/api/backup/download", `${databaseName}-${new Date().toISOString().slice(0, 19)}.aipdb`),
    });
  }

  async function renameDatabase(event) {
    event.preventDefault();
    const result = await runRenameAction({
      pending: "saving",
      successMessage: "Database renamed. Unlock it again to continue.",
      action: () => apiPost("/api/databases/rename", { database_name: renameName }),
    });
    if (result !== undefined) window.setTimeout(() => window.location.reload(), 800);
  }

  function updatePasswordField(field, value) {
    setPasswordForm((current) => ({ ...current, [field]: value }));
  }

  async function changePassword(event) {
    event.preventDefault();
    await runPasswordAction({
      pending: "saving",
      successMessage: "Database password changed. Future unlocks and new backups use the new password.",
      action: async () => {
        await apiPost("/api/databases/change-password", passwordForm);
        setPasswordForm({ current_password: "", new_password: "", confirm_password: "" });
      },
    });
  }

  function requestDeleteDatabase(event) {
    event.preventDefault();
    setDeleteDialogOpen(true);
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

  useEffect(() => {
    if (!deleteDialogOpen) return;
    window.setTimeout(() => deletePasswordRef.current?.focus(), 0);
  }, [deleteDialogOpen]);

  function updateRetentionField(field, value) {
    const numeric = Number.parseInt(value, 10);
    setRetention((current) => ({
      ...current,
      data: {
        ...current.data,
        [field]: Number.isFinite(numeric) && numeric >= 0 ? numeric : 0,
      },
    }));
  }

  async function saveRetention(event) {
    event.preventDefault();
    await runRetentionAction({
      pending: "saving",
      successMessage: "Retention settings saved and cleanup ran.",
      action: async () => {
        const data = await apiPut("/api/settings/retention", retention.data);
        setRetention({ state: "ready", data, error: null });
      },
    });
  }

  async function purgeRetention(target, days) {
    const ok = window.confirm(`Delete ${target} records older than ${days} days? This cannot be undone.`);
    if (!ok) return;
    await runPurgeAction({
      pending: "purging",
      successMessage: (data) => `Deleted ${data.deleted} ${target} records.`,
      action: () => apiPost("/api/settings/retention/purge", { target, days }),
    });
  }

  return (
    <section className="mx-auto grid w-full max-w-2xl gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">Settings</h3>
          <p className="text-sm text-stone-500">Manage the current encrypted database backup, password, rename, and delete lifecycle.</p>
        </div>
      </div>

      {database.state === "error" ? <Notice tone="bad">{database.error}</Notice> : null}

      <Card>
        <CardHeader>
          <CardTitle>Backup</CardTitle>
          <CardDescription>Download the currently unlocked encrypted database file.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <Notice>The downloaded file is already protected by its database password.</Notice>
          <Button type="button" onClick={downloadDatabase} disabled={backupState.state === "downloading"}>
            <Download className="h-4 w-4" />
            {backupState.state === "downloading" ? "Downloading..." : "Download database"}
          </Button>
          {backupState.message ? <Notice tone="good">{backupState.message}</Notice> : null}
          {backupState.state === "error" ? <Notice tone="bad">{backupState.error}</Notice> : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Data retention</CardTitle>
          <CardDescription>Keep History and Audit usable by cleaning old local records.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4" onSubmit={saveRetention}>
            <Notice>
              Cleanup runs when a database is unlocked and immediately after saving these settings. Use 0 to disable automatic cleanup for a category.
            </Notice>
            {retention.state === "error" ? <Notice tone="bad">{retention.error}</Notice> : null}
            <div className="grid gap-3 sm:grid-cols-2">
              <RetentionField
                label="Command history days"
                value={retention.data.history_days}
                onChange={(value) => updateRetentionField("history_days", value)}
              />
              <RetentionField
                label="Audit log days"
                value={retention.data.audit_days}
                onChange={(value) => updateRetentionField("audit_days", value)}
              />
              <RetentionField
                label="Console session days"
                value={retention.data.console_days}
                onChange={(value) => updateRetentionField("console_days", value)}
              />
              <RetentionField
                label="Message days"
                value={retention.data.message_days}
                onChange={(value) => updateRetentionField("message_days", value)}
              />
            </div>
            <Button type="submit" variant="outline" disabled={retentionState.state === "saving" || retention.state === "loading"}>
              <Clock3 className="h-4 w-4" />
              {retentionState.state === "saving" ? "Saving..." : "Save retention"}
            </Button>
            {retentionState.message ? <Notice tone="good">{retentionState.message}</Notice> : null}
            {retentionState.state === "error" ? <Notice tone="bad">{retentionState.error}</Notice> : null}

            <div className="grid gap-3 rounded-md border border-stone-200 p-3">
              <div>
                <h4 className="text-sm font-semibold text-stone-900">Manual cleanup</h4>
                <p className="text-xs text-stone-500">Run a one-time purge without changing automatic retention settings.</p>
              </div>
              <div className="grid gap-2 sm:grid-cols-2">
                <Button type="button" variant="outline" onClick={() => purgeRetention("history", 30)} disabled={purgeState.state === "purging"}>
                  Purge history older than 30 days
                </Button>
                <Button type="button" variant="outline" onClick={() => purgeRetention("audit", 30)} disabled={purgeState.state === "purging"}>
                  Purge audit older than 30 days
                </Button>
                <Button type="button" variant="outline" onClick={() => purgeRetention("console", 7)} disabled={purgeState.state === "purging"}>
                  Purge consoles older than 7 days
                </Button>
                <Button type="button" variant="outline" onClick={() => purgeRetention("messages", 7)} disabled={purgeState.state === "purging"}>
                  Purge messages older than 7 days
                </Button>
              </div>
              {purgeState.message ? <Notice tone="good">{purgeState.message}</Notice> : null}
              {purgeState.state === "error" ? <Notice tone="bad">{purgeState.error}</Notice> : null}
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Change password</CardTitle>
          <CardDescription>Re-encrypt the current database with a new unlock password.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4" onSubmit={changePassword}>
            <Notice>Downloaded backups keep the password they had when they were created.</Notice>
            <Field>
              Current password
              <Input
                type="password"
                value={passwordForm.current_password}
                onChange={(event) => updatePasswordField("current_password", event.target.value)}
                autoComplete="current-password"
                required
              />
            </Field>
            <Field>
              New password
              <Input
                type="password"
                value={passwordForm.new_password}
                onChange={(event) => updatePasswordField("new_password", event.target.value)}
                autoComplete="new-password"
                minLength={14}
                required
              />
            </Field>
            <Field>
              Confirm new password
              <Input
                type="password"
                value={passwordForm.confirm_password}
                onChange={(event) => updatePasswordField("confirm_password", event.target.value)}
                autoComplete="new-password"
                minLength={14}
                required
              />
            </Field>
            <Notice>Use at least 14 characters with uppercase letters, lowercase letters, and numbers.</Notice>
            <Button
              type="submit"
              variant="outline"
              disabled={
                passwordState.state === "saving" ||
                !newPasswordValid ||
                passwordForm.new_password !== passwordForm.confirm_password ||
                passwordForm.current_password === passwordForm.new_password
              }
            >
              <KeyRound className="h-4 w-4" />
              {passwordState.state === "saving" ? "Changing..." : "Change password"}
            </Button>
            {passwordState.message ? <Notice tone="good">{passwordState.message}</Notice> : null}
            {passwordState.state === "error" ? <Notice tone="bad">{passwordState.error}</Notice> : null}
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Rename</CardTitle>
          <CardDescription>Rename the current database. You will unlock it again after rename.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4" onSubmit={renameDatabase}>
            <Field>
              Database name
              <Input value={renameName} onChange={(event) => setRenameName(event.target.value)} required />
            </Field>
            <Button type="submit" variant="outline" disabled={renameState.state === "saving" || renameName.trim() === databaseName}>
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
          <form className="grid gap-4" onSubmit={requestDeleteDatabase}>
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
            <Button type="submit" variant="danger" disabled={deleteState.state === "deleting" || deleteName !== databaseName || !deletePassword}>
              <Trash2 className="h-4 w-4" />
              {deleteState.state === "deleting" ? "Deleting..." : "Delete permanently"}
            </Button>
          </div>
        </form>
      </Dialog>
    </section>
  );
}

function RetentionField({ label, value, onChange }) {
  return (
    <Field>
      {label}
      <Input type="number" min="0" step="1" value={value} onChange={(event) => onChange(event.target.value)} />
    </Field>
  );
}
