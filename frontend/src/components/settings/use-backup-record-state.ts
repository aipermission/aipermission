import { backupCountResponse } from "./backup-contracts";
import { useRef, useState, type FormEvent } from "react";
import { apiDownload, apiGet, apiPost } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import type { AsyncActionState } from "../../lib/use-async-action";
import { backupRecordsActionBusy, parseBackupKeepLatest } from "./backup-state";
import { backupItems, isBackupRecord, type BackupProvider, type BackupRecord, type LoadState } from "./backup-contracts";

type RunAction = <T>(_options: {
  pending?: string;
  successMessage?: string | null | ((_result: T) => string | null | undefined);
  action: () => T | Promise<T>;
}) => Promise<T | undefined>;

type BackupRecordOptions = {
  backupProviderState: AsyncActionState;
  runBackupProviderAction: RunAction;
  resetBackupProviderAction: () => void;
};

type CountResult = { deleted_count: number; keep_latest: number };

export function useBackupRecordState({ backupProviderState, runBackupProviderAction, resetBackupProviderAction }: BackupRecordOptions) {
  const [backupRecordsProvider, setBackupRecordsProvider] = useState<BackupProvider | null>(null);
  const [backupRecords, setBackupRecords] = useState<LoadState<BackupRecord>>({ state: "idle", data: [], error: null });
  const [backupPruneTarget, setBackupPruneTarget] = useState<BackupProvider | null>(null);
  const [backupPruneKeepLatest, setBackupPruneKeepLatest] = useState("10");
  const [selectedBackupRecordIDs, setSelectedBackupRecordIDs] = useState<number[]>([]);
  const [backupDeleteRecords, setBackupDeleteRecords] = useState<BackupRecord[]>([]);
  const [restoreRecordTarget, setRestoreRecordTarget] = useState<BackupRecord | null>(null);
  const [restoreRecordForm, setRestoreRecordForm] = useState({ database_name: "", database_password: "" });
  const backupRecordsRequest = useRef(0);
  const parsedBackupPruneKeepLatest = parseBackupKeepLatest(backupPruneKeepLatest);
  const selectedBackupRecords = backupRecords.data.filter((record) => selectedBackupRecordIDs.includes(record.id));

  async function openBackupRecordsDialog(provider: BackupProvider) {
    resetBackupProviderAction();
    const requestID = backupRecordsRequest.current + 1;
    backupRecordsRequest.current = requestID;
    setBackupRecordsProvider(provider);
    setSelectedBackupRecordIDs([]);
    setBackupRecords({ state: "loading", data: [], error: null });
    try {
      const data = await apiGet(`/api/backup/providers/${provider.id}/records`);
      if (backupRecordsRequest.current !== requestID) return;
      setBackupRecords({ state: "ready", data: backupItems(data, isBackupRecord), error: null });
    } catch (error) {
      if (backupRecordsRequest.current !== requestID) return;
      setBackupRecords({ state: "error", data: [], error: errorMessage(error, "Unable to load backup records.") });
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

  function toggleBackupRecordSelection(recordID: number) {
    setSelectedBackupRecordIDs((current) => {
      if (current.includes(recordID)) return current.filter((id) => id !== recordID);
      if (current.length >= Math.max(0, backupRecords.data.length - 1)) return current;
      return [...current, recordID];
    });
  }

  function selectOlderBackupRecords() {
    setSelectedBackupRecordIDs(backupRecords.data.slice(1, 101).map((record) => record.id));
  }

  function requestDeleteBackupRecords(records: BackupRecord[]) {
    if (!records.length || records.length >= backupRecords.data.length) return;
    resetBackupProviderAction();
    setBackupDeleteRecords(records);
  }

  function closeDeleteBackupRecordsDialog() {
    if (backupProviderState.state !== "deleting-records") setBackupDeleteRecords([]);
  }

  async function deleteBackupRecords(event: FormEvent) {
    event.preventDefault();
    if (!backupRecordsProvider || !backupDeleteRecords.length) return;
    const provider = backupRecordsProvider;
    const result = await runBackupProviderAction<CountResult>({
      pending: "deleting-records",
      successMessage: (response) => `Deleted ${response.deleted_count} backup version${response.deleted_count === 1 ? "" : "s"}.`,
      action: async () =>
        backupCountResponse(
          await apiPost(`/api/backup/providers/${provider.id}/records/delete`, {
            record_ids: backupDeleteRecords.map((record) => record.id),
          }),
        ),
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

  async function pruneBackupRecords(event: FormEvent) {
    event.preventDefault();
    const provider = backupPruneTarget;
    const keepLatest = parseBackupKeepLatest(backupPruneKeepLatest);
    if (!provider || keepLatest === null) return;
    const result = await runBackupProviderAction<CountResult>({
      pending: "pruning",
      successMessage: (response) =>
        response.deleted_count > 0
          ? `Deleted ${response.deleted_count} old backup version${response.deleted_count === 1 ? "" : "s"}.`
          : `No backups were older than the latest ${response.keep_latest}.`,
      action: async () => backupCountResponse(await apiPost(`/api/backup/providers/${provider.id}/prune`, { keep_latest: keepLatest })),
    });
    if (result === undefined) return;
    setBackupPruneTarget(null);
    await openBackupRecordsDialog(provider);
  }

  async function downloadBackupRecord(record: BackupRecord) {
    if (!backupRecordsProvider) return;
    await runBackupProviderAction({
      pending: `downloading-record-${record.id}`,
      successMessage: (result) => (result?.canceled ? null : `Downloaded ${record.filename}.`),
      action: () =>
        apiDownload(
          `/api/backup/providers/${backupRecordsProvider.id}/records/${record.id}/download`,
          record.filename || "aipermission-backup.aipdb",
          { picker: true, requireStreaming: true },
        ),
    });
  }

  function requestRestoreBackupRecord(record: BackupRecord) {
    resetBackupProviderAction();
    setRestoreRecordTarget(record);
    setRestoreRecordForm({ database_name: suggestedRestoreDatabaseName(record), database_password: "" });
  }

  function closeRestoreBackupRecordDialog() {
    if (backupProviderState.state === `restoring-${restoreRecordTarget?.id}`) return;
    setRestoreRecordTarget(null);
    setRestoreRecordForm({ database_name: "", database_password: "" });
  }

  async function restoreBackupRecord(event: FormEvent) {
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

export function suggestedRestoreDatabaseName(record: BackupRecord | null | undefined) {
  const base = String(record?.database_name || record?.database_id || "restored-backup")
    .trim()
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return `${base || "restored-backup"}-restore`;
}
