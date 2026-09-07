import { Download, RotateCcw, Trash2 } from "lucide-react";
import { useState } from "react";
import { formatLocalTimestamp, formatRelativeAge } from "../../lib/date-time";
import { formatBytes } from "../../lib/file-transfer-utils";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Checkbox, Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";
import { BackupRetentionPanel } from "./backup-retention-panel";
import { backupRecordsActionBusy } from "./backup-state";

export function BackupRecordDialogs({ state }) {
  const [retentionBusy, setRetentionBusy] = useState(false);
  return (
    <>
      <BackupRecordsBrowserDialog state={state} retentionBusy={retentionBusy} setRetentionBusy={setRetentionBusy} />
      <DeleteBackupRecordsDialog state={state} />
      <PruneBackupRecordsDialog state={state} />
      <RestoreBackupRecordDialog state={state} />
    </>
  );
}

function BackupRecordsBrowserDialog({ state, retentionBusy, setRetentionBusy }) {
  const {
    backupProviderState,
    backupRecordsProvider,
    closeBackupRecordsDialog,
    refreshBackupRecords,
    selectedBackupRecordIDs,
    requestDeleteBackupRecords,
    backupRecords,
    toggleBackupRecordSelection,
    downloadBackupRecord,
    requestRestoreBackupRecord,
  } = state;
  return (
    <Dialog
      open={Boolean(backupRecordsProvider)}
      title="Remote backup records"
      description={backupRecordsProvider ? `Backups uploaded through ${backupRecordsProvider.name}.` : "Remote backup records."}
      onClose={closeBackupRecordsDialog}
      closeDisabled={backupRecordsActionBusy(backupProviderState.state) || retentionBusy}
      closeOnOverlay={false}
      size="wide"
      className="!max-w-4xl"
    >
      <div className="grid gap-4">
        {backupRecordsProvider ? (
          <BackupRetentionPanel provider={backupRecordsProvider} onRecordsChanged={refreshBackupRecords} onBusyChange={setRetentionBusy} />
        ) : null}
        <Notice>
          Download a remote <code>.aipdb</code> file for manual import, or restore it as a new local database. Restores never overwrite the
          currently open database.
        </Notice>
        <BackupRecordsToolbar state={state} retentionBusy={retentionBusy} />
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
                <BackupRecordRow
                  key={record.id}
                  record={record}
                  total={backupRecords.data.length}
                  selectedIDs={selectedBackupRecordIDs}
                  state={backupProviderState.state}
                  retentionBusy={retentionBusy}
                  onToggle={toggleBackupRecordSelection}
                  onDownload={downloadBackupRecord}
                  onRestore={requestRestoreBackupRecord}
                  onDelete={requestDeleteBackupRecords}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </Dialog>
  );
}

function BackupRecordsToolbar({ state, retentionBusy }) {
  const {
    backupProviderState,
    selectedBackupRecordIDs,
    selectedBackupRecords,
    requestDeleteBackupRecords,
    backupRecords,
    selectOlderBackupRecords,
    requestPruneBackupRecords,
    refreshBackupRecords,
  } = state;
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <p className="text-sm text-stone-500">
        {backupRecords.state === "ready"
          ? `${backupRecords.data.length} backup${backupRecords.data.length === 1 ? "" : "s"}`
          : "Loading backups..."}
      </p>
      <div className="flex items-center gap-2">
        {selectedBackupRecordIDs.length > 0 ? (
          <Button
            type="button"
            variant="danger"
            className="h-9 px-3 text-xs"
            onClick={() => requestDeleteBackupRecords(selectedBackupRecords)}
            disabled={backupProviderState.state === "deleting-records" || retentionBusy}
          >
            <Trash2 className="h-4 w-4" /> Delete selected ({selectedBackupRecordIDs.length})
          </Button>
        ) : backupRecords.data.length > 1 ? (
          <Button type="button" variant="outline" className="h-9 px-3 text-xs" onClick={selectOlderBackupRecords} disabled={retentionBusy}>
            Select older
          </Button>
        ) : null}
        <Button
          type="button"
          variant="outline"
          className="h-9 px-3 text-xs"
          onClick={requestPruneBackupRecords}
          disabled={retentionBusy || backupRecords.state !== "ready" || backupRecords.data.length === 0}
        >
          <Trash2 className="h-4 w-4" /> Prune
        </Button>
        <Button
          type="button"
          variant="outline"
          className="h-9 px-3 text-xs"
          onClick={refreshBackupRecords}
          disabled={retentionBusy || backupRecords.state === "loading"}
        >
          <RotateCcw className="h-4 w-4" /> Refresh
        </Button>
      </div>
    </div>
  );
}

