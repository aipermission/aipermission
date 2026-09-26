import { useCallback, useEffect, useState } from "react";
import { apiGet } from "../lib/api";
import { errorMessage } from "../lib/errors";
import { useRequestGuard } from "../lib/request-guard";
import { settingsDatabaseResponse, type SettingsDatabase } from "../lib/gateway-contracts/settings-database-contract";
import { BackupProviderDialogs } from "../components/settings/backup-provider-dialogs";
import { BackupProviderPanel } from "../components/settings/backup-provider-panel";
import { BackupRecordDialogs } from "../components/settings/backup-record-dialogs";
import { DatabaseSettingsPanel } from "../components/settings/database-settings-panel";
import { DiagnosticsPanel } from "../components/settings/diagnostics-panel";
import { HistoryLabelsPanel } from "../components/settings/history-labels-panel";
import { HistoryRetentionPanel } from "../components/settings/history-retention-panel";
import { MaintenanceConsolePanel } from "../components/settings/maintenance-console-panel";
import { LocalActionRetryPanel } from "../components/settings/local-action-retry-panel";
import { useBackupProviderState } from "../components/settings/use-backup-provider-state";
import { Notice } from "../components/ui/notice";

export function SettingsPage() {
  const [database, setDatabase] = useState<{ state: "loading" | "ready" | "error"; data: SettingsDatabase | null; error: string | null }>({ state: "loading", data: null, error: null });
  const requestGuard = useRequestGuard("settings-database-status");
  const backupProvider = useBackupProviderState(database);

  const loadDatabase = useCallback(async () => {
    const request = requestGuard.begin("status");
    try {
      const data = settingsDatabaseResponse(await apiGet("/api/unlock/status", { signal: request.signal }));
      if (request.isCurrent()) setDatabase({ state: "ready", data, error: null });
    } catch (error) {
      if (request.isCurrent()) setDatabase({ state: "error", data: null, error: errorMessage(error, "Unable to load database status.") });
    } finally {
      request.complete();
    }
  }, [requestGuard]);

  useEffect(() => {
    void loadDatabase();
  }, [loadDatabase]);

  const databaseName = database.data?.database_name || "Unknown";

  return (
    <section className="mx-auto grid w-full max-w-2xl gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Settings</h1>
          <p className="text-sm text-stone-500">Manage the current encrypted database backup, password, rename, and delete lifecycle.</p>
        </div>
      </div>
      {database.state === "error" ? <Notice tone="bad">{database.error}</Notice> : null}
      <BackupProviderPanel state={backupProvider} />
      <MaintenanceConsolePanel />
      <LocalActionRetryPanel />
      <DiagnosticsPanel />
      <HistoryRetentionPanel />
      <HistoryLabelsPanel />
      <DatabaseSettingsPanel databaseName={databaseName} />
      <BackupProviderDialogs state={backupProvider} database={database} />
      <BackupRecordDialogs state={backupProvider} />
    </section>
  );
}
