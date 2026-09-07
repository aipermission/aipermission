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
  sshConnectorConsoleTemplateSource,
  vaultSessionDialogSource,
  ptyConsoleSource,
} from "./app-smoke-fixtures.js";

test("Console exposes connector action approvals", () => {
  assert.match(shellSource, /\/api\/targets/);
  assert.match(shellSource, /\/api\/connector-action-approvals/);
  assert.doesNotMatch(shellSource, /\/api\/approvals/);
  assert.doesNotMatch(consolePageSource, /components\/console\/approval-dialog|<ApprovalDialog\b|activeApprovalSnapshot/);
  assert.match(consolePageSource, /useConnectorApprovalDialog/);
  assert.doesNotMatch(consolePageSource, /\/api\/connector-action-approvals\/\$\{/);
  assert.doesNotMatch(consolePageSource, /\/api\/approvals/);
  assert.doesNotMatch(bulkCommandDialogSource, /\/api\/approvals/);
  assert.match(bulkCommandDialogSource, /\/api\/console\/command-requests\/\$\{item\.request_id\}/);
  assert.match(consolePageSource, /ConnectorActionApprovalDialog/);
  assert.match(consolePageSource, /ConnectorActivityDialog/);
  assert.match(consolePageSource, /useConsoleConnectorView/);
  assert.match(consolePageSource, /Console: selectedTemplate\?\.Console/);
  assert.match(consolePageSource, /useConnectorPermissions/);
  assert.match(consolePageSource, /useConsoleWorkspaceSession/);
  assert.match(consolePageSource, /onNewStructuredSession/);
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
  assert.match(fileTransferDialogSource, /transitionBatch\("pausing", "pause"\)/);
  assert.match(fileTransferDialogSource, /transitionBatch\("resuming", "resume"\)/);
  assert.match(fileTransferDialogSource, /transitionBatch\("canceling", "cancel"/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfer-batches\/\$\{batch\.item\.id\}\/queue/);
  assert.match(fileTransferDialogSource, /remote_files_exist/);
  assert.match(fileTransferConfirmSource, /Overwrite all/);
  assert.match(`${fileTransferDialogSource}\n${fileTransferBrowserSource}\n${fileTransferConfirmSource}`, /closeOnOverlay=\{false\}/);
  assert.match(fileTransferDialogSource, /apiPostForm/);
  assert.match(fileTransferDialogSource, /short-lived local staging files/);
  assert.match(sidebarSource, /Transfers/);
  assert.match(transferCenterSource, /Transfer Center/);
  assert.match(transferCenterSource, /Closing this panel does not stop transfers/);
  assert.match(transferCenterSource, /pending_approval/);
  assert.match(transferCenterSource, /Approve selected/);
  assert.match(fileTransferActionsSource, /\/api\/file-transfer-batches\/\$\{batchID\}\/approve/);
  assert.match(fileTransferActionsSource, /\/api\/file-transfer-batches\/\$\{batchID\}\/decline/);
});

test("Console exposes stuck command recovery controls", () => {
  assert.match(consolePageSource, /ConsoleRecoveryPanel/);
  assert.match(consolePageSource, /AI command running/);
  assert.match(consolePageSource, /Manual command running/);
  assert.match(consolePageSource, /Looks stuck\? Restart opens a fresh console session/);
  assert.match(consolePageSource, /commandPreview/);
  assert.match(consolePageSource, /Restart/);
});

test("Console starts supported sessions with explicit Vault environment choices", () => {
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
