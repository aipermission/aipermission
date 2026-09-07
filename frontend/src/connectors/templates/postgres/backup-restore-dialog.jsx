import { Download, Upload } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Field, Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { usePostgresBackupRestore } from "./use-postgres-backup-restore";

export function BackupRestoreDialog({ value, onClose }) {
  const controller = usePostgresBackupRestore(value);
  return (
    <Dialog
      open={value.open}
      title={value.target ? `${value.target.name} backup / restore` : "Backup / restore database"}
      description="Download a plain SQL dump, or restore an SQL dump into this Postgres target."
      onClose={onClose}
      closeDisabled={controller.isWorking}
      size="xl"
      className="!max-w-3xl"
    >
      <div className="grid gap-5">
        <BackupRestoreTabs active={controller.activeTab} onChange={controller.setActiveTab} />
        {controller.activeTab === "backup" ? <BackupPanel controller={controller} /> : <RestorePanel controller={controller} />}
        <ActionFeedback controller={controller} />
      </div>
    </Dialog>
  );
}

function BackupRestoreTabs({ active, onChange }) {
  return (
    <div
      className="inline-flex w-fit rounded-md border border-stone-200 bg-stone-50 p-1"
      role="tablist"
      aria-label="Database transfer mode"
    >
      <button
        type="button"
        role="tab"
        aria-selected={active === "backup"}
        className={`rounded px-3 py-1.5 text-sm font-medium ${active === "backup" ? "bg-white text-stone-950 shadow-sm" : "text-stone-500 hover:text-stone-900"}`}
        onClick={() => onChange("backup")}
      >
        Backup
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={active === "restore"}
        className={`rounded px-3 py-1.5 text-sm font-medium ${active === "restore" ? "bg-red-600 text-white shadow-sm" : "text-stone-500 hover:text-stone-900"}`}
        onClick={() => onChange("restore")}
      >
        Restore
      </button>
    </div>
  );
}

function BackupPanel({ controller }) {
  return (
    <section className="grid gap-3 rounded-lg border border-stone-200 bg-stone-50 p-3">
      <div>
        <h4 className="text-sm font-semibold text-stone-900">Backup</h4>
        <p className="text-xs text-stone-500">Creates a plain SQL dump with clean statements, no owner, and no privilege grants.</p>
      </div>
      <div className="flex justify-end">
        <Button type="button" onClick={controller.downloadBackup} disabled={!controller.endpoint || controller.isWorking}>
          <Download className="h-4 w-4" />
          {controller.backupState.state === "running" ? "Working" : "Download SQL dump"}
        </Button>
      </div>
    </section>
  );
}

function RestorePanel({ controller }) {
  return (
    <form className="grid gap-3 rounded-lg border border-red-200 bg-red-50 p-3 dark-notice-bad" onSubmit={controller.restoreBackup}>
      <Notice tone="bad">
        Restore executes the selected SQL file against this database profile. Use a trusted dump and verify the target before continuing.
      </Notice>
      <div>
        <h4 className="text-sm font-semibold">Restore</h4>
        <p className="text-xs">
          This may drop and recreate objects if the dump contains clean statements. Type the connector target name exactly before restoring.
        </p>
      </div>
      <Field>
        SQL dump file
        <Input
          type="file"
          accept=".sql,text/plain,application/sql"
          onChange={(event) => controller.setFile(event.target.files?.[0] || null)}
        />
      </Field>
      <Field>
        Type target name to confirm: {controller.targetName}
        <Input
          value={controller.confirmTarget}
          onChange={(event) => controller.setConfirmTarget(event.target.value)}
          placeholder={controller.targetName}
          autoComplete="off"
        />
      </Field>
      <div className="flex justify-end">
        <Button type="submit" variant="danger" disabled={!controller.restoreReady}>
          <Upload className="h-4 w-4" />
          {controller.restoreState.state === "running" ? "Restoring" : "Restore SQL dump"}
        </Button>
      </div>
    </form>
  );
}

function ActionFeedback({ controller }) {
  const state = controller.activeTab === "backup" ? controller.backupState : controller.restoreState;
  return (
    <>
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      {state.message ? <Notice tone="good">{state.message}</Notice> : null}
    </>
  );
}
