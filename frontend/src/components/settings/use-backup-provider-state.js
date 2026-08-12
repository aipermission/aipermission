import { useEffect, useRef, useState } from "react";
import { apiDelete, apiDownload, apiGet, apiPost, apiPut } from "../../lib/api";
import { useAsyncAction } from "../../lib/use-async-action";
import { backupRecordsActionBusy, parseBackupKeepLatest } from "./backup-state";

const emptyState = { state: "idle", error: null, message: null };

export function useBackupProviderState(database) {
  const { actionState: backupState, runAction: runBackupAction } = useAsyncAction(emptyState);
  const {
    actionState: backupProviderState,
    runAction: runBackupProviderAction,
    resetAction: resetBackupProviderAction,
  } = useAsyncAction(emptyState);
  const [backupProviderCatalog, setBackupProviderCatalog] = useState({ state: "loading", data: [], error: null });
  const [backupProviders, setBackupProviders] = useState({ state: "loading", data: [], error: null });
  const [backupProviderDialogOpen, setBackupProviderDialogOpen] = useState(false);
  const [backupProviderArchiveTarget, setBackupProviderArchiveTarget] = useState(null);
  const [backupProviderEditingID, setBackupProviderEditingID] = useState(null);
  const [backupEnableTarget, setBackupEnableTarget] = useState(null);
  const [backupEnablePassword, setBackupEnablePassword] = useState("");
  const [backupUploadTarget, setBackupUploadTarget] = useState(null);
  const [backupRecordsProvider, setBackupRecordsProvider] = useState(null);
  const [backupRecords, setBackupRecords] = useState({ state: "idle", data: [], error: null });
  const [backupPruneTarget, setBackupPruneTarget] = useState(null);
  const [backupPruneKeepLatest, setBackupPruneKeepLatest] = useState("10");
  const [selectedBackupRecordIDs, setSelectedBackupRecordIDs] = useState([]);
  const [backupDeleteRecords, setBackupDeleteRecords] = useState([]);
  const [restoreRecordTarget, setRestoreRecordTarget] = useState(null);
  const [restoreRecordForm, setRestoreRecordForm] = useState({ database_name: "", database_password: "" });
  const backupRecordsRequest = useRef(0);
  const [backupProviderForm, setBackupProviderForm] = useState(emptyBackupProviderForm);

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
    void loadBackupProviderCatalog();
    void loadBackupProviders();
  }, []);

  const databaseName = database.data?.database_name || "Unknown";
  const parsedBackupPruneKeepLatest = parseBackupKeepLatest(backupPruneKeepLatest);
  const selectedBackupRecords = backupRecords.data.filter((record) => selectedBackupRecordIDs.includes(record.id));

  async function downloadDatabase() {
    await runBackupAction({
      pending: "downloading",
      successMessage: "Encrypted database downloaded.",
      action: () => apiDownload("/api/backup/download", `${databaseName}-${new Date().toISOString().slice(0, 19)}.aipdb`),
    });
  }

  function openBackupProviderDialog(provider = null) {
    resetBackupProviderAction();
    if (provider) {
      setBackupProviderEditingID(provider.id);
      setBackupProviderForm({
        provider_type: provider.provider_type,
        name: provider.name,
        base_url: provider.public?.base_url || "",
        token: "",
      });
    } else {
      const firstType = backupProviderCatalog.data[0]?.provider_type || "aipermission_backup";
      setBackupProviderEditingID(null);
      setBackupProviderForm({
        provider_type: firstType,
        name: backupProviderLabel(firstType, backupProviderCatalog.data),
        base_url: "",
        token: "",
      });
    }
    setBackupProviderDialogOpen(true);
  }

  function closeBackupProviderDialog() {
    if (backupProviderState.state === "saving") return;
    setBackupProviderDialogOpen(false);
    setBackupProviderEditingID(null);
    setBackupProviderForm(emptyBackupProviderForm());
  }

  function updateBackupProviderField(field, value) {
    setBackupProviderForm((current) => ({ ...current, [field]: value }));
  }

  async function saveBackupProvider(event) {
    event.preventDefault();
    const payload = {
      provider_type: backupProviderForm.provider_type,
      name: backupProviderForm.name,
      public: {
        base_url: backupProviderForm.base_url.trim(),
      },
    };
    if (backupProviderForm.token.trim()) {
      payload.secret = { token: backupProviderForm.token.trim() };
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
        setBackupProviderEditingID(null);
        setBackupProviderForm(emptyBackupProviderForm());
        await loadBackupProviders();
      },
    });
  }

  async function testBackupProvider(provider) {
    await runBackupProviderAction({
      pending: `testing-${provider.id}`,
      successMessage: `${provider.name} is reachable and protocol-compatible.`,
      action: () => apiPost(`/api/backup/providers/${provider.id}/test`, {}),
    });
    await loadBackupProviders();
  }

  async function disableBackupProvider(provider) {
    await runBackupProviderAction({
      pending: `disabling-${provider.id}`,
      successMessage: `${provider.name} disabled.`,
      action: () => apiPut(`/api/backup/providers/${provider.id}`, { name: provider.name, status: "disabled" }),
    });
    await loadBackupProviders();
  }

  function requestEnableBackupProvider(provider) {
    resetBackupProviderAction();
    setBackupEnableTarget(provider);
    setBackupEnablePassword("");
  }

  function closeEnableBackupProviderDialog() {
    if (backupProviderState.state === `enabling-${backupEnableTarget?.id}`) return;
    setBackupEnableTarget(null);
    setBackupEnablePassword("");
  }

  async function enableBackupProvider(event) {
    event.preventDefault();
    const provider = backupEnableTarget;
    if (!provider) return;
    const result = await runBackupProviderAction({
      pending: `enabling-${provider.id}`,
      successMessage: `${provider.name} enabled.`,
      action: () => apiPost(`/api/backup/providers/${provider.id}/enable`, { current_password: backupEnablePassword }),
    });
    if (result !== undefined) {
      closeEnableBackupProviderDialog();
      await loadBackupProviders();
    }
  }

  function closeBackupProviderArchiveDialog() {
    if (backupProviderState.state === "archiving") return;
    setBackupProviderArchiveTarget(null);
  }

  function requestArchiveBackupProvider(provider) {
    resetBackupProviderAction();
    setBackupProviderArchiveTarget(provider);
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
    resetBackupProviderAction();
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
    resetBackupProviderAction();
    const requestID = backupRecordsRequest.current + 1;
    backupRecordsRequest.current = requestID;
    setBackupRecordsProvider(provider);
    setSelectedBackupRecordIDs([]);
    setBackupRecords({ state: "loading", data: [], error: null });
    try {
      const data = await apiGet(`/api/backup/providers/${provider.id}/records`);
      if (backupRecordsRequest.current !== requestID) return;
      setBackupRecords({ state: "ready", data: data?.items || [], error: null });
    } catch (error) {
      if (backupRecordsRequest.current !== requestID) return;
      setBackupRecords({ state: "error", data: [], error: error.message });
    }
  }

  function closeBackupRecordsDialog() {
    if (backupRecordsActionBusy(backupProviderState.state)) return;
    backupRecordsRequest.current += 1;
    setBackupPruneTarget(null);
    setBackupDeleteRecords([]);
    setSelectedBackupRecordIDs([]);
    setBackupRecordsProvider(null);
    setBackupRecords({ state: "idle", data: [], error: null });
  }

  async function refreshBackupRecords() {
    if (!backupRecordsProvider) return;
    await openBackupRecordsDialog(backupRecordsProvider);
  }

  function toggleBackupRecordSelection(recordID) {
    setSelectedBackupRecordIDs((current) => {
      if (current.includes(recordID)) return current.filter((id) => id !== recordID);
      if (current.length >= Math.max(0, backupRecords.data.length - 1)) return current;
      return [...current, recordID];
    });
  }

  function selectOlderBackupRecords() {
    setSelectedBackupRecordIDs(backupRecords.data.slice(1, 101).map((record) => record.id));
  }

  function requestDeleteBackupRecords(records) {
    if (!records.length || records.length >= backupRecords.data.length) return;
    resetBackupProviderAction();
    setBackupDeleteRecords(records);
  }

  function closeDeleteBackupRecordsDialog() {
    if (backupProviderState.state === "deleting-records") return;
    setBackupDeleteRecords([]);
  }

  async function deleteBackupRecords(event) {
    event.preventDefault();
    if (!backupRecordsProvider || !backupDeleteRecords.length) return;
    const provider = backupRecordsProvider;
    const result = await runBackupProviderAction({
      pending: "deleting-records",
      successMessage: (response) => `Deleted ${response.deleted_count} backup version${response.deleted_count === 1 ? "" : "s"}.`,
      action: () =>
        apiPost(`/api/backup/providers/${provider.id}/records/delete`, {
          record_ids: backupDeleteRecords.map((record) => record.id),
        }),
    });
    if (result !== undefined) {
      setBackupDeleteRecords([]);
      setSelectedBackupRecordIDs([]);
      await openBackupRecordsDialog(provider);
    }
  }

  function requestPruneBackupRecords() {
    if (!backupRecordsProvider) return;
    resetBackupProviderAction();
    setBackupPruneTarget(backupRecordsProvider);
    setBackupPruneKeepLatest("10");
  }

  function closePruneBackupRecordsDialog() {
    if (backupProviderState.state === "pruning") return;
    setBackupPruneTarget(null);
  }

  async function pruneBackupRecords(event) {
    event.preventDefault();
    const provider = backupPruneTarget;
    const keepLatest = parseBackupKeepLatest(backupPruneKeepLatest);
    if (!provider || keepLatest === null) return;
    const result = await runBackupProviderAction({
      pending: "pruning",
      successMessage: (response) =>
        response.deleted_count > 0
          ? `Deleted ${response.deleted_count} old backup version${response.deleted_count === 1 ? "" : "s"}.`
          : `No backups were older than the latest ${response.keep_latest}.`,
      action: () => apiPost(`/api/backup/providers/${provider.id}/prune`, { keep_latest: keepLatest }),
    });
    if (result !== undefined) {
      setBackupPruneTarget(null);
      await openBackupRecordsDialog(provider);
    }
  }

  async function downloadBackupRecord(record) {
    if (!backupRecordsProvider) return;
    await runBackupProviderAction({
      pending: `downloading-record-${record.id}`,
      successMessage: `Downloaded ${record.filename}.`,
      action: () =>
        apiDownload(
          `/api/backup/providers/${backupRecordsProvider.id}/records/${record.id}/download`,
          record.filename || "aipermission-backup.aipdb",
        ),
    });
  }

  function requestRestoreBackupRecord(record) {
    resetBackupProviderAction();
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
      setRestoreRecordForm({ database_name: "", database_password: "" });
      window.setTimeout(() => window.location.reload(), 800);
    }
  }

  return {
    backupState,
    backupProviderState,
    backupProviderCatalog,
    backupProviders,
    backupProviderDialogOpen,
    backupProviderArchiveTarget,
    backupProviderEditingID,
    backupEnableTarget,
    backupEnablePassword,
    setBackupEnablePassword,
    backupUploadTarget,
    backupRecordsProvider,
    backupRecords,
    backupPruneTarget,
    backupPruneKeepLatest,
    setBackupPruneKeepLatest,
    selectedBackupRecordIDs,
    backupDeleteRecords,
    restoreRecordTarget,
    restoreRecordForm,
    setRestoreRecordForm,
    backupProviderForm,
    setBackupProviderForm,
    databaseName,
    parsedBackupPruneKeepLatest,
    selectedBackupRecords,
    downloadDatabase,
    openBackupProviderDialog,
    closeBackupProviderDialog,
    updateBackupProviderField,
    saveBackupProvider,
    testBackupProvider,
    disableBackupProvider,
    requestEnableBackupProvider,
    closeEnableBackupProviderDialog,
    enableBackupProvider,
    requestArchiveBackupProvider,
    closeBackupProviderArchiveDialog,
    archiveBackupProvider,
    requestUploadBackupProvider,
    closeUploadBackupDialog,
    uploadBackupProvider,
    openBackupRecordsDialog,
    closeBackupRecordsDialog,
    refreshBackupRecords,
    toggleBackupRecordSelection,
    selectOlderBackupRecords,
    requestDeleteBackupRecords,
    closeDeleteBackupRecordsDialog,
    deleteBackupRecords,
    requestPruneBackupRecords,
    closePruneBackupRecordsDialog,
    pruneBackupRecords,
    downloadBackupRecord,
    requestRestoreBackupRecord,
    closeRestoreBackupRecordDialog,
    restoreBackupRecord,
  };
}

export function backupProviderLabel(providerType, catalog) {
  return catalog.find((item) => item.provider_type === providerType)?.label || providerType;
}

export function suggestedRestoreDatabaseName(record) {
  const base = String(record?.database_name || record?.database_id || "restored-backup")
    .trim()
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return `${base || "restored-backup"}-restore`;
}

function emptyBackupProviderForm() {
  return {
    provider_type: "aipermission_backup",
    name: "AIPermission Backup",
    base_url: "",
    token: "",
  };
}
