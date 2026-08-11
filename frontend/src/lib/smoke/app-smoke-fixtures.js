import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

export const currentDir = join(dirname(fileURLToPath(import.meta.url)), "..");
export const connectorTemplatesDir = join(currentDir, "..", "connectors", "templates");
export const indexSource = readFileSync(join(currentDir, "..", "..", "index.html"), "utf8");
export const themeInitSource = readFileSync(join(currentDir, "..", "..", "public", "theme-init.js"), "utf8");
export const appSource = readFileSync(join(currentDir, "..", "App.jsx"), "utf8");
export const apiSource = readFileSync(join(currentDir, "api.js"), "utf8");
export const nginxSource = readFileSync(join(currentDir, "..", "..", "nginx.conf"), "utf8");
export const sidebarSource = readFileSync(join(currentDir, "..", "components", "app-sidebar.jsx"), "utf8");
export const unlockSource = readFileSync(join(currentDir, "..", "pages", "unlock.jsx"), "utf8");
export const releaseSource = readFileSync(join(currentDir, "release.js"), "utf8");
export const releaseManifest = JSON.parse(readFileSync(join(currentDir, "..", "..", "..", "release-manifest.json"), "utf8"));
export const connectorApprovalDialogSource = readFileSync(
  join(currentDir, "..", "components", "console", "connector-action-approval-dialog.jsx"),
  "utf8",
);
export const connectorActivityDialogSource = readFileSync(
  join(currentDir, "..", "components", "console", "connector-activity-dialog.jsx"),
  "utf8",
);
export const settingsSource = [
  "settings.jsx",
  "database-settings-panel.jsx",
  "password-settings-dialog.jsx",
  "history-retention-panel.jsx",
  "history-labels-panel.jsx",
  "maintenance-console-panel.jsx",
  "backup-provider-panel.jsx",
  "backup-provider-dialogs.jsx",
  "backup-record-dialogs.jsx",
  "use-backup-provider-state.js",
]
  .map((filename, index) =>
    readFileSync(
      index === 0 ? join(currentDir, "..", "pages", filename) : join(currentDir, "..", "components", "settings", filename),
      "utf8",
    ),
  )
  .join("\n");
