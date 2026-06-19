import { Archive, Clock3, Cloud, Download, Edit3, KeyRound, Play, Plus, Tags, Terminal, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { apiDelete, apiDownload, apiGet, apiPost, apiPut } from "../lib/api";
import { useAsyncAction } from "../lib/use-async-action";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { CopyButton } from "../components/ui/copy-button";
import { Dialog } from "../components/ui/dialog";
import { Field, Input, Select, Textarea } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { isValidDatabasePassword } from "../lib/password";
import { TerminalBlock } from "../components/ui/terminal-block";

const emptyState = { state: "idle", error: null, message: null };

export function SettingsPage() {
  const [database, setDatabase] = useState({ state: "loading", data: null, error: null });
  const { actionState: backupState, runAction: runBackupAction } = useAsyncAction(emptyState);
  const { actionState: passwordState, runAction: runPasswordAction } = useAsyncAction(emptyState);
  const { actionState: renameState, runAction: runRenameAction } = useAsyncAction(emptyState);
  const { actionState: deleteState, runAction: runDeleteAction } = useAsyncAction(emptyState);
  const { actionState: retentionState, runAction: runRetentionAction } = useAsyncAction(emptyState);
  const { actionState: purgeState, runAction: runPurgeAction } = useAsyncAction(emptyState);
  const { actionState: labelDeleteState, runAction: runLabelDeleteAction } = useAsyncAction(emptyState);
  const { actionState: maintenanceState, runAction: runMaintenanceAction } = useAsyncAction(emptyState);
  const { actionState: backupProviderState, runAction: runBackupProviderAction } = useAsyncAction(emptyState);
  const [retention, setRetention] = useState({
    state: "loading",
    data: { history_days: 0, audit_days: 0, console_days: 0, message_days: 0 },
    error: null,
  });
  const [labels, setLabels] = useState({ state: "loading", data: [], error: null });
  const [selectedLabelID, setSelectedLabelID] = useState("");
  const [labelDeleteDialogOpen, setLabelDeleteDialogOpen] = useState(false);
  const [passwordForm, setPasswordForm] = useState({ current_password: "", new_password: "", confirm_password: "" });
  const [renameName, setRenameName] = useState("");
  const [deleteName, setDeleteName] = useState("");
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deletePassword, setDeletePassword] = useState("");
  const deletePasswordRef = useRef(null);
  const [maintenanceOpen, setMaintenanceOpen] = useState(false);
  const [maintenanceOpenError, setMaintenanceOpenError] = useState("");
  const [maintenanceCommand, setMaintenanceCommand] = useState("pwd\nls -la");
  const [maintenanceTimeout, setMaintenanceTimeout] = useState(10);
  const [maintenanceResult, setMaintenanceResult] = useState(null);
  const [backupProviderCatalog, setBackupProviderCatalog] = useState({ state: "loading", data: [], error: null });
  const [backupProviders, setBackupProviders] = useState({ state: "loading", data: [], error: null });
  const [backupProviderDialogOpen, setBackupProviderDialogOpen] = useState(false);
  const [backupProviderArchiveTarget, setBackupProviderArchiveTarget] = useState(null);
  const [backupProviderEditingID, setBackupProviderEditingID] = useState(null);
  const [backupProviderForm, setBackupProviderForm] = useState({
    provider_type: "google_drive",
    name: "Google Drive",
    status: "active",
    folder_name: "AIPermission Backups",
  });

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

  async function loadHistoryLabels() {
    try {
      const data = await apiGet("/api/history-labels");
      setLabels({ state: "ready", data: data || [], error: null });
    } catch (error) {
      setLabels({ state: "error", data: [], error: error.message });
    }
  }

  async function loadBackupProviderCatalog() {
    try {
      const data = await apiGet("/api/backup/providers/catalog");
      setBackupProviderCatalog({ state: "ready", data: data?.items || [], error: null });
    } catch (error) {
      setBackupProviderCatalog({ state: "error", data: [], error: error.message });
    }
  }

  async function loadBackupProviders() {
    try {
      const data = await apiGet("/api/backup/providers");
      setBackupProviders({ state: "ready", data: data?.items || [], error: null });
    } catch (error) {
      setBackupProviders({ state: "error", data: [], error: error.message });
    }
  }

  useEffect(() => {
    void loadDatabase();
    void loadRetention();
    void loadHistoryLabels();
    void loadBackupProviderCatalog();
    void loadBackupProviders();
  }, []);

  const databaseName = database.data?.database_name || "Unknown";
  const newPasswordValid = isValidDatabasePassword(passwordForm.new_password);
  const selectedLabel = labels.data.find((label) => String(label.id) === String(selectedLabelID));

  async function downloadDatabase() {
    await runBackupAction({
      pending: "downloading",
      successMessage: "Encrypted database downloaded.",
      action: () => apiDownload("/api/backup/download", `${databaseName}-${new Date().toISOString().slice(0, 19)}.aipdb`),
    });
  }

  function openBackupProviderDialog(provider = null) {
    if (provider) {
      setBackupProviderEditingID(provider.id);
      setBackupProviderForm({
        provider_type: provider.provider_type,
        name: provider.name,
        status: provider.status,
        folder_name: provider.public?.folder_name || "AIPermission Backups",
      });
    } else {
      const firstType = backupProviderCatalog.data[0]?.provider_type || "google_drive";
      setBackupProviderEditingID(null);
      setBackupProviderForm({
        provider_type: firstType,
        name: providerLabel(firstType, backupProviderCatalog.data),
        status: "active",
        folder_name: "AIPermission Backups",
      });
    }
    setBackupProviderDialogOpen(true);
  }

  function closeBackupProviderDialog() {
    if (backupProviderState.state === "saving") return;
    setBackupProviderDialogOpen(false);
  }

  function updateBackupProviderField(field, value) {
    setBackupProviderForm((current) => ({ ...current, [field]: value }));
  }

  async function saveBackupProvider(event) {
    event.preventDefault();
    const payload = {
      provider_type: backupProviderForm.provider_type,
      name: backupProviderForm.name,
      status: backupProviderForm.status,
      public: {
        folder_name: backupProviderForm.folder_name,
      },
    };
    await runBackupProviderAction({
      pending: "saving",
      successMessage: backupProviderEditingID ? "Backup provider updated." : "Backup provider added.",
      action: async () => {
        if (backupProviderEditingID) {
          await apiPut(`/api/backup/providers/${backupProviderEditingID}`, payload);
        } else {
          await apiPost("/api/backup/providers", payload);
        }
        setBackupProviderDialogOpen(false);
        await loadBackupProviders();
      },
    });
  }

  function closeBackupProviderArchiveDialog() {
    if (backupProviderState.state === "archiving") return;
    setBackupProviderArchiveTarget(null);
  }

  async function archiveBackupProvider(event) {
    event.preventDefault();
    const provider = backupProviderArchiveTarget;
    if (!provider) return;
    await runBackupProviderAction({
      pending: "archiving",
      successMessage: `Archived backup provider "${provider.name}".`,
      action: async () => {
        await apiDelete(`/api/backup/providers/${provider.id}`);
        setBackupProviderArchiveTarget(null);
        await loadBackupProviders();
      },
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

  async function deleteHistoryLabel(event) {
    event.preventDefault();
    if (!selectedLabel) return;
    const deletedLabel = selectedLabel;
    await runLabelDeleteAction({
      pending: "deleting",
      successMessage: `Deleted history label "${deletedLabel.name}".`,
      action: async () => {
        await apiDelete(`/api/history-labels/${deletedLabel.id}`);
        setSelectedLabelID("");
        setLabelDeleteDialogOpen(false);
        await loadHistoryLabels();
      },
    });
  }

  function closeLabelDeleteDialog() {
    if (labelDeleteState.state === "deleting") return;
    setLabelDeleteDialogOpen(false);
  }

  async function openMaintenanceConsole() {
    setMaintenanceOpenError("");
    try {
      await apiPost("/api/settings/maintenance-console/open", {});
      setMaintenanceOpen(true);
    } catch (error) {
      setMaintenanceOpenError(error.message);
    }
  }

  async function closeMaintenanceConsole() {
    if (maintenanceState.state === "running") return;
    setMaintenanceOpen(false);
    try {
      await apiPost("/api/settings/maintenance-console/close", {});
    } catch {
      // The dialog is already local UI state; failing to audit close should not trap the user.
    }
  }

  async function runMaintenanceCommand(event) {
    event.preventDefault();
    const command = maintenanceCommand.trim();
    if (!command) return;
    await runMaintenanceAction({
      pending: "running",
      successMessage: (data) => `Maintenance command ${data.status}.`,
      action: async () => {
        const data = await apiPost("/api/settings/maintenance-console/run", {
          command,
          timeout_seconds: Number(maintenanceTimeout) || 10,
        });
        setMaintenanceResult(data);
        return data;
      },
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
          <CardDescription>Download the current encrypted database and prepare optional remote backup providers.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <Notice>The downloaded file is already protected by its database password.</Notice>
          <Button type="button" onClick={downloadDatabase} disabled={backupState.state === "downloading"}>
            <Download className="h-4 w-4" />
            {backupState.state === "downloading" ? "Downloading..." : "Download database"}
          </Button>
          {backupState.message ? <Notice tone="good">{backupState.message}</Notice> : null}
          {backupState.state === "error" ? <Notice tone="bad">{backupState.error}</Notice> : null}
          <div className="grid gap-3 border-t border-stone-200 pt-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h4 className="text-sm font-semibold text-stone-900">Remote backup providers</h4>
                <p className="text-xs text-stone-500">Provider metadata is local. Remote backup/restore actions will use these records.</p>
              </div>
              <Button type="button" variant="outline" onClick={() => openBackupProviderDialog()}>
                <Plus className="h-4 w-4" />
                Add provider
              </Button>
            </div>
            {backupProviderCatalog.state === "error" ? <Notice tone="bad">{backupProviderCatalog.error}</Notice> : null}
            {backupProviders.state === "error" ? <Notice tone="bad">{backupProviders.error}</Notice> : null}
            {backupProviderState.message ? <Notice tone="good">{backupProviderState.message}</Notice> : null}
            {backupProviderState.state === "error" ? <Notice tone="bad">{backupProviderState.error}</Notice> : null}
            <div className="grid gap-2">
              {backupProviders.data.length === 0 ? (
                <div className="rounded-md border border-dashed border-stone-300 px-3 py-4 text-sm text-stone-500">
                  No remote backup providers configured.
                </div>
              ) : (
                backupProviders.data.map((provider) => (
                  <div key={provider.id} className="grid gap-3 rounded-md border border-stone-200 p-3 sm:grid-cols-[minmax(0,1fr)_auto]">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <Cloud className="h-4 w-4 text-emerald-600" />
                        <p className="truncate text-sm font-semibold text-stone-950">{provider.name}</p>
                        <span className="rounded-full bg-stone-100 px-2 py-0.5 text-[11px] font-semibold uppercase text-stone-600">
                          {providerLabel(provider.provider_type, backupProviderCatalog.data)}
                        </span>
                        <span className={provider.status === "active" ? "rounded-full bg-emerald-100 px-2 py-0.5 text-[11px] font-semibold text-emerald-700" : "rounded-full bg-stone-100 px-2 py-0.5 text-[11px] font-semibold text-stone-600"}>
                          {provider.status}
                        </span>
                      </div>
                      <p className="mt-1 truncate text-xs text-stone-500">{provider.public?.folder_name || "AIPermission Backups"}</p>
                    </div>
                    <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                      <Button type="button" variant="outline" className="h-9 px-3" onClick={() => openBackupProviderDialog(provider)}>
                        <Edit3 className="h-4 w-4" />
                        Edit
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        className="h-9 px-3"
                        onClick={() => setBackupProviderArchiveTarget(provider)}
                        disabled={backupProviderState.state === "archiving"}
                      >
                        <Archive className="h-4 w-4" />
                        Archive
                      </Button>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Maintenance console</CardTitle>
          <CardDescription>Run a bounded command inside the local AIPermission gateway runtime.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <Notice tone="warn">
            Local UI-only diagnostics for the gateway runtime. It is not exposed to MCP, output is bounded, and every submitted command is audited.
          </Notice>
          <Button type="button" onClick={openMaintenanceConsole}>
            <Terminal className="h-4 w-4" />
            Open maintenance console
          </Button>
          {maintenanceOpenError ? <Notice tone="bad">{maintenanceOpenError}</Notice> : null}
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
          <CardTitle>History labels</CardTitle>
          <CardDescription>Manage labels used to organize command history.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <Notice>Deleting a label removes it from related history entries. The history records stay intact.</Notice>
          {labels.state === "error" ? <Notice tone="bad">{labels.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
            <Select
              value={selectedLabelID}
              onChange={(event) => setSelectedLabelID(event.target.value)}
              disabled={labels.state === "loading" || labels.data.length === 0}
            >
              <option value="">{labels.state === "loading" ? "Loading labels..." : "Select a label"}</option>
              {labels.data.map((label) => (
                <option key={label.id} value={label.id}>
                  {label.name}
                </option>
              ))}
            </Select>
            <Button
              type="button"
              variant="outline"
              onClick={() => setLabelDeleteDialogOpen(true)}
              disabled={!selectedLabel || labelDeleteState.state === "deleting"}
            >
              <Trash2 className="h-4 w-4" />
              Delete label
            </Button>
          </div>
          {labels.state === "ready" && labels.data.length === 0 ? <Notice>No labels yet. Add labels from a history detail.</Notice> : null}
          {labelDeleteState.message ? <Notice tone="good">{labelDeleteState.message}</Notice> : null}
          {labelDeleteState.state === "error" ? <Notice tone="bad">{labelDeleteState.error}</Notice> : null}
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
        open={backupProviderDialogOpen}
        title={backupProviderEditingID ? "Edit backup provider" : "Add backup provider"}
        description="Store provider metadata for encrypted database backups."
        onClose={closeBackupProviderDialog}
        size="md"
      >
        <form className="grid gap-4" onSubmit={saveBackupProvider}>
          <Notice>Remote providers store encrypted database files only. They do not receive MCP tokens, connector credentials, or the database password.</Notice>
          <Field>
            Provider type
            <Select
              value={backupProviderForm.provider_type}
              onChange={(event) => {
                const providerType = event.target.value;
                setBackupProviderForm((current) => ({
                  ...current,
                  provider_type: providerType,
                  name: current.name || providerLabel(providerType, backupProviderCatalog.data),
                }));
              }}
              disabled={Boolean(backupProviderEditingID)}
            >
              {backupProviderCatalog.data.map((item) => (
                <option key={item.provider_type} value={item.provider_type}>
                  {item.label}
                </option>
              ))}
            </Select>
          </Field>
          <Field>
            Name
            <Input value={backupProviderForm.name} onChange={(event) => updateBackupProviderField("name", event.target.value)} required />
          </Field>
          <Field>
            Folder name
            <Input value={backupProviderForm.folder_name} onChange={(event) => updateBackupProviderField("folder_name", event.target.value)} required />
          </Field>
          <Field>
            Status
            <Select value={backupProviderForm.status} onChange={(event) => updateBackupProviderField("status", event.target.value)}>
              <option value="active">Active</option>
              <option value="disabled">Disabled</option>
            </Select>
          </Field>
          {backupProviderState.state === "error" ? <Notice tone="bad">{backupProviderState.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={closeBackupProviderDialog} disabled={backupProviderState.state === "saving"}>
              Cancel
            </Button>
            <Button type="submit" disabled={backupProviderState.state === "saving" || !backupProviderForm.name.trim()}>
              <Cloud className="h-4 w-4" />
              {backupProviderState.state === "saving" ? "Saving..." : "Save provider"}
            </Button>
          </div>
        </form>
      </Dialog>

      <Dialog
        open={Boolean(backupProviderArchiveTarget)}
        title="Archive backup provider"
        description={backupProviderArchiveTarget ? `Archive "${backupProviderArchiveTarget.name}"?` : "Archive backup provider?"}
        onClose={closeBackupProviderArchiveDialog}
        size="md"
      >
        <form className="grid gap-4" onSubmit={archiveBackupProvider}>
          <Notice tone="warn">This removes the provider from Settings. Existing remote backup files are not deleted.</Notice>
          <div className="rounded-md border border-stone-200 bg-stone-50 px-3 py-2">
            <p className="text-xs font-semibold uppercase text-stone-500">Provider</p>
            <p className="mt-1 truncate text-sm font-semibold text-stone-950">{backupProviderArchiveTarget?.name || "-"}</p>
          </div>
          {backupProviderState.state === "error" ? <Notice tone="bad">{backupProviderState.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={closeBackupProviderArchiveDialog} disabled={backupProviderState.state === "archiving"}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" disabled={!backupProviderArchiveTarget || backupProviderState.state === "archiving"}>
              <Archive className="h-4 w-4" />
              {backupProviderState.state === "archiving" ? "Archiving..." : "Archive provider"}
            </Button>
          </div>
        </form>
      </Dialog>

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

      <Dialog
        open={maintenanceOpen}
        title="Maintenance console"
        description="Run a bounded command inside the local gateway container."
        onClose={closeMaintenanceConsole}
        size="xl"
        closeDisabled={maintenanceState.state === "running"}
        closeOnOverlay={false}
      >
        <form className="grid gap-4" onSubmit={runMaintenanceCommand}>
          <Notice tone="warn">
            This console is for local diagnostics only. Do not print secrets; command text is written to the audit log.
          </Notice>
          <Field>
            Command
            <Textarea
              value={maintenanceCommand}
              onChange={(event) => setMaintenanceCommand(event.target.value)}
              className="min-h-32 font-mono text-xs"
              spellCheck={false}
              required
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-[minmax(0,12rem)_auto]">
            <Field>
              Timeout seconds
              <Input
                type="number"
                min="1"
                max="30"
                value={maintenanceTimeout}
                onChange={(event) => setMaintenanceTimeout(event.target.value)}
              />
            </Field>
            <Button type="submit" className="self-end" disabled={maintenanceState.state === "running" || !maintenanceCommand.trim()}>
              <Play className="h-4 w-4" />
              {maintenanceState.state === "running" ? "Running..." : "Run command"}
            </Button>
          </div>
          {maintenanceState.message ? <Notice tone={maintenanceResult?.status === "completed" ? "good" : "warn"}>{maintenanceState.message}</Notice> : null}
          {maintenanceState.state === "error" ? <Notice tone="bad">{maintenanceState.error}</Notice> : null}
          {maintenanceResult ? (
            <div className="grid gap-3">
              <div className="flex flex-wrap items-center gap-2 text-xs text-stone-500">
                <span className="inline-flex items-center gap-1 rounded-full border border-stone-200 px-2 py-1 font-semibold text-stone-700">
                  <Terminal className="h-3.5 w-3.5" />
                  {maintenanceResult.status}
                </span>
                {maintenanceResult.exit_code !== undefined ? <span>exit {maintenanceResult.exit_code}</span> : null}
                <span>{maintenanceResult.duration_ms} ms</span>
                {maintenanceResult.output_truncated ? <span>output truncated</span> : null}
              </div>
              <div className="grid gap-2">
                <p className="text-xs font-semibold uppercase text-stone-500">stdout</p>
                <TerminalBlock surface="log" className="max-h-72 min-h-28 text-xs">
                  {maintenanceResult.stdout || "(empty)"}
                </TerminalBlock>
              </div>
              {maintenanceResult.stderr ? (
                <div className="grid gap-2">
                  <p className="text-xs font-semibold uppercase text-stone-500">stderr</p>
                  <TerminalBlock surface="log" className="max-h-48 min-h-20 text-xs">
                    {maintenanceResult.stderr}
                  </TerminalBlock>
                </div>
              ) : null}
            </div>
          ) : null}
        </form>
      </Dialog>

      <Dialog
        open={labelDeleteDialogOpen}
        title="Delete history label"
        description={selectedLabel ? `Remove "${selectedLabel.name}" from history?` : "Select a history label first."}
        onClose={closeLabelDeleteDialog}
        size="md"
      >
        <form className="grid gap-4" onSubmit={deleteHistoryLabel}>
          <Notice tone="bad">
            This removes the label from every related history entry. Command history records, outputs, and audit logs are not deleted.
          </Notice>
          <div className="rounded-md border border-stone-200 bg-stone-50 px-3 py-2">
            <p className="text-xs font-semibold uppercase text-stone-500">Selected label</p>
            <p className="mt-1 truncate text-sm font-semibold text-stone-950">{selectedLabel?.name || "-"}</p>
          </div>
          {labelDeleteState.state === "error" ? <Notice tone="bad">{labelDeleteState.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={closeLabelDeleteDialog} disabled={labelDeleteState.state === "deleting"}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" disabled={!selectedLabel || labelDeleteState.state === "deleting"}>
              <Tags className="h-4 w-4" />
              {labelDeleteState.state === "deleting" ? "Deleting..." : "Delete label"}
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

function providerLabel(providerType, catalog) {
  return catalog.find((item) => item.provider_type === providerType)?.label || providerType;
}
