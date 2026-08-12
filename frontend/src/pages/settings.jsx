import { useEffect, useState } from "react";
import { apiGet } from "../lib/api";
import { BackupProviderDialogs } from "../components/settings/backup-provider-dialogs";
import { BackupProviderPanel } from "../components/settings/backup-provider-panel";
import { BackupRecordDialogs } from "../components/settings/backup-record-dialogs";
import { DatabaseSettingsPanel } from "../components/settings/database-settings-panel";
import { DiagnosticsPanel } from "../components/settings/diagnostics-panel";
import { HistoryLabelsPanel } from "../components/settings/history-labels-panel";
import { HistoryRetentionPanel } from "../components/settings/history-retention-panel";
import { MaintenanceConsolePanel } from "../components/settings/maintenance-console-panel";
import { useBackupProviderState } from "../components/settings/use-backup-provider-state";
import { Notice } from "../components/ui/notice";

export function SettingsPage() {
  const [database, setDatabase] = useState({ state: "loading", data: null, error: null });
  const backupProvider = useBackupProviderState(database);

  useEffect(() => {
    void loadDatabase();
  }, []);

  async function loadDatabase() {
    try {
      const data = await apiGet("/api/unlock/status");
      setDatabase({ state: "ready", data, error: null });
    } catch (error) {
      setDatabase({ state: "error", data: null, error: error.message });
    }
  }

  const databaseName = database.data?.database_name || "Unknown";

  return (
    <section className="mx-auto grid w-full max-w-2xl gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">Settings</h3>
          <p className="text-sm text-stone-500">Manage the current encrypted database backup, password, rename, and delete lifecycle.</p>
        </div>
      </div>
      {database.state === "error" ? <Notice tone="bad">{database.error}</Notice> : null}
      <BackupProviderPanel state={backupProvider} />
      <MaintenanceConsolePanel />
      <DiagnosticsPanel />
      <HistoryRetentionPanel />
      <HistoryLabelsPanel />
      <DatabaseSettingsPanel databaseName={databaseName} />
      <BackupProviderDialogs state={backupProvider} database={database} />
      <BackupRecordDialogs state={backupProvider} />
    </section>
  );
}