export const backupRetentionPanelSource = readFileSync(
  join(currentDir, "..", "components", "settings", "backup-retention-panel.jsx"),
  "utf8",
);
export const shellSource = [
  readFileSync(join(currentDir, "..", "components", "app-shell.jsx"), "utf8"),
  readFileSync(join(currentDir, "..", "components", "app-shell-runtime.js"), "utf8"),
  readFileSync(join(currentDir, "..", "components", "backup-freshness-notices.jsx"), "utf8"),
].join("\n");
export const historySource = readFileSync(join(currentDir, "..", "pages", "history.jsx"), "utf8");
export const auditLogsSource = readFileSync(join(currentDir, "..", "pages", "audit-logs.jsx"), "utf8");
export const consolePageSource = [
  readFileSync(join(currentDir, "..", "pages", "console.jsx"), "utf8"),
  readFileSync(join(currentDir, "..", "components", "console", "console-target-sidebar.jsx"), "utf8"),
  readFileSync(join(currentDir, "..", "components", "console", "console-recovery-panel.jsx"), "utf8"),
].join("\n");
export const connectorsSource = [
  readFileSync(join(currentDir, "..", "pages", "connectors.jsx"), "utf8"),
  readFileSync(join(currentDir, "..", "connectors", "editor", "use-connector-editor.js"), "utf8"),
  readFileSync(join(currentDir, "..", "connectors", "editor", "use-connector-connection-tests.js"), "utf8"),
].join("\n");
export const projectsSource = readFileSync(join(currentDir, "..", "pages", "projects.jsx"), "utf8");
export const tokensSource = readFileSync(join(currentDir, "..", "pages", "tokens.jsx"), "utf8");
export const credentialsSource = [
  readFileSync(join(currentDir, "..", "pages", "credentials.jsx"), "utf8"),
  readFileSync(join(currentDir, "..", "connectors", "editor", "use-credential-profile-editor.js"), "utf8"),
].join("\n");
export const fileTransferDialogSource = readFileSync(
  join(currentDir, "..", "components", "file-transfer", "file-transfer-dialog.jsx"),
  "utf8",
);
export const fileTransferBrowserSource = readFileSync(
  join(currentDir, "..", "components", "file-transfer", "file-transfer-browser-dialog.jsx"),
  "utf8",
);
export const fileTransferConfirmSource = readFileSync(
  join(currentDir, "..", "components", "file-transfer", "file-transfer-confirm-dialogs.jsx"),
  "utf8",
);
export const bulkCommandDialogSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "ssh", "bulk-command-dialog.jsx"),
  "utf8",
);
export const transferCenterSource = readFileSync(join(currentDir, "..", "components", "transfer-center.jsx"), "utf8");
export const tokenPermissionPanelSource = readFileSync(
  join(currentDir, "..", "components", "console", "token-permission-panel.jsx"),
  "utf8",
);
export const connectorTokenPermissionPanelSource = readFileSync(
  join(currentDir, "..", "components", "console", "connector-token-permission-panel.jsx"),
  "utf8",
);
export const connectorPermissionDialogSource = readFileSync(
  join(currentDir, "..", "components", "tokens", "connector-permission-dialog.jsx"),
  "utf8",
);
export const vaultPermissionDialogSource = readFileSync(
  join(currentDir, "..", "components", "tokens", "vault-permission-dialog.jsx"),
  "utf8",
);
export const connectorTemplateCommonSource = readFileSync(join(currentDir, "..", "connectors", "templates", "common.jsx"), "utf8");
export const kafkaConsoleSource = readFileSync(join(currentDir, "..", "connectors", "templates", "kafka", "console.jsx"), "utf8");
export const mailConsoleSource = readFileSync(join(currentDir, "..", "connectors", "templates", "mail", "console.jsx"), "utf8");
export const kafkaWriteDialogsSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "kafka", "write-dialogs.jsx"),
  "utf8",
);
export const connectorTargetProfileSaveSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "target-profile-save.js"),
  "utf8",
);
export const connectorTemplateRegistrySource = readFileSync(join(currentDir, "..", "connectors", "templates", "registry.jsx"), "utf8");
export const connectorTemplateCatalogSource = readFileSync(join(currentDir, "..", "connectors", "templates", "catalog.js"), "utf8");
export const connectorHostPingSource = readFileSync(join(currentDir, "..", "connectors", "templates", "host-ping-button.jsx"), "utf8");
export const s3PresignDialogSource = readFileSync(join(currentDir, "..", "connectors", "templates", "s3", "presign-dialog.jsx"), "utf8");
export const s3VersionsDialogSource = readFileSync(join(currentDir, "..", "connectors", "templates", "s3", "versions-dialog.jsx"), "utf8");
export const s3LifecycleDialogSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "s3", "lifecycle-dialog.jsx"),
  "utf8",
);
export const backendConnectorRegistrySource = readFileSync(
  join(currentDir, "..", "..", "..", "backend", "internal", "connectors", "builtin", "registry.go"),
  "utf8",
);
export const connectorTemplateKinds = readdirSync(connectorTemplatesDir, { withFileTypes: true })
  .filter((entry) => entry.isDirectory() && !entry.name.startsWith("_"))
  .map((entry) => entry.name)
  .sort();
export const sshConnectorFormTemplateSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "form.jsx"), "utf8");
export const sshCredentialFormTemplateSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "ssh", "credential-form.jsx"),
  "utf8",
);
export const sshCredentialRowActionsTemplateSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "ssh", "credential-row-actions.jsx"),
  "utf8",
);
export const sshConnectorListItemTemplateSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "ssh", "list-item.jsx"),
  "utf8",
);
export const sshConnectorConsoleTemplateSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "ssh", "console.jsx"),
  "utf8",
);
export const sshConnectorIndexSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "index.jsx"), "utf8");
export const sshConnectorMetadataSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "metadata.json"), "utf8");
export const sshConnectorModelSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "model.js"), "utf8");
export const sshConnectorOperationsSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "ssh", "operations.jsx"),
  "utf8",
);
export const postgresConnectorFormTemplateSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "postgres", "form.jsx"),
  "utf8",
);
export const postgresCredentialFormTemplateSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "postgres", "credential-form.jsx"),
  "utf8",
);
export const postgresConnectorListItemTemplateSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "postgres", "list-item.jsx"),
  "utf8",
);
export const postgresConnectorConsoleTemplateSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "postgres", "console.jsx"),
  "utf8",
);
export const sharedSQLConsoleSource = readFileSync(join(currentDir, "..", "connectors", "templates", "_shared", "sql-console.jsx"), "utf8");
export const sharedSQLConsoleSupportSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "_shared", "sql-console-support.jsx"),
  "utf8",
);
export const sharedSQLEditorSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "_shared", "sql-editor.jsx"),
  "utf8",
);
export const sharedNetworkTransportSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "_shared", "network-transport-fields.jsx"),
  "utf8",
);
export const sharedDatabaseConnectorModelSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "_shared", "database-connector-model.js"),
  "utf8",
);
export const postgresSQLConsoleSource = `${postgresConnectorConsoleTemplateSource}\n${sharedSQLConsoleSource}\n${sharedSQLConsoleSupportSource}\n${sharedSQLEditorSource}`;
export const postgresConnectorIndexSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "postgres", "index.jsx"),
  "utf8",
);
export const postgresConnectorMetadataSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "postgres", "metadata.json"),
  "utf8",
);
export const postgresConnectorModelSource = readFileSync(join(currentDir, "..", "connectors", "templates", "postgres", "model.js"), "utf8");
export const postgresConnectorOperationsSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "postgres", "operations.jsx"),
  "utf8",
);
export const clickHouseConnectorFormSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "clickhouse", "form.jsx"),
  "utf8",
);
export const clickHouseConnectorConsoleSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "clickhouse", "console.jsx"),
  "utf8",
);
export const clickHouseConnectorIndexSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "clickhouse", "index.jsx"),
  "utf8",
);
export const clickHouseConnectorMetadataSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "clickhouse", "metadata.json"),
  "utf8",
);
export const clickHouseConnectorModelSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "clickhouse", "model.js"),
  "utf8",
);
export const redisConnectorFormTemplateSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "redis", "form.jsx"),
  "utf8",
);
export const rabbitMQConnectorFormTemplateSource = readFileSync(
  join(currentDir, "..", "connectors", "templates", "rabbitmq", "form.jsx"),
  "utf8",
);
export const vaultPageSource = readFileSync(join(currentDir, "..", "pages", "vault.jsx"), "utf8");
export const vaultComponentsSource = readFileSync(join(currentDir, "..", "pages", "vault-components.jsx"), "utf8");
export const vaultFeatureSource = `${vaultPageSource}\n${vaultComponentsSource}`;
export const vaultSessionDialogSource = readFileSync(join(currentDir, "..", "components", "console", "vault-session-dialog.jsx"), "utf8");
export const ptyConsoleSource = readFileSync(join(currentDir, "..", "components", "console", "pty-console.jsx"), "utf8");
export const vaultActionApprovalDialogSource = readFileSync(
  join(currentDir, "..", "components", "vault", "vault-action-approval-dialog.jsx"),
  "utf8",
);

export function backendRegisteredConnectorKinds(source) {
  const connectorImports = new Map();
  for (const match of source.matchAll(/(\w+)\s+"github\.com\/aipermission\/aipermission\/backend\/internal\/connectors\/([^"]+)"/g)) {
    const alias = match[1];
    const parts = match[2].split("/");
    connectorImports.set(alias, parts[0]);
  }
  const kinds = [];
  for (const match of source.matchAll(/(\w+)\.New\(\)/g)) {
    const kind = connectorImports.get(match[1]);
    if (kind) kinds.push(kind);
  }
  return [...new Set(kinds)].sort();
}