function BackupRecordRow({ record, total, selectedIDs, state, retentionBusy, onToggle, onDownload, onRestore, onDelete }) {
  const selected = selectedIDs.includes(record.id);
  const lastRecord = total <= 1;
  return (
    <div className="grid gap-3 p-3 md:grid-cols-[minmax(0,1fr)_auto]">
      <div className="flex min-w-0 items-start gap-3">
        <Checkbox
          checked={selected}
          onChange={() => onToggle(record.id)}
          disabled={lastRecord || (!selected && selectedIDs.length >= total - 1)}
          aria-label={`Select ${record.filename}`}
          className="mt-1 shrink-0"
        />
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-stone-950">{record.filename}</p>
          <p className="mt-1 text-xs text-stone-500">
            {formatBytes(record.size_bytes)} · {formatRelativeAge(record.backup_created_at || record.uploaded_at)} · from{" "}
            {record.source_machine || "unknown machine"}
          </p>
          <p className="mt-1 text-[11px] text-stone-400">
            {formatLocalTimestamp(record.backup_created_at || record.uploaded_at) ||
              record.backup_created_at ||
              record.uploaded_at ||
              "unknown time"}
          </p>
          <p className="mt-1 truncate font-mono text-[11px] text-stone-400">{record.checksum_sha256 || "no checksum"}</p>
        </div>
      </div>
      <div className="grid grid-cols-[1fr_1fr_auto] gap-2 md:w-72">
        <Button
          type="button"
          variant="outline"
          className="h-9 px-3 text-xs"
          onClick={() => onDownload(record)}
          disabled={retentionBusy || state === `downloading-record-${record.id}`}
        >
          <Download className="h-4 w-4 shrink-0" />
          {state === `downloading-record-${record.id}` ? "Saving..." : "Download"}
        </Button>
        <Button
          type="button"
          variant="outline"
          className="h-9 px-3 text-xs"
          onClick={() => onRestore(record)}
          disabled={retentionBusy || state?.startsWith("restoring-")}
        >
          <RotateCcw className="h-4 w-4 shrink-0" />
          Restore
        </Button>
        <Button
          type="button"
          variant="danger"
          className="h-9 w-9 px-0"
          onClick={() => onDelete([record])}
          disabled={retentionBusy || lastRecord || state === "deleting-records"}
          title={lastRecord ? "The last recovery version must remain" : "Delete backup version"}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}

function DeleteBackupRecordsDialog({ state }) {
  const { backupProviderState, backupDeleteRecords, closeDeleteBackupRecordsDialog, deleteBackupRecords } = state;
  const pending = backupProviderState.state === "deleting-records";
  return (
    <Dialog
      open={backupDeleteRecords.length > 0}
      title={backupDeleteRecords.length === 1 ? "Delete backup version" : "Delete selected backup versions"}
      description={
        backupDeleteRecords.length === 1
          ? "Permanently remove this encrypted remote version."
          : `Permanently remove ${backupDeleteRecords.length} encrypted remote versions.`
      }
      onClose={closeDeleteBackupRecordsDialog}
      closeDisabled={pending}
      closeOnOverlay={false}
      size="md"
    >
      <form className="grid gap-4" onSubmit={deleteBackupRecords}>
        <Notice tone="warn">
          This cannot be undone. AIPermission Backup will remove the selected immutable files and metadata. At least one recovery version
          always remains.
        </Notice>
        <div className="max-h-48 overflow-auto rounded-md border border-stone-200 bg-stone-50">
          <div className="divide-y divide-stone-200">
            {backupDeleteRecords.map((record) => (
              <div key={record.id} className="px-3 py-2">
                <p className="truncate text-sm font-semibold text-stone-950">{record.filename}</p>
                <p className="mt-0.5 text-xs text-stone-500">
                  {formatRelativeAge(record.backup_created_at || record.uploaded_at)} · {record.source_machine || "unknown machine"}
                </p>
              </div>
            ))}
          </div>
        </div>
        <StateError state={backupProviderState} />
        <ConfirmActions
          pending={pending}
          onCancel={closeDeleteBackupRecordsDialog}
          disabled={pending || backupDeleteRecords.length === 0}
          label={pending ? "Deleting..." : "Delete permanently"}
        />
      </form>
    </Dialog>
  );
}

function PruneBackupRecordsDialog({ state }) {
  const {
    backupProviderState,
    backupPruneTarget,
    closePruneBackupRecordsDialog,
    pruneBackupRecords,
    backupPruneKeepLatest,
    setBackupPruneKeepLatest,
    parsedBackupPruneKeepLatest,
    backupRecords,
  } = state;
  const pending = backupProviderState.state === "pruning";
  const deletedCount = Math.max(0, backupRecords.data.length - (parsedBackupPruneKeepLatest || 0));
  return (
    <Dialog
      open={Boolean(backupPruneTarget)}
      title="Prune old backup versions"
      description={
        backupPruneTarget ? `Keep only the newest versions uploaded through ${backupPruneTarget.name}.` : "Prune old backup versions."
      }
      onClose={closePruneBackupRecordsDialog}
      closeDisabled={pending}
      closeOnOverlay={false}
      size="md"
    >
      <form className="grid gap-4" onSubmit={pruneBackupRecords}>
        <Notice tone="warn">
          Older remote versions will be permanently deleted from the self-hosted backup service. The newest versions are never removed by
          this action.
        </Notice>
        <Field>
          Versions to keep
          <Input
            type="number"
            min="1"
            max="1000"
            step="1"
            value={backupPruneKeepLatest}
            onChange={(event) => setBackupPruneKeepLatest(event.target.value)}
            required
          />
        </Field>
        <p className="text-xs text-stone-500">
          {backupRecords.data.length > 0
            ? `${deletedCount} of the ${backupRecords.data.length} currently listed backups would be deleted.`
            : "No remote versions are currently listed."}
        </p>
        <StateError state={backupProviderState} />
        <ConfirmActions
          pending={pending}
          onCancel={closePruneBackupRecordsDialog}
          disabled={pending || parsedBackupPruneKeepLatest === null}
          label={pending ? "Pruning..." : "Prune old versions"}
        />
      </form>
    </Dialog>
  );
}

function RestoreBackupRecordDialog({ state }) {
  const {
    backupProviderState,
    restoreRecordTarget,
    closeRestoreBackupRecordDialog,
    restoreBackupRecord,
    restoreRecordForm,
    setRestoreRecordForm,
  } = state;
  const pending = backupProviderState.state === `restoring-${restoreRecordTarget?.id}`;
  return (
    <Dialog
      open={Boolean(restoreRecordTarget)}
      title="Restore remote backup"
      description={restoreRecordTarget ? `Restore ${restoreRecordTarget.filename} as a new local database.` : "Restore remote backup."}
      onClose={closeRestoreBackupRecordDialog}
      closeDisabled={pending}
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
        <StateError state={backupProviderState} />
        <div className="grid gap-2 sm:grid-cols-2">
          <Button type="button" variant="outline" onClick={closeRestoreBackupRecordDialog} disabled={pending}>
            Cancel
          </Button>
          <Button
            type="submit"
            variant="danger"
            disabled={!restoreRecordTarget || pending || !restoreRecordForm.database_name.trim() || !restoreRecordForm.database_password}
          >
            <RotateCcw className="h-4 w-4" />
            {pending ? "Restoring..." : "Restore"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function ConfirmActions({ pending, onCancel, disabled, label }) {
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      <Button type="button" variant="outline" onClick={onCancel} disabled={pending}>
        Cancel
      </Button>
      <Button type="submit" variant="danger" disabled={disabled}>
        <Trash2 className="h-4 w-4" />
        {label}
      </Button>
    </div>
  );
}

function StateError({ state }) {
  return state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null;
}
