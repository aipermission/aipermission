import { Archive, Cloud, Download, Edit3, FileDown, Plus, RotateCcw, Upload } from "lucide-react";
import { formatRelativeAge } from "../../lib/date-time";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Notice } from "../ui/notice";
import { backupProviderLabel } from "./use-backup-provider-state";

export function BackupProviderPanel({ state }) {
  const {
    backupState,
    backupProviderState,
    backupProviderCatalog,
    backupProviders,
    downloadDatabase,
    openBackupProviderDialog,
    requestUploadBackupProvider,
    openBackupRecordsDialog,
    testBackupProvider,
    disableBackupProvider,
    requestEnableBackupProvider,
    setBackupProviderArchiveTarget,
  } = state;

  return (
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
                <div
                  key={provider.id}
                  className="grid gap-3 rounded-md border border-stone-200 p-3 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,20rem)]"
                >
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <Cloud className="h-4 w-4 text-emerald-600" />
                      <p className="truncate text-sm font-semibold text-stone-950">{provider.name}</p>
                      <span className="rounded-full bg-stone-100 px-2 py-0.5 text-[11px] font-semibold uppercase text-stone-600">
                        {backupProviderLabel(provider.provider_type, backupProviderCatalog.data)}
                      </span>
                      <span
                        className={
                          provider.status === "active"
                            ? "rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[11px] font-semibold text-emerald-800 dark-badge-good"
                            : "rounded-full border border-stone-200 bg-stone-100 px-2 py-0.5 text-[11px] font-semibold text-stone-700 dark-badge-neutral"
                        }
                      >
                        {provider.status}
                      </span>
                    </div>
                    <p className="mt-1 truncate font-mono text-xs text-stone-500">
                      {provider.public?.base_url || "Service URL not configured"}
                    </p>
                    <p className="mt-1 text-xs text-stone-500">
                      {provider.last_checked_at
                        ? `Last verified ${formatRelativeAge(provider.last_checked_at)}`
                        : "Connection has not been verified yet"}
                    </p>
                  </div>
                  <div className="grid grid-cols-2 gap-2">
                    {provider.status === "active" ? (
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
                    {provider.status === "active" ? (
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
                    <Button
                      type="button"
                      variant="outline"
                      className="h-9 px-2 text-xs"
                      onClick={() => testBackupProvider(provider)}
                      disabled={backupProviderState.state === `testing-${provider.id}` || backupProviderState.state === "archiving"}
                    >
                      <RotateCcw className="h-4 w-4" />
                      {backupProviderState.state === `testing-${provider.id}` ? "Testing..." : "Test"}
                    </Button>
                    {provider.status === "active" ? (
                      <Button
                        type="button"
                        variant="outline"
                        className="h-9 px-2 text-xs"
                        onClick={() => disableBackupProvider(provider)}
                        disabled={backupProviderState.state === `disabling-${provider.id}` || backupProviderState.state === "archiving"}
                      >
                        <Cloud className="h-4 w-4" />
                        {backupProviderState.state === `disabling-${provider.id}` ? "Disabling..." : "Disable"}
                      </Button>
                    ) : (
                      <Button
                        type="button"
                        variant="outline"
                        className="h-9 px-2 text-xs"
                        onClick={() => requestEnableBackupProvider(provider)}
                        disabled={backupProviderState.state === "archiving" || !provider.has_secret}
                        title={provider.has_secret ? "Enable remote backups" : "Add a service token before enabling"}
                      >
                        <Cloud className="h-4 w-4" />
                        Enable
                      </Button>
                    )}
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
  );
}
