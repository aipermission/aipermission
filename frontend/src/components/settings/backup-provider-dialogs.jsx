import { Archive, Cloud, Upload } from "lucide-react";
import { formatBytes } from "../../lib/file-transfer-utils";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Field, Input, Select } from "../ui/form";
import { Notice } from "../ui/notice";
import { backupProviderLabel } from "./use-backup-provider-state";

const backupServiceGuideURL = "https://github.com/aipermission/aipermission/blob/main/docs/providers/aipermission-backup.md";

export function BackupProviderDialogs({ state, database }) {
  return (
    <>
      <ProviderEditorDialog state={state} />
      <ProviderArchiveDialog state={state} />
      <BackupUploadDialog state={state} database={database} />
      <ProviderEnableDialog state={state} />
    </>
  );
}

function ProviderEditorDialog({ state }) {
  const {
    backupProviderDialogOpen,
    backupProviderEditingID,
    backupProviderForm,
    setBackupProviderForm,
    backupProviderCatalog,
    backupProviderState,
    closeBackupProviderDialog,
    saveBackupProvider,
    updateBackupProviderField,
  } = state;
  const saving = backupProviderState.state === "saving";
  return (
    <Dialog
      open={backupProviderDialogOpen}
      title={backupProviderEditingID ? "Edit backup provider" : "Add backup provider"}
      description="Store provider metadata for encrypted database backups."
      onClose={closeBackupProviderDialog}
      size="md"
    >
      <form className="grid gap-4" onSubmit={saveBackupProvider}>
        <Notice>
          Remote providers store encrypted database files only. They do not receive MCP tokens, connector credentials, or the database
          password.
        </Notice>
        <Field>
          Provider type
          <Select
            value={backupProviderForm.provider_type}
            onChange={(event) => {
              const providerType = event.target.value;
              setBackupProviderForm((current) => ({
                ...current,
                provider_type: providerType,
                name: current.name || backupProviderLabel(providerType, backupProviderCatalog.data),
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
          Backup service URL
          <Input
            type="url"
            value={backupProviderForm.base_url}
            onChange={(event) => updateBackupProviderField("base_url", event.target.value)}
            placeholder="https://backups.example.com"
            required
          />
        </Field>
        <Field>
          Service token
          <Input
            type="password"
            value={backupProviderForm.token}
            onChange={(event) => updateBackupProviderField("token", event.target.value)}
            placeholder={backupProviderEditingID ? "Leave blank to keep the existing token" : "At least 32 characters"}
            autoComplete="off"
            required={!backupProviderEditingID}
          />
          <span className="text-xs font-normal text-stone-500">
            Stored encrypted in this local database and never returned by the API.{" "}
            <a
              className="font-semibold text-emerald-700 underline-offset-2 hover:underline"
              href={backupServiceGuideURL}
              target="_blank"
              rel="noreferrer"
            >
              Setup guide
            </a>
          </span>
        </Field>
        <ProviderError state={backupProviderState} />
        <DialogActions
          onCancel={closeBackupProviderDialog}
          pending={saving}
          submitDisabled={saving || !backupProviderForm.name.trim()}
          icon={<Cloud className="h-4 w-4" />}
          label={saving ? "Saving..." : "Save provider"}
        />
      </form>
    </Dialog>
  );
}

function ProviderArchiveDialog({ state }) {
  const { backupProviderArchiveTarget: target, closeBackupProviderArchiveDialog, archiveBackupProvider, backupProviderState } = state;
  const pending = backupProviderState.state === "archiving";
  return (
    <Dialog
      open={Boolean(target)}
      title="Archive backup provider"
      description={target ? `Archive "${target.name}"?` : "Archive backup provider?"}
      onClose={closeBackupProviderArchiveDialog}
      size="md"
    >
      <form className="grid gap-4" onSubmit={archiveBackupProvider}>
        <Notice tone="warn">This removes the provider from Settings. Existing remote backup files are not deleted.</Notice>
        <div className="rounded-md border border-stone-200 bg-stone-50 px-3 py-2">
          <p className="text-xs font-semibold uppercase text-stone-500">Provider</p>
          <p className="mt-1 truncate text-sm font-semibold text-stone-950">{target?.name || "-"}</p>
        </div>
        <ProviderError state={backupProviderState} />
        <DialogActions
          onCancel={closeBackupProviderArchiveDialog}
          pending={pending}
          submitDisabled={!target || pending}
          variant="danger"
          icon={<Archive className="h-4 w-4" />}
          label={pending ? "Archiving..." : "Archive provider"}
        />
      </form>
    </Dialog>
  );
}

function BackupUploadDialog({ state, database }) {
  const { backupUploadTarget: target, databaseName, closeUploadBackupDialog, uploadBackupProvider, backupProviderState } = state;
  const pending = backupProviderState.state === `uploading-${target?.id}`;
  return (
    <Dialog
      open={Boolean(target)}
      title="Upload encrypted backup"
      description={target ? `Upload the current ${databaseName} database to ${target.name}.` : "Upload encrypted backup."}
      onClose={closeUploadBackupDialog}
      closeDisabled={pending}
      closeOnOverlay={false}
      size="md"
    >
      <form className="grid gap-4" onSubmit={uploadBackupProvider}>
        <Notice>
          AIPermission will upload an encrypted <code>.aipdb</code> snapshot. The database password and encryption key are never sent to the
          backup service.
        </Notice>
        <div className="grid gap-2 rounded-md border border-stone-200 bg-stone-50 p-3 text-sm">
          <SummaryRow label="Database" value={databaseName} />
          <SummaryRow label="Estimated upload size" value={formatBytes(database.data?.database_size_bytes)} />
          <SummaryRow label="Provider" value={target?.name || "-"} />
        </div>
        <ProviderError state={backupProviderState} />
        <DialogActions
          onCancel={closeUploadBackupDialog}
          pending={pending}
          submitDisabled={!target || pending}
          icon={<Upload className="h-4 w-4" />}
          label={pending ? "Uploading..." : "Upload backup"}
        />
      </form>
    </Dialog>
  );
}

function ProviderEnableDialog({ state }) {
  const {
    backupEnableTarget: target,
    closeEnableBackupProviderDialog,
    enableBackupProvider,
    backupEnablePassword,
    setBackupEnablePassword,
    backupProviderState,
  } = state;
  const pending = backupProviderState.state === `enabling-${target?.id}`;
  return (
    <Dialog
      open={Boolean(target)}
      title="Enable remote backups"
      description={target ? `Verify this database before enabling ${target.name}.` : "Enable remote backups."}
      onClose={closeEnableBackupProviderDialog}
      size="md"
      closeDisabled={pending}
      closeOnOverlay={false}
      autoFocusClose={false}
    >
      <form className="grid gap-4" onSubmit={enableBackupProvider}>
        <Notice tone="warn">
          Encrypted database bytes will leave this machine. Remote backup requires a strong database password. The password itself is
          verified locally and is never sent to the backup service.
        </Notice>
        <Field>
          Current database password
          <Input
            type="password"
            value={backupEnablePassword}
            onChange={(event) => setBackupEnablePassword(event.target.value)}
            autoComplete="current-password"
            required
          />
          <span className="text-xs font-normal text-stone-500">
            Use at least 18 characters with uppercase, lowercase, and numbers. Common or database-derived passwords are rejected.
          </span>
        </Field>
        <ProviderError state={backupProviderState} />
        <DialogActions
          onCancel={closeEnableBackupProviderDialog}
          pending={pending}
          submitDisabled={!backupEnablePassword || pending}
          icon={<Cloud className="h-4 w-4" />}
          label={pending ? "Enabling..." : "Enable backups"}
        />
      </form>
    </Dialog>
  );
}

function DialogActions({ onCancel, pending, submitDisabled, variant, icon, label }) {
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      <Button type="button" variant="outline" onClick={onCancel} disabled={pending}>
        Cancel
      </Button>
      <Button type="submit" variant={variant} disabled={submitDisabled}>
        {icon}
        {label}
      </Button>
    </div>
  );
}

function ProviderError({ state }) {
  return state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null;
}

function SummaryRow({ label, value }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-stone-500">{label}</span>
      <span className="max-w-56 truncate font-semibold text-stone-950">{value}</span>
    </div>
  );
}
