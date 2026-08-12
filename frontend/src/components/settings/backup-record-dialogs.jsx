import { Download, RotateCcw, Trash2 } from "lucide-react";
import { useState } from "react";
import { formatLocalTimestamp, formatRelativeAge } from "../../lib/date-time";
import { formatBytes } from "../../lib/file-transfer-utils";
import { BackupRetentionPanel } from "./backup-retention-panel";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Checkbox, Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";
import { backupRecordsActionBusy } from "./backup-state";

export function BackupRecordDialogs({ state }) {
  const [retentionBusy, setRetentionBusy] = useState(false);
  const {
    backupProviderState,
    backupRecordsProvider,
    closeBackupRecordsDialog,
    refreshBackupRecords,
    selectedBackupRecordIDs,
    selectedBackupRecords,
    requestDeleteBackupRecords,
    backupRecords,
    selectOlderBackupRecords,
    requestPruneBackupRecords,
    toggleBackupRecordSelection,
    downloadBackupRecord,
    requestRestoreBackupRecord,
    backupDeleteRecords,
    closeDeleteBackupRecordsDialog,
    deleteBackupRecords,
    backupPruneTarget,
    closePruneBackupRecordsDialog,
    pruneBackupRecords,
    backupPruneKeepLatest,
    setBackupPruneKeepLatest,
    parsedBackupPruneKeepLatest,
    restoreRecordTarget,
    closeRestoreBackupRecordDialog,
    restoreBackupRecord,
    restoreRecordForm,
    setRestoreRecordForm,
  } = state;

  return (
    <>
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
            <BackupRetentionPanel
              provider={backupRecordsProvider}
              onRecordsChanged={refreshBackupRecords}
              onBusyChange={setRetentionBusy}
            />
          ) : null}
          <Notice>
            Download a remote <code>.aipdb</code> file for manual import, or restore it as a new local database. Restores never overwrite
            the currently open database.
          </Notice>
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
                  <Trash2 className="h-4 w-4" />
                  Delete selected ({selectedBackupRecordIDs.length})
                </Button>
              ) : backupRecords.data.length > 1 ? (
                <Button
                  type="button"
                  variant="outline"
                  className="h-9 px-3 text-xs"
                  onClick={selectOlderBackupRecords}
                  disabled={retentionBusy}
                >
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
                <Trash2 className="h-4 w-4" />
                Prune
              </Button>
              <Button
                type="button"
                variant="outline"
                className="h-9 px-3 text-xs"
                onClick={refreshBackupRecords}
                disabled={retentionBusy || backupRecords.state === "loading"}
              >
                <RotateCcw className="h-4 w-4" />
                Refresh
              </Button>
            </div>
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
                    <div className="flex min-w-0 items-start gap-3">
                      <Checkbox
                        checked={selectedBackupRecordIDs.includes(record.id)}
                        onChange={() => toggleBackupRecordSelection(record.id)}
                        disabled={
                          backupRecords.data.length <= 1 ||
                          (!selectedBackupRecordIDs.includes(record.id) && selectedBackupRecordIDs.length >= backupRecords.data.length - 1)
                        }
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
                        onClick={() => downloadBackupRecord(record)}
                        disabled={retentionBusy || backupProviderState.state === `downloading-record-${record.id}`}
                      >
                        <Download className="h-4 w-4 shrink-0" />
                        {backupProviderState.state === `downloading-record-${record.id}` ? "Saving..." : "Download"}
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        className="h-9 px-3 text-xs"
                        onClick={() => requestRestoreBackupRecord(record)}
                        disabled={retentionBusy || backupProviderState.state?.startsWith("restoring-")}
                      >
                        <RotateCcw className="h-4 w-4 shrink-0" />
                        Restore
                      </Button>
                      <Button
                        type="button"
                        variant="danger"
                        className="h-9 w-9 px-0"
                        onClick={() => requestDeleteBackupRecords([record])}
                        disabled={retentionBusy || backupRecords.data.length <= 1 || backupProviderState.state === "deleting-records"}
                        title={backupRecords.data.length <= 1 ? "The last recovery version must remain" : "Delete backup version"}
                      >
                        <Trash2 className="h-4 w-4" />
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
        open={backupDeleteRecords.length > 0}
        title={backupDeleteRecords.length === 1 ? "Delete backup version" : "Delete selected backup versions"}
        description={
          backupDeleteRecords.length === 1
            ? "Permanently remove this encrypted remote version."
            : `Permanently remove ${backupDeleteRecords.length} encrypted remote versions.`
        }
        onClose={closeDeleteBackupRecordsDialog}
        closeDisabled={backupProviderState.state === "deleting-records"}
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
          {backupProviderState.state === "error" ? <Notice tone="bad">{backupProviderState.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button
              type="button"
              variant="outline"
              onClick={closeDeleteBackupRecordsDialog}
              disabled={backupProviderState.state === "deleting-records"}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="danger"
              disabled={backupProviderState.state === "deleting-records" || backupDeleteRecords.length === 0}
            >
              <Trash2 className="h-4 w-4" />
              {backupProviderState.state === "deleting-records" ? "Deleting..." : "Delete permanently"}
            </Button>
          </div>
        </form>
      </Dialog>

      <Dialog
        open={Boolean(backupPruneTarget)}
        title="Prune old backup versions"
        description={
          backupPruneTarget ? `Keep only the newest versions uploaded through ${backupPruneTarget.name}.` : "Prune old backup versions."
        }
        onClose={closePruneBackupRecordsDialog}
        closeDisabled={backupProviderState.state === "pruning"}
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
              ? `${Math.max(0, backupRecords.data.length - (parsedBackupPruneKeepLatest || 0))} of the ${backupRecords.data.length} currently listed backups would be deleted.`
              : "No remote versions are currently listed."}
          </p>
          {backupProviderState.state === "error" ? <Notice tone="bad">{backupProviderState.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button
              type="button"
              variant="outline"
              onClick={closePruneBackupRecordsDialog}
              disabled={backupProviderState.state === "pruning"}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="danger"
              disabled={backupProviderState.state === "pruning" || parsedBackupPruneKeepLatest === null}
            >
              <Trash2 className="h-4 w-4" />
              {backupProviderState.state === "pruning" ? "Pruning..." : "Prune old versions"}
            </Button>
          </div>
        </form>
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
            <Button
              type="button"
              variant="outline"
              onClick={closeRestoreBackupRecordDialog}
              disabled={backupProviderState.state === `restoring-${restoreRecordTarget?.id}`}
            >
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
    </>
  );
}
