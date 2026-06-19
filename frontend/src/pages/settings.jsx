import { Archive, Clock3, Cloud, Download, Edit3, FileDown, KeyRound, Plus, RotateCcw, Tags, Terminal, Trash2, Upload } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { apiDelete, apiDownload, apiGet, apiPost, apiPut, apiUrl } from "../lib/api";
import { useAsyncAction } from "../lib/use-async-action";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { CopyButton } from "../components/ui/copy-button";
import { Dialog } from "../components/ui/dialog";
import { Field, Input, Select } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { isValidDatabasePassword } from "../lib/password";
import { PtyConsole } from "../components/console/pty-console";

const emptyState = { state: "idle", error: null, message: null };
const googleDriveProviderGuideURL = "https://github.com/aipermission/aipermission/blob/main/docs/providers/google-drive.md";

export function SettingsPage() {
  const [database, setDatabase] = useState({ state: "loading", data: null, error: null });
  const { actionState: backupState, runAction: runBackupAction } = useAsyncAction(emptyState);
  const { actionState: passwordState, runAction: runPasswordAction } = useAsyncAction(emptyState);
  const { actionState: renameState, runAction: runRenameAction } = useAsyncAction(emptyState);
  const { actionState: deleteState, runAction: runDeleteAction } = useAsyncAction(emptyState);
  const { actionState: retentionState, runAction: runRetentionAction } = useAsyncAction(emptyState);
  const { actionState: purgeState, runAction: runPurgeAction } = useAsyncAction(emptyState);
  const { actionState: labelDeleteState, runAction: runLabelDeleteAction } = useAsyncAction(emptyState);
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
  const maintenanceSocketRef = useRef(null);
  const [maintenanceSession, setMaintenanceSession] = useState({ transcript: "", status: "closed", error: null, shell: "" });
  const [backupProviderCatalog, setBackupProviderCatalog] = useState({ state: "loading", data: [], error: null });
  const [backupProviders, setBackupProviders] = useState({ state: "loading", data: [], error: null });
  const [backupProviderDialogOpen, setBackupProviderDialogOpen] = useState(false);
  const [backupProviderArchiveTarget, setBackupProviderArchiveTarget] = useState(null);
  const [backupProviderEditingID, setBackupProviderEditingID] = useState(null);
  const [googleAuthProvider, setGoogleAuthProvider] = useState(null);
  const [googleDeviceFlow, setGoogleDeviceFlow] = useState(null);
  const [googleAuthState, setGoogleAuthState] = useState(emptyState);
  const [backupUploadTarget, setBackupUploadTarget] = useState(null);
  const [backupRecordsProvider, setBackupRecordsProvider] = useState(null);
  const [backupRecords, setBackupRecords] = useState({ state: "idle", data: [], error: null });
  const [restoreRecordTarget, setRestoreRecordTarget] = useState(null);
  const [restoreRecordForm, setRestoreRecordForm] = useState({ database_name: "", database_password: "" });
  const [backupProviderForm, setBackupProviderForm] = useState({
    provider_type: "google_drive",
    name: "Google Drive",
    status: "active",
    folder_name: "AIPermission Backups",
    client_id: "",
    client_secret: "",
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

  useEffect(() => {
    return () => {
      maintenanceSocketRef.current?.close();
      maintenanceSocketRef.current = null;
    };
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
        client_id: provider.public?.client_id || "",
        client_secret: "",
      });
    } else {
      const firstType = backupProviderCatalog.data[0]?.provider_type || "google_drive";
      setBackupProviderEditingID(null);
      setBackupProviderForm({
        provider_type: firstType,
        name: providerLabel(firstType, backupProviderCatalog.data),
        status: "active",
        folder_name: "AIPermission Backups",
        client_id: "",
        client_secret: "",
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
        client_id: backupProviderForm.client_id.trim(),
      },
    };
    if (backupProviderForm.client_secret.trim()) {
      payload.secret = { client_secret: backupProviderForm.client_secret.trim() };
    }
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

  function requestUploadBackupProvider(provider) {
    setBackupUploadTarget(provider);
  }

  function closeUploadBackupDialog() {
    if (backupProviderState.state === `uploading-${backupUploadTarget?.id}`) return;
    setBackupUploadTarget(null);
  }

  async function uploadBackupProvider(event) {
    event.preventDefault();
    const provider = backupUploadTarget;
    if (!provider) return;
    await runBackupProviderAction({
      pending: `uploading-${provider.id}`,
      successMessage: (record) => `Uploaded ${record.filename} to ${provider.name}.`,
      action: async () => {
        const record = await apiPost(`/api/backup/providers/${provider.id}/upload`, {});
        setBackupUploadTarget(null);
        await loadBackupProviders();
        return record;
      },
    });
  }

  async function openBackupRecordsDialog(provider) {
    setBackupRecordsProvider(provider);
    setBackupRecords({ state: "loading", data: [], error: null });
    try {
      const data = await apiGet(`/api/backup/providers/${provider.id}/records`);
      setBackupRecords({ state: "ready", data: data?.items || [], error: null });
    } catch (error) {
      setBackupRecords({ state: "error", data: [], error: error.message });
    }
  }

  function closeBackupRecordsDialog() {
    if (backupProviderState.state?.startsWith("restoring-")) return;
    setBackupRecordsProvider(null);
    setBackupRecords({ state: "idle", data: [], error: null });
  }

  async function refreshBackupRecords() {
    if (!backupRecordsProvider) return;
    await openBackupRecordsDialog(backupRecordsProvider);
  }

  async function downloadBackupRecord(record) {
    if (!backupRecordsProvider) return;
    await runBackupProviderAction({
      pending: `downloading-record-${record.id}`,
      successMessage: `Downloaded ${record.filename}.`,
      action: () => apiDownload(`/api/backup/providers/${backupRecordsProvider.id}/records/${record.id}/download`, record.filename || "aipermission-backup.aipdb"),
    });
  }

  function requestRestoreBackupRecord(record) {
    setRestoreRecordTarget(record);
    setRestoreRecordForm({
      database_name: suggestedRestoreDatabaseName(record),
      database_password: "",
    });
  }

  function closeRestoreBackupRecordDialog() {
    if (backupProviderState.state === `restoring-${restoreRecordTarget?.id}`) return;
    setRestoreRecordTarget(null);
    setRestoreRecordForm({ database_name: "", database_password: "" });
  }

  async function restoreBackupRecord(event) {
    event.preventDefault();
    if (!backupRecordsProvider || !restoreRecordTarget) return;
    const record = restoreRecordTarget;
    const provider = backupRecordsProvider;
    const result = await runBackupProviderAction({
      pending: `restoring-${record.id}`,
      successMessage: `Restored ${record.filename} as ${restoreRecordForm.database_name}.`,
      action: () =>
        apiPost(`/api/backup/providers/${provider.id}/records/${record.id}/restore`, {
          database_name: restoreRecordForm.database_name,
          database_password: restoreRecordForm.database_password,
        }),
    });
    if (result !== undefined) {
      setRestoreRecordTarget(null);
      window.setTimeout(() => window.location.reload(), 800);
    }
  }

  async function startGoogleProviderAuth(provider) {
    setGoogleAuthProvider(provider);
    setGoogleDeviceFlow(null);
    if (!provider.public?.client_id?.trim()) {
      setGoogleAuthState({
        state: "needs_client_id",
        error: null,
        message: "Add a Google OAuth client ID to this provider before connecting.",
      });
      return;
    }
    if (!provider.has_oauth_client_secret) {
      setGoogleAuthState({
        state: "needs_client_secret",
        error: null,
        message: "Add the Google OAuth client secret to this provider before connecting.",
      });
      return;
    }
    setGoogleAuthState({ state: "starting", error: null, message: null });
    try {
      const data = await apiPost(`/api/backup/providers/${provider.id}/google/device/start`, {});
      setGoogleDeviceFlow(data);
      setGoogleAuthState({ state: "idle", error: null, message: null });
    } catch (error) {
      setGoogleAuthState({ state: "error", error: error.message, message: null });
    }
  }

  async function finishGoogleProviderAuth() {
    if (!googleAuthProvider) return;
    setGoogleAuthState({ state: "polling", error: null, message: null });
    try {
      const data = await apiPost(`/api/backup/providers/${googleAuthProvider.id}/google/device/poll`, {});
      if (data?.status === "authorization_pending" || data?.status === "slow_down") {
        setGoogleAuthState({
          state: "idle",
          error: null,
          message: "Google is still waiting for approval. Approve the code, then try Finish connection again.",
        });
        return;
      }
      setGoogleAuthProvider(null);
      setGoogleDeviceFlow(null);
      setGoogleAuthState({ state: "idle", error: null, message: "Google Drive connected." });
      await loadBackupProviders();
    } catch (error) {
      setGoogleAuthState({ state: "error", error: error.message, message: null });
    }
  }

  function closeGoogleAuthDialog() {
    if (googleAuthState.state === "starting" || googleAuthState.state === "polling") return;
    setGoogleAuthProvider(null);
    setGoogleDeviceFlow(null);
    setGoogleAuthState(emptyState);
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
      setMaintenanceSession((current) => ({ ...current, status: "connecting", error: null }));
      setMaintenanceOpen(true);
      window.setTimeout(() => connectMaintenanceConsole({ force: true }), 0);
    } catch (error) {
      setMaintenanceOpenError(error.message);
    }
  }

  async function closeMaintenanceConsole() {
    setMaintenanceOpen(false);
    maintenanceSocketRef.current?.close();
    maintenanceSocketRef.current = null;
    setMaintenanceSession({ transcript: "", status: "closed", error: null, shell: "" });
    try {
      await apiPost("/api/settings/maintenance-console/close", {});
    } catch {
      // The dialog is already local UI state; failing to audit close should not trap the user.
    }
  }

  async function reconnectMaintenanceConsole() {
    setMaintenanceOpenError("");
    try {
      await apiPost("/api/settings/maintenance-console/open", {});
      connectMaintenanceConsole({ force: true });
    } catch (error) {
      setMaintenanceOpenError(error.message);
    }
  }

  function connectMaintenanceConsole(options = {}) {
    const existing = maintenanceSocketRef.current;
    if (existing && (existing.readyState === WebSocket.OPEN || existing.readyState === WebSocket.CONNECTING)) {
      if (!options.force) return;
      existing.close();
    }
    const socket = new WebSocket(maintenanceConsoleAttachUrl());
    maintenanceSocketRef.current = socket;
    setMaintenanceSession((current) => ({ ...current, status: "connecting", error: null }));
    socket.onmessage = (event) => {
      if (maintenanceSocketRef.current !== socket) return;
      const message = JSON.parse(event.data);
      if (message.type === "snapshot") {
        setMaintenanceSession({
          transcript: message.data || "",
          status: message.status || "connected",
          error: null,
          shell: message.shell || "",
        });
      }
      if (message.type === "ready") {
        setMaintenanceSession((current) => ({ ...current, status: message.status || "connected", shell: message.shell || current.shell, error: null }));
      }
      if (message.type === "output") {
        setMaintenanceSession((current) => ({
          ...current,
          transcript: limitMaintenanceTranscript(`${current.transcript || ""}${message.data || ""}`),
          status: message.status || "connected",
          shell: message.shell || current.shell,
          error: null,
        }));
      }
      if (message.type === "error") {
        setMaintenanceSession((current) => ({
          ...current,
          transcript: limitMaintenanceTranscript(`${current.transcript || ""}\r\n${message.data || "Maintenance console error"}\r\n`),
          status: "error",
          error: message.data || "Maintenance console error",
        }));
      }
      if (message.type === "exit") {
        setMaintenanceSession((current) => ({
          ...current,
          status: message.status || "closed",
          error: message.data || "",
        }));
      }
    };
    socket.onerror = () => {
      if (maintenanceSocketRef.current !== socket) return;
      setMaintenanceSession((current) => ({ ...current, status: "error", error: "Maintenance console connection failed." }));
    };
    socket.onclose = () => {
      if (maintenanceSocketRef.current !== socket) return;
      maintenanceSocketRef.current = null;
    };
  }

  function sendMaintenanceInput(data) {
    const socket = maintenanceSocketRef.current;
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "input", data }));
      return;
    }
    connectMaintenanceConsole();
  }

  function resizeMaintenanceConsole(cols, rows) {
    const socket = maintenanceSocketRef.current;
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "resize", cols, rows }));
    }
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
                  <div key={provider.id} className="grid gap-3 rounded-md border border-stone-200 p-3 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,20rem)]">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <Cloud className="h-4 w-4 text-emerald-600" />
                        <p className="truncate text-sm font-semibold text-stone-950">{provider.name}</p>
                        <span className="rounded-full bg-stone-100 px-2 py-0.5 text-[11px] font-semibold uppercase text-stone-600">
                          {providerLabel(provider.provider_type, backupProviderCatalog.data)}
                        </span>
                        <span className={provider.status === "active" ? "rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[11px] font-semibold text-emerald-800 dark-badge-good" : "rounded-full border border-stone-200 bg-stone-100 px-2 py-0.5 text-[11px] font-semibold text-stone-700 dark-badge-neutral"}>
                          {provider.status}
                        </span>
                      </div>
                      <p className="mt-1 truncate text-xs text-stone-500">{provider.public?.folder_name || "AIPermission Backups"}</p>
                      <p className="mt-1 text-xs text-stone-500">
                        {provider.has_oauth_token
                          ? "Google Drive connected"
                          : provider.public?.client_id && provider.has_oauth_client_secret
                            ? "Ready to connect Google Drive"
                            : "Add Google OAuth client ID and client secret to connect"}
                      </p>
                    </div>
                    <div className="grid grid-cols-2 gap-2">
                      {provider.provider_type === "google_drive" && provider.has_oauth_token ? (
                        <Button
                          type="button"
                          variant="outline"
                          className="h-9 px-2 text-xs"
                          onClick={() => requestUploadBackupProvider(provider)}
                          disabled={backupProviderState.state === `uploading-${provider.id}` || backupProviderState.state === "archiving"}
                        >
                          <Upload className="h-4 w-4" />
                          {backupProviderState.state === `uploading-${provider.id}` ? "Uploading..." : "Upload"}
                        </Button>
                      ) : null}
                      {provider.provider_type === "google_drive" && provider.has_oauth_token ? (
                        <Button
                          type="button"
                          variant="outline"
                          className="h-9 px-2 text-xs"
                          onClick={() => openBackupRecordsDialog(provider)}
                          disabled={backupProviderState.state === "archiving"}
                        >
                          <FileDown className="h-4 w-4" />
                          Backups
                        </Button>
                      ) : null}
                      {provider.provider_type === "google_drive" ? (
                        <Button
                          type="button"
                          variant="outline"
                          className="h-9 px-2 text-xs"
                          onClick={() => startGoogleProviderAuth(provider)}
                          disabled={backupProviderState.state === "archiving" || googleAuthState.state === "starting"}
                        >
                          <Cloud className="h-4 w-4" />
                          {provider.has_secret ? "Reconnect" : "Connect"}
                        </Button>
                      ) : null}
                      <Button type="button" variant="outline" className="h-9 px-2 text-xs" onClick={() => openBackupProviderDialog(provider)}>
                        <Edit3 className="h-4 w-4" />
                        Edit
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        className="h-9 px-2 text-xs"
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
          <CardDescription>Open a realtime local terminal inside the AIPermission gateway runtime.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <Notice tone="warn">
            Local UI-only diagnostics for the gateway runtime. It is not exposed to MCP, output is bounded in memory, and open/close lifecycle events are audited.
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
          {backupProviderForm.provider_type === "google_drive" ? (
            <Field>
              Google OAuth client ID
              <Input
                value={backupProviderForm.client_id}
                onChange={(event) => updateBackupProviderField("client_id", event.target.value)}
                placeholder="TVs and limited-input OAuth client ID"
              />
              <span className="text-xs font-normal text-stone-500">
                Create this in Google Cloud Console as an OAuth client for TVs and limited-input devices, with Google Drive API enabled.{" "}
                <a className="font-semibold text-emerald-700 underline-offset-2 hover:underline" href={googleDriveProviderGuideURL} target="_blank" rel="noreferrer">
                  Setup guide
                </a>
              </span>
            </Field>
          ) : null}
          {backupProviderForm.provider_type === "google_drive" ? (
            <Field>
              Google OAuth client secret
              <Input
                type="password"
                value={backupProviderForm.client_secret}
                onChange={(event) => updateBackupProviderField("client_secret", event.target.value)}
                placeholder={backupProviderEditingID ? "Leave blank to keep existing secret" : "OAuth client secret"}
                autoComplete="off"
              />
              <span className="text-xs font-normal text-stone-500">
                Stored encrypted in the local database. This value is never returned by the API after save.{" "}
                <a className="font-semibold text-emerald-700 underline-offset-2 hover:underline" href={googleDriveProviderGuideURL} target="_blank" rel="noreferrer">
                  Setup guide
                </a>
              </span>
            </Field>
          ) : null}
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
        open={Boolean(backupUploadTarget)}
        title="Upload encrypted backup"
        description={backupUploadTarget ? `Upload the current ${databaseName} database to ${backupUploadTarget.name}.` : "Upload encrypted backup."}
        onClose={closeUploadBackupDialog}
        closeDisabled={backupProviderState.state === `uploading-${backupUploadTarget?.id}`}
        closeOnOverlay={false}
        size="md"
      >
        <form className="grid gap-4" onSubmit={uploadBackupProvider}>
          <Notice>
            AIPermission will upload an encrypted <code>.aipdb</code> snapshot. The database password is not sent to Google Drive.
          </Notice>
          <div className="grid gap-2 rounded-md border border-stone-200 bg-stone-50 p-3 text-sm">
            <div className="flex items-center justify-between gap-3">
              <span className="text-stone-500">Database</span>
              <span className="max-w-56 truncate font-semibold text-stone-950">{databaseName}</span>
            </div>
            <div className="flex items-center justify-between gap-3">
              <span className="text-stone-500">Estimated upload size</span>
              <span className="font-semibold text-stone-950">{formatBytes(database.data?.database_size_bytes)}</span>
            </div>
            <div className="flex items-center justify-between gap-3">
              <span className="text-stone-500">Provider</span>
              <span className="max-w-56 truncate font-semibold text-stone-950">{backupUploadTarget?.name || "-"}</span>
            </div>
          </div>
          {backupProviderState.state === "error" ? <Notice tone="bad">{backupProviderState.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={closeUploadBackupDialog} disabled={backupProviderState.state === `uploading-${backupUploadTarget?.id}`}>
              Cancel
            </Button>
            <Button type="submit" disabled={!backupUploadTarget || backupProviderState.state === `uploading-${backupUploadTarget?.id}`}>
              <Upload className="h-4 w-4" />
              {backupProviderState.state === `uploading-${backupUploadTarget?.id}` ? "Uploading..." : "Upload backup"}
            </Button>
          </div>
        </form>
      </Dialog>

      <Dialog
        open={Boolean(backupRecordsProvider)}
        title="Remote backup records"
        description={backupRecordsProvider ? `Backups uploaded through ${backupRecordsProvider.name}.` : "Remote backup records."}
        onClose={closeBackupRecordsDialog}
        closeDisabled={backupProviderState.state?.startsWith("restoring-")}
        closeOnOverlay={false}
        size="wide"
        className="!max-w-4xl"
      >
        <div className="grid gap-4">
          <Notice>
            Download a remote <code>.aipdb</code> file for manual import, or restore it as a new local database. Restores never overwrite the currently open database.
          </Notice>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-sm text-stone-500">
              {backupRecords.state === "ready" ? `${backupRecords.data.length} backup${backupRecords.data.length === 1 ? "" : "s"}` : "Loading backups..."}
            </p>
            <Button type="button" variant="outline" className="h-9 px-3 text-xs" onClick={refreshBackupRecords} disabled={backupRecords.state === "loading"}>
              <RotateCcw className="h-4 w-4" />
              Refresh
            </Button>
          </div>
          {backupRecords.state === "error" ? <Notice tone="bad">{backupRecords.error}</Notice> : null}
          {backupProviderState.message ? <Notice tone="good">{backupProviderState.message}</Notice> : null}
          {backupProviderState.state === "error" ? <Notice tone="bad">{backupProviderState.error}</Notice> : null}
          <div className="max-h-[420px] overflow-auto rounded-md border border-stone-200">
            {backupRecords.state === "loading" ? (
              <div className="p-4 text-sm text-stone-500">Loading remote backup records...</div>
            ) : backupRecords.data.length === 0 ? (
              <div className="p-4 text-sm text-stone-500">No backups uploaded from this database yet.</div>
            ) : (
              <div className="divide-y divide-stone-200">
                {backupRecords.data.map((record) => (
                  <div key={record.id} className="grid gap-3 p-3 md:grid-cols-[minmax(0,1fr)_auto]">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold text-stone-950">{record.filename}</p>
                      <p className="mt-1 text-xs text-stone-500">
                        {formatBytes(record.size_bytes)} · uploaded {formatTimestamp(record.uploaded_at)} · from {record.source_machine || "unknown machine"}
                      </p>
                      <p className="mt-1 truncate font-mono text-[11px] text-stone-400">{record.checksum_sha256 || "no checksum"}</p>
                    </div>
                    <div className="grid grid-cols-2 gap-2 md:w-56">
                      <Button
                        type="button"
                        variant="outline"
                        className="h-9 px-2 text-xs"
                        onClick={() => downloadBackupRecord(record)}
                        disabled={backupProviderState.state === `downloading-record-${record.id}`}
                      >
                        <Download className="h-4 w-4" />
                        {backupProviderState.state === `downloading-record-${record.id}` ? "Saving..." : "Download"}
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        className="h-9 px-2 text-xs"
                        onClick={() => requestRestoreBackupRecord(record)}
                        disabled={backupProviderState.state?.startsWith("restoring-")}
                      >
                        <RotateCcw className="h-4 w-4" />
                        Restore
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </Dialog>

      <Dialog
        open={Boolean(restoreRecordTarget)}
        title="Restore remote backup"
        description={restoreRecordTarget ? `Restore ${restoreRecordTarget.filename} as a new local database.` : "Restore remote backup."}
        onClose={closeRestoreBackupRecordDialog}
        closeDisabled={backupProviderState.state === `restoring-${restoreRecordTarget?.id}`}
        closeOnOverlay={false}
        size="md"
      >
        <form className="grid gap-4" onSubmit={restoreBackupRecord}>
          <Notice tone="warn">
            This creates a new local database and unlocks it after the backup password is verified. The current database is not overwritten.
          </Notice>
          <Field>
            New local database name
            <Input
              value={restoreRecordForm.database_name}
              onChange={(event) => setRestoreRecordForm((current) => ({ ...current, database_name: event.target.value }))}
              required
            />
          </Field>
          <Field>
            Backup database password
            <Input
              type="password"
              value={restoreRecordForm.database_password}
              onChange={(event) => setRestoreRecordForm((current) => ({ ...current, database_password: event.target.value }))}
              autoComplete="current-password"
              required
            />
          </Field>
          {backupProviderState.state === "error" ? <Notice tone="bad">{backupProviderState.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={closeRestoreBackupRecordDialog} disabled={backupProviderState.state === `restoring-${restoreRecordTarget?.id}`}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant="danger"
              disabled={
                !restoreRecordTarget ||
                backupProviderState.state === `restoring-${restoreRecordTarget?.id}` ||
                !restoreRecordForm.database_name.trim() ||
                !restoreRecordForm.database_password
              }
            >
              <RotateCcw className="h-4 w-4" />
              {backupProviderState.state === `restoring-${restoreRecordTarget?.id}` ? "Restoring..." : "Restore"}
            </Button>
          </div>
        </form>
      </Dialog>

      <Dialog
        open={Boolean(googleAuthProvider)}
        title="Connect Google Drive"
        description={googleAuthProvider ? `Authorize "${googleAuthProvider.name}" to store encrypted backups.` : "Authorize Google Drive."}
        onClose={closeGoogleAuthDialog}
        size="md"
        closeDisabled={googleAuthState.state === "starting" || googleAuthState.state === "polling"}
        closeOnOverlay={false}
      >
        <div className="grid gap-4">
          <Notice>
            AIPermission stores only encrypted <code>.aipdb</code> backup files in Google Drive. The OAuth token is encrypted in this local database and is never returned by the API.
          </Notice>
          {googleDeviceFlow ? (
            <div className="grid gap-3">
              <div className="rounded-md border border-stone-200 bg-stone-50 p-3">
                <p className="text-xs font-semibold uppercase text-stone-500">User code</p>
                <div className="mt-2 flex flex-wrap items-center gap-2">
                  <p className="font-mono text-lg font-bold tracking-wide text-stone-950">{googleDeviceFlow.user_code}</p>
                  <CopyButton value={googleDeviceFlow.user_code} variant="outline" className="h-8 px-2 text-xs" iconClassName="h-3.5 w-3.5">
                    Copy
                  </CopyButton>
                </div>
              </div>
              <div className="grid gap-2 sm:grid-cols-2">
                <Button asChild type="button" variant="outline">
                  <a href={googleDeviceFlow.verification_url_complete || googleDeviceFlow.verification_url} target="_blank" rel="noreferrer">
                    Open Google
                  </a>
                </Button>
                <Button type="button" onClick={finishGoogleProviderAuth} disabled={googleAuthState.state === "polling"}>
                  {googleAuthState.state === "polling" ? "Checking..." : "Finish connection"}
                </Button>
              </div>
              <p className="text-xs text-stone-500">
                The code expires in about {Math.max(1, Math.round((googleDeviceFlow.expires_in || 0) / 60))} minutes. If Google still shows pending, wait a few seconds and click Finish connection again.
              </p>
            </div>
          ) : googleAuthState.state === "needs_client_id" || googleAuthState.state === "needs_client_secret" ? (
            <div className="grid gap-3">
              <Notice tone="warn">
                Google Drive backup needs your own Google OAuth client ID and client secret first. Create an OAuth client for TVs and limited-input devices in Google Cloud Console, enable Google Drive API, then save both values on this provider.{" "}
                <a className="font-semibold underline-offset-2 hover:underline" href={googleDriveProviderGuideURL} target="_blank" rel="noreferrer">
                  Open setup guide
                </a>
              </Notice>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  const provider = googleAuthProvider;
                  closeGoogleAuthDialog();
                  if (provider) openBackupProviderDialog(provider);
                }}
              >
                <Edit3 className="h-4 w-4" />
                Edit provider
              </Button>
            </div>
          ) : (
            <Notice>{googleAuthState.state === "starting" ? "Starting Google authorization..." : "Start Google authorization from a provider row."}</Notice>
          )}
          {googleAuthState.message ? <Notice tone="good">{googleAuthState.message}</Notice> : null}
          {googleAuthState.state === "error" ? <Notice tone="bad">{googleAuthState.error}</Notice> : null}
        </div>
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
        description="Interactive local terminal inside the gateway container."
        onClose={closeMaintenanceConsole}
        size="wide"
        className="h-[calc(100vh-100px)] !w-[85vw] !max-w-[1600px] grid-rows-[auto_minmax(0,1fr)]"
        bodyClassName="min-h-0 p-0"
        closeOnOverlay={false}
      >
        <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto]">
          <div className="border-b border-stone-200 p-4">
            <Notice tone="warn" className="py-2 text-xs">
              Local UI-only diagnostics for the gateway runtime. It is not exposed to MCP. Avoid printing secrets in this terminal.
            </Notice>
          </div>
          <div className="min-h-0">
            <PtyConsole
              session={maintenanceSession}
              onInput={sendMaintenanceInput}
              onResize={resizeMaintenanceConsole}
              theme="dark"
            />
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-stone-200 px-4 py-3 text-xs text-stone-500">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="inline-flex items-center gap-1 rounded-full border border-stone-200 px-2 py-1 font-semibold text-stone-700">
                <Terminal className="h-3.5 w-3.5" />
                {maintenanceSession.status || "closed"}
              </span>
              {maintenanceSession.shell ? <span className="truncate font-mono">{maintenanceSession.shell}</span> : null}
              {maintenanceSession.error || maintenanceOpenError ? <span className="truncate text-red-600">{maintenanceSession.error || maintenanceOpenError}</span> : null}
            </div>
            <Button type="button" variant="outline" className="h-8 px-3 text-xs" onClick={reconnectMaintenanceConsole}>
              Reconnect
            </Button>
          </div>
        </div>
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

function maintenanceConsoleAttachUrl() {
  const url = new URL(apiUrl, window.location.origin);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.pathname = "/api/settings/maintenance-console/attach";
  return url.toString();
}

function limitMaintenanceTranscript(value) {
  const maxLength = 200000;
  if (value.length <= maxLength) return value;
  return value.slice(value.length - maxLength);
}

function formatBytes(value) {
  const bytes = Number(value || 0);
  if (!Number.isFinite(bytes) || bytes <= 0) return "Unknown";
  const units = ["B", "KB", "MB", "GB"];
  let size = bytes;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  const precision = size >= 10 || unitIndex === 0 ? 0 : 1;
  return `${size.toFixed(precision)} ${units[unitIndex]}`;
}

function formatTimestamp(value) {
  if (!value) return "unknown time";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function suggestedRestoreDatabaseName(record) {
  const base = String(record?.database_name || record?.database_id || "restored-backup")
    .trim()
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return `${base || "restored-backup"}-restore`;
}
