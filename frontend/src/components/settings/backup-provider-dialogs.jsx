import { Archive, Cloud, Upload } from "lucide-react";
import { formatBytes } from "../../lib/file-transfer-utils";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Field, Input, Select } from "../ui/form";
import { Notice } from "../ui/notice";
import { backupProviderLabel } from "./use-backup-provider-state";

const backupServiceGuideURL = "https://github.com/aipermission/aipermission/blob/main/docs/providers/aipermission-backup.md";

export function BackupProviderDialogs({ state, database }) {
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
    backupProviderArchiveTarget,
    closeBackupProviderArchiveDialog,
    archiveBackupProvider,
    backupUploadTarget,
    databaseName,
    closeUploadBackupDialog,
    uploadBackupProvider,
    backupEnableTarget,
    closeEnableBackupProviderDialog,
    enableBackupProvider,
    backupEnablePassword,
    setBackupEnablePassword,
  } = state;

  return (
    <>
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
            <Button
              type="button"
              variant="outline"
              onClick={closeBackupProviderArchiveDialog}
              disabled={backupProviderState.state === "archiving"}
            >
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
        description={
          backupUploadTarget ? `Upload the current ${databaseName} database to ${backupUploadTarget.name}.` : "Upload encrypted backup."
        }
        onClose={closeUploadBackupDialog}
        closeDisabled={backupProviderState.state === `uploading-${backupUploadTarget?.id}`}
        closeOnOverlay={false}
        size="md"
      >
        <form className="grid gap-4" onSubmit={uploadBackupProvider}>
          <Notice>
            AIPermission will upload an encrypted <code>.aipdb</code> snapshot. The database password and encryption key are never sent to
            the backup service.
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
            <Button
              type="button"
              variant="outline"
              onClick={closeUploadBackupDialog}
              disabled={backupProviderState.state === `uploading-${backupUploadTarget?.id}`}
            >
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
        open={Boolean(backupEnableTarget)}
        title="Enable remote backups"
        description={backupEnableTarget ? `Verify this database before enabling ${backupEnableTarget.name}.` : "Enable remote backups."}
        onClose={closeEnableBackupProviderDialog}
        size="md"
        closeDisabled={backupProviderState.state === `enabling-${backupEnableTarget?.id}`}
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
              autoFocus
              required
            />
            <span className="text-xs font-normal text-stone-500">
              Use at least 18 characters with uppercase, lowercase, and numbers. Common or database-derived passwords are rejected.
            </span>
          </Field>
          {backupProviderState.state === "error" ? <Notice tone="bad">{backupProviderState.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button
              type="button"
              variant="outline"
              onClick={closeEnableBackupProviderDialog}
              disabled={backupProviderState.state === `enabling-${backupEnableTarget?.id}`}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={!backupEnablePassword || backupProviderState.state === `enabling-${backupEnableTarget?.id}`}>
              <Cloud className="h-4 w-4" />
              {backupProviderState.state === `enabling-${backupEnableTarget?.id}` ? "Enabling..." : "Enable backups"}
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
