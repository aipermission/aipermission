import { useRef, useState } from "react";
import { apiDownload, apiGet, apiPost } from "../../lib/api";
import { backupRecordsActionBusy, parseBackupKeepLatest } from "./backup-state";

export function useBackupRecordState({ backupProviderState, runBackupProviderAction, resetBackupProviderAction }) {
  const [backupRecordsProvider, setBackupRecordsProvider] = useState(null);
  const [backupRecords, setBackupRecords] = useState({ state: "idle", data: [], error: null });
  const [backupPruneTarget, setBackupPruneTarget] = useState(null);
  const [backupPruneKeepLatest, setBackupPruneKeepLatest] = useState("10");
  const [selectedBackupRecordIDs, setSelectedBackupRecordIDs] = useState([]);
  const [backupDeleteRecords, setBackupDeleteRecords] = useState([]);
  const [restoreRecordTarget, setRestoreRecordTarget] = useState(null);
  const [restoreRecordForm, setRestoreRecordForm] = useState({ database_name: "", database_password: "" });
  const backupRecordsRequest = useRef(0);
  const parsedBackupPruneKeepLatest = parseBackupKeepLatest(backupPruneKeepLatest);
  const selectedBackupRecords = backupRecords.data.filter((record) => selectedBackupRecordIDs.includes(record.id));

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
    if (backupRecordsProvider) await openBackupRecordsDialog(backupRecordsProvider);
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
    if (backupProviderState.state !== "deleting-records") setBackupDeleteRecords([]);
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
    if (result === undefined) return;
    setBackupDeleteRecords([]);
    setSelectedBackupRecordIDs([]);
    await openBackupRecordsDialog(provider);
  }

  function requestPruneBackupRecords() {
    if (!backupRecordsProvider) return;
    resetBackupProviderAction();
    setBackupPruneTarget(backupRecordsProvider);
    setBackupPruneKeepLatest("10");
  }

  function closePruneBackupRecordsDialog() {
    if (backupProviderState.state !== "pruning") setBackupPruneTarget(null);
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
    if (result === undefined) return;
    setBackupPruneTarget(null);
    await openBackupRecordsDialog(provider);
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
    setRestoreRecordForm({ database_name: suggestedRestoreDatabaseName(record), database_password: "" });
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
    if (result === undefined) return;
    setRestoreRecordTarget(null);
    setRestoreRecordForm({ database_name: "", database_password: "" });
    window.setTimeout(() => window.location.reload(), 800);
  }

  return {
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
    parsedBackupPruneKeepLatest,
    selectedBackupRecords,
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

export function suggestedRestoreDatabaseName(record) {
  const base = String(record?.database_name || record?.database_id || "restored-backup")
    .trim()
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return `${base || "restored-backup"}-restore`;
}
