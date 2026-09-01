import assert from "node:assert/strict";
import test from "node:test";
import {
  sidebarSource,
  connectorApprovalDialogSource,
  connectorActivityDialogSource,
  shellSource,
  historySource,
  auditLogsSource,
  consolePageSource,
  fileTransferDialogSource,
  fileTransferBrowserSource,
  fileTransferConfirmSource,
  bulkCommandDialogSource,
  transferCenterSource,
  fileTransferActionsSource,
  tokenPermissionPanelSource,
  connectorTokenPermissionPanelSource,
  connectorPermissionDialogSource,
  mailConsoleSource,
  sshConnectorConsoleTemplateSource,
  postgresSQLConsoleSource,
  vaultSessionDialogSource,
  ptyConsoleSource,
} from "./app-smoke-fixtures.js";

test("Console exposes connector action approvals", () => {
  assert.match(shellSource, /\/api\/targets/);
  assert.match(shellSource, /\/api\/connector-action-approvals/);
  assert.doesNotMatch(shellSource, /\/api\/approvals/);
  assert.doesNotMatch(consolePageSource, /components\/console\/approval-dialog|<ApprovalDialog\b|activeApprovalSnapshot/);
  assert.equal(consolePageSource.includes("run" + "Approval"), false);
  assert.equal(consolePageSource.includes("decline" + "Approval"), false);
  assert.doesNotMatch(consolePageSource, /\/api\/approvals/);
  assert.doesNotMatch(bulkCommandDialogSource, /\/api\/approvals/);
  assert.match(bulkCommandDialogSource, /\/api\/console\/command-requests\/\$\{item\.request_id\}/);
  assert.match(consolePageSource, /ConnectorActionApprovalDialog/);
  assert.match(consolePageSource, /ConnectorActivityDialog/);
  assert.match(consolePageSource, /SelectedConnectorConsoleTemplate/);
  assert.match(consolePageSource, /selectedConnectorTemplate\?\.Console/);
  assert.match(consolePageSource, /useConnectorPermissions/);
  assert.match(mailConsoleSource, /pendingActions/);
  assert.match(mailConsoleSource, /outboundPending/);
  assert.match(mailConsoleSource, /generation !== requestGeneration\.current/);
  assert.match(mailConsoleSource, /submission_status === "submission_unknown"/);
  assert.match(mailConsoleSource, /reconcilePendingAction/);
  assert.match(mailConsoleSource, /case "get_message"/);
  assert.match(mailConsoleSource, /case "move_message"/);
  assert.match(postgresSQLConsoleSource, /PostgresConnectorToolbarActionsTemplate/);
  assert.match(postgresSQLConsoleSource, /browserLabel: "Schema"/);
  assert.match(postgresSQLConsoleSource, /Search \$\{namespaceLabel\.toLowerCase\(\)\}s or tables/);
  assert.match(postgresSQLConsoleSource, /prepareTableQuery/);
  assert.match(postgresSQLConsoleSource, /filteredTableBrowserRows/);
  assert.match(postgresSQLConsoleSource, /rowsToCSVText/);
  assert.match(postgresSQLConsoleSource, /filenamePrefix: "postgres-result"/);
  assert.match(postgresSQLConsoleSource, /Session requests/);
  assert.match(postgresSQLConsoleSource, /No active \{config\.label\} session/);
  assert.match(postgresSQLConsoleSource, /monaco-editor\/esm\/vs\/editor\/editor\.api/);
  assert.match(postgresSQLConsoleSource, /queryAction: "query_readonly"/);
  assert.match(postgresSQLConsoleSource, /describeAction: "describe_table"/);
  assert.match(postgresSQLConsoleSource, /FROM pg_class c/);
  assert.match(postgresSQLConsoleSource, /json_agg/);
  assert.match(postgresSQLConsoleSource, /a\.attnum/);
  assert.match(postgresSQLConsoleSource, /ChevronRight/);
  assert.match(postgresSQLConsoleSource, /referencedTablesFromSQL/);
  assert.match(postgresSQLConsoleSource, /tableMatchesReference/);
  assert.match(postgresSQLConsoleSource, /CompletionItemKind\.Field/);
  assert.match(postgresSQLConsoleSource, /fixedOverflowWidgets: true/);
  assert.match(postgresSQLConsoleSource, /suggestController\.js/);
  assert.match(postgresSQLConsoleSource, /acceptSuggestionOnEnter: "on"/);
  assert.match(postgresSQLConsoleSource, /KeyMod\.CtrlCmd \| monacoInstance\.KeyCode\.Enter/);
  assert.match(postgresSQLConsoleSource, /Run SQL \(Ctrl\+Enter\)/);
  assert.match(postgresSQLConsoleSource, /Result View/);
  assert.match(consolePageSource, /structuredSessionsByTarget/);
  assert.match(consolePageSource, /onNewStructuredSession/);
  assert.match(consolePageSource, /onNewStructuredSession=\{startStructuredConnectorSession\}/);
  assert.match(consolePageSource, /target=/);
  assert.match(consolePageSource, /Search connectors/);
  assert.match(consolePageSource, /Connectors/);
  assert.match(consolePageSource, /targetUsesLiveConsole/);
  assert.match(consolePageSource, /recoverableRunningActions/);
  assert.doesNotMatch(consolePageSource, /connector_kind === "ssh"/);
  assert.match(consolePageSource, /getConnectorModel/);
  assert.match(consolePageSource, /ConnectorIcon/);
  assert.match(tokenPermissionPanelSource, /ConnectorTokenPermissionPanel/);
  assert.match(connectorTokenPermissionPanelSource, /connectorPermissionState/);
  assert.match(connectorTokenPermissionPanelSource, /loadConnectorActions\?\.\(\{ \.\.\.selectedTarget, profile_id: profile\.profile_id/);
  assert.match(connectorTokenPermissionPanelSource, /connectorActionCacheKey\(selectedTarget, profile\.profile_id\)/);
  assert.match(connectorPermissionDialogSource, /\/api\/connector-targets\/inventory/);
  assert.match(connectorTokenPermissionPanelSource, /ProfileLifetimeControls/);
  assert.match(sshConnectorConsoleTemplateSource, /SSHConnectorToolbarActionsTemplate/);
  assert.match(consolePageSource, /pendingConnectorApprovals/);
  assert.match(consolePageSource, /runConnectorActionApproval/);
  assert.match(consolePageSource, /declineConnectorActionApproval/);
  assert.match(connectorApprovalDialogSource, /structured connector action/);
  assert.match(connectorApprovalDialogSource, /Decline note/);
  assert.match(connectorApprovalDialogSource, /approval\.preview/);
  assert.match(connectorApprovalDialogSource, /Approval preview/);
  assert.match(connectorActivityDialogSource, /Recent structured connector requests/);
  assert.match(connectorActivityDialogSource, /always-run requests/);
});

test("History page exposes label filtering and item label endpoints", () => {
  assert.match(historySource, /\/api\/history-labels/);
  assert.match(historySource, /\/api\/history\?/);
  assert.match(historySource, /\/api\/history\/\$\{item\.id\}/);
  assert.match(historySource, /\/api\/history\/\$\{id\}\/labels/);
  assert.match(historySource, /All connectors/);
  assert.match(historySource, /targetRef/);
  assert.match(historySource, /target_id/);
  assert.match(historySource, /runtime_id/);
  assert.match(historySource, /label_id/);
  assert.match(historySource, /source/);
  assert.match(historySource, /connector_kind/);
  assert.doesNotMatch(historySource, /All activity/);
  assert.match(historySource, /SourceBadge/);
  assert.match(historySource, /Not tracked/);
  assert.match(historySource, /Stale/);
  assert.doesNotMatch(historySource, /setLabelDialogOpen/);
});

test("Audit page exposes connector-aware filters", () => {
  assert.match(auditLogsSource, /connector_kind/);
  assert.match(auditLogsSource, /target_id/);
  assert.match(auditLogsSource, /All connectors/);
  assert.match(auditLogsSource, /auditTargetLabel/);
  assert.match(auditLogsSource, /connectorKindOptions/);
});

test("Console and History expose connector file transfer flows", () => {
  assert.match(historySource, /file_transfer/);
  assert.match(historySource, /TransferDetail/);
  assert.match(historySource, /\/api\/file-transfers\/\$\{item\.source_ref_id\}\/download/);
  assert.match(historySource, /Save download/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfers\/upload-batch/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfers\/download-batch/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfers\/browse/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfers\/expand/);
  assert.match(fileTransferBrowserSource, /Load more/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfer-batches\/\$\{batch\.item\.id\}\/pause/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfer-batches\/\$\{batch\.item\.id\}\/resume/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfer-batches\/\$\{batch\.item\.id\}\/cancel/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfer-batches\/\$\{batch\.item\.id\}\/queue/);
  assert.match(fileTransferDialogSource, /remote_files_exist/);
  assert.match(fileTransferConfirmSource, /Overwrite all/);
  assert.match(`${fileTransferDialogSource}\n${fileTransferBrowserSource}\n${fileTransferConfirmSource}`, /closeOnOverlay=\{false\}/);
  assert.match(fileTransferDialogSource, /apiPostForm/);
  assert.match(fileTransferDialogSource, /short-lived local staging files/);
  assert.match(shellSource, /\/api\/file-transfer-batches\?limit=30/);
  assert.match(sidebarSource, /Transfers/);
  assert.match(transferCenterSource, /Transfer Center/);
  assert.match(transferCenterSource, /Closing this panel does not stop transfers/);
  assert.match(transferCenterSource, /pending_approval/);
  assert.match(transferCenterSource, /Approve selected/);
  assert.match(shellSource, /createFileTransferBatchActions/);
  assert.match(shellSource, /onApprove=\{transferBatchActions\.approve\}/);
  assert.match(shellSource, /onDecline=\{transferBatchActions\.decline\}/);
  assert.match(fileTransferActionsSource, /\/api\/file-transfer-batches\/\$\{batchID\}\/approve/);
  assert.match(fileTransferActionsSource, /\/api\/file-transfer-batches\/\$\{batchID\}\/decline/);
});

test("Console exposes stuck command recovery controls", () => {
  assert.match(shellSource, /restartConsoleRuntime/);
  assert.match(shellSource, /\/api\/console\/runtime-surfaces\/\$\{runtimeID\}\/restart/);
  assert.match(consolePageSource, /ConsoleRecoveryPanel/);
  assert.match(consolePageSource, /AI command running/);
  assert.match(consolePageSource, /Manual command running/);
  assert.match(consolePageSource, /Looks stuck\? Restart opens a fresh console session/);
  assert.match(consolePageSource, /commandPreview/);
  assert.match(consolePageSource, /Restart/);
});

test("Console starts supported sessions with explicit Vault environment choices", () => {
  assert.match(shellSource, /\/api\/vault-session-options\?runtime_id=/);
  assert.match(shellSource, /vault_items:/);
  assert.match(shellSource, /deferActivation: true/);
  assert.match(shellSource, /setTimeout\(\(\) => activateConsoleSession\(session\), 0\)/);
  assert.match(shellSource, /setTimeout\(\(\) => attachConsoleSession\(session\.id\), 0\)/);
  assert.match(ptyConsoleSource, /lastTranscriptRef\.current = ""/);
  assert.match(ptyConsoleSource, /syncTerminalTranscript\(terminal, lastTranscriptRef, latestTranscriptRef\.current\)/);
  assert.match(vaultSessionDialogSource, /Start session with Vault environment/);
  assert.match(vaultSessionDialogSource, /target_project_id/);
  assert.match(vaultSessionDialogSource, /Search this project/);
  assert.match(vaultSessionDialogSource, /Overwrite existing shell value/);
  assert.doesNotMatch(vaultSessionDialogSource, /allowedProjectIDs/);
});

test("Console exposes bulk command execution controls", () => {
  assert.doesNotMatch(consolePageSource, /BulkCommandDialog/);
  assert.match(sshConnectorConsoleTemplateSource, /BulkCommandDialog/);
  assert.match(sshConnectorConsoleTemplateSource, /Bulk/);
  assert.match(bulkCommandDialogSource, /\/api\/console\/bulk-exec/);
  assert.match(bulkCommandDialogSource, /RUN ON \$\{selectedIDs\.length\} TARGETS/);
  assert.match(bulkCommandDialogSource, /Run selected/);
});
