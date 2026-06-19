package api

import (
	"net/http"
	"time"
)

type tokenHandlers struct{ *Server }
type credentialHandlers struct{ *Server }
type consoleHandlers struct{ *Server }
type securityHandlers struct{ *Server }
type retentionHandlers struct{ *Server }
type redactionRuleHandlers struct{ *Server }
type auditHandlers struct{ *Server }
type backupHandlers struct{ *Server }
type databaseHandlers struct{ *Server }
type unlockHandlers struct{ *Server }
type messageHandlers struct{ *Server }
type historyEntryHandlers struct{ *Server }
type historyLabelHandlers struct{ *Server }
type fileTransferHandlers struct{ *Server }
type connectorHandlers struct{ *Server }
type connectorTargetHandlers struct{ *Server }
type targetHandlers struct{ *Server }
type mcpHandlers struct{ *Server }
type maintenanceConsoleHandlers struct{ *Server }

func (s *Server) routes() {
	unlock := unlockHandlers{s}
	security := securityHandlers{s}
	retention := retentionHandlers{s}
	redactionRules := redactionRuleHandlers{s}
	credentials := credentialHandlers{s}
	tokens := tokenHandlers{s}
	backup := backupHandlers{s}
	databases := databaseHandlers{s}
	console := consoleHandlers{s}
	connectorApprovals := connectorActionApprovalHandlers{s}
	messages := messageHandlers{s}
	audit := auditHandlers{s}
	historyEntries := historyEntryHandlers{s}
	historyLabels := historyLabelHandlers{s}
	fileTransfers := fileTransferHandlers{s}
	connectors := connectorHandlers{s}
	connectorActions := connectorActionHandlers{s}
	connectorTargets := connectorTargetHandlers{s}
	targets := targetHandlers{s}
	mcp := mcpHandlers{s}
	maintenanceConsole := maintenanceConsoleHandlers{s}

	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("GET /api/status", s.status)
	s.mux.HandleFunc("GET /api/settings/security", security.getSecuritySettings)
	s.mux.HandleFunc("PUT /api/settings/security", security.updateSecuritySettings)
	s.mux.HandleFunc("GET /api/settings/retention", retention.getRetentionSettings)
	s.mux.HandleFunc("PUT /api/settings/retention", retention.updateRetentionSettings)
	s.mux.HandleFunc("POST /api/settings/retention/purge", retention.purgeRetention)
	s.mux.HandleFunc("GET /api/settings/redaction-rules", redactionRules.listRedactionRules)
	s.mux.HandleFunc("POST /api/settings/redaction-rules", redactionRules.createRedactionRule)
	s.mux.HandleFunc("PUT /api/settings/redaction-rules/{id}", redactionRules.updateRedactionRule)
	s.mux.HandleFunc("DELETE /api/settings/redaction-rules/{id}", redactionRules.deleteRedactionRule)
	s.mux.HandleFunc("GET /api/settings/maintenance-console/status", maintenanceConsole.status)
	s.mux.HandleFunc("POST /api/settings/maintenance-console/open", maintenanceConsole.open)
	s.mux.HandleFunc("GET /api/settings/maintenance-console/attach", maintenanceConsole.attach)
	s.mux.HandleFunc("POST /api/settings/maintenance-console/close", maintenanceConsole.close)
	s.mux.HandleFunc("GET /api/unlock/status", unlock.unlockStatus)
	s.mux.HandleFunc("POST /api/unlock/setup", unlock.setupUnlock)
	s.mux.HandleFunc("POST /api/unlock", unlock.unlock)
	s.mux.HandleFunc("POST /api/lock", unlock.lock)
	s.mux.HandleFunc("GET /api/connectors/{kind}/credentials", credentials.listCredentials)
	s.mux.HandleFunc("POST /api/connectors/{kind}/credentials", credentials.createCredential)
	s.mux.HandleFunc("POST /api/connectors/{kind}/credentials/import", credentials.importCredential)
	s.mux.HandleFunc("GET /api/connectors/{kind}/credentials/{id}", credentials.getCredential)
	s.mux.HandleFunc("PUT /api/connectors/{kind}/credentials/{id}", credentials.updateCredential)
	s.mux.HandleFunc("DELETE /api/connectors/{kind}/credentials/{id}", credentials.deleteCredential)
	s.mux.HandleFunc("POST /api/connector-targets/{id}/operations/{operation}", connectorTargets.runConnectorTargetOperation)
	s.mux.HandleFunc("GET /api/tokens", tokens.listTokens)
	s.mux.HandleFunc("POST /api/tokens", tokens.createToken)
	s.mux.HandleFunc("POST /api/tokens/{id}/revoke", tokens.revokeToken)
	s.mux.HandleFunc("GET /api/tokens/{id}/connector-permissions", tokens.listTokenConnectorPermissions)
	s.mux.HandleFunc("PUT /api/tokens/{id}/connector-permissions", tokens.updateTokenConnectorPermissions)
	s.mux.HandleFunc("GET /api/backup/download", backup.downloadDatabase)
	s.mux.HandleFunc("POST /api/backup/import", backup.importDatabase)
	s.mux.HandleFunc("GET /api/backup/providers/catalog", backup.providerCatalog)
	s.mux.HandleFunc("GET /api/backup/providers", backup.listProviders)
	s.mux.HandleFunc("POST /api/backup/providers", backup.createProvider)
	s.mux.HandleFunc("PUT /api/backup/providers/{id}", backup.updateProvider)
	s.mux.HandleFunc("DELETE /api/backup/providers/{id}", backup.deleteProvider)
	s.mux.HandleFunc("GET /api/backup/providers/{id}/records", backup.listProviderRecords)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/upload", backup.uploadProviderBackup)
	s.mux.HandleFunc("GET /api/backup/providers/{id}/records/{record_id}/download", backup.downloadProviderRecord)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/records/{record_id}/restore", backup.restoreProviderRecord)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/google/device/start", backup.startGoogleDeviceFlow)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/google/device/poll", backup.pollGoogleDeviceFlow)
	s.mux.HandleFunc("POST /api/databases/rename", databases.renameDatabase)
	s.mux.HandleFunc("POST /api/databases/delete", databases.deleteDatabase)
	s.mux.HandleFunc("POST /api/databases/delete-locked", databases.deleteLockedDatabase)
	s.mux.HandleFunc("POST /api/databases/switch", databases.switchDatabase)
	s.mux.HandleFunc("POST /api/databases/change-password", databases.changeDatabasePassword)
	s.mux.HandleFunc("POST /api/console/bulk-exec", console.runBulkConsoleCommand)
	s.mux.HandleFunc("GET /api/console/sessions", console.listConsoleSessions)
	s.mux.HandleFunc("POST /api/console/sessions", console.createConsoleSession)
	s.mux.HandleFunc("GET /api/console/sessions/{id}", console.getConsoleSession)
	s.mux.HandleFunc("POST /api/console/sessions/{id}/input", console.inputConsoleSession)
	s.mux.HandleFunc("POST /api/console/sessions/{id}/close", console.closeConsoleSession)
	s.mux.HandleFunc("GET /api/console/sessions/{id}/attach", console.attachConsoleSession)
	s.mux.HandleFunc("POST /api/console/runtime-surfaces/{id}/restart", console.restartTargetConsoleSession)
	s.mux.HandleFunc("POST /api/console/targets/{id}/restart", console.restartTargetConsoleSession)
	s.mux.HandleFunc("GET /api/console/command-requests/{id}", console.getConsoleCommandRequest)
	s.mux.HandleFunc("GET /api/connector-action-approvals", connectorApprovals.listConnectorActionApprovals)
	s.mux.HandleFunc("GET /api/connector-action-approvals/{id}", connectorApprovals.getConnectorActionApproval)
	s.mux.HandleFunc("POST /api/connector-action-approvals/{id}/run", connectorApprovals.runConnectorActionApproval)
	s.mux.HandleFunc("POST /api/connector-action-approvals/{id}/decline", connectorApprovals.declineConnectorActionApproval)
	s.mux.HandleFunc("POST /api/connector-actions/local-run", connectorActions.runLocalConnectorAction)
	s.mux.HandleFunc("GET /api/history/targets", historyEntries.listHistoryTargetFacets)
	s.mux.HandleFunc("GET /api/history", historyEntries.listHistoryEntries)
	s.mux.HandleFunc("GET /api/history/{id}", historyEntries.getHistoryEntry)
	s.mux.HandleFunc("POST /api/history/{id}/labels", historyLabels.attachHistoryEntryLabel)
	s.mux.HandleFunc("DELETE /api/history/{id}/labels/{label_id}", historyLabels.detachHistoryEntryLabel)
	s.mux.HandleFunc("GET /api/history-labels", historyLabels.listHistoryLabels)
	s.mux.HandleFunc("POST /api/history-labels", historyLabels.createHistoryLabel)
	s.mux.HandleFunc("DELETE /api/history-labels/{id}", historyLabels.deleteHistoryLabel)
	s.mux.HandleFunc("GET /api/file-transfers", fileTransfers.listFileTransfers)
	s.mux.HandleFunc("GET /api/file-transfers/{id}", fileTransfers.getFileTransfer)
	s.mux.HandleFunc("GET /api/file-transfers/{id}/download", fileTransfers.downloadTransferredFile)
	s.mux.HandleFunc("POST /api/file-transfers/{id}/cancel", fileTransfers.cancelFileTransfer)
	s.mux.HandleFunc("GET /api/file-transfer-batches", fileTransfers.listFileTransferBatches)
	s.mux.HandleFunc("GET /api/file-transfer-batches/{id}", fileTransfers.getFileTransferBatch)
	s.mux.HandleFunc("GET /api/file-transfer-batches/{id}/download", fileTransfers.downloadFileTransferBatch)
	s.mux.HandleFunc("POST /api/file-transfer-batches/{id}/pause", fileTransfers.pauseFileTransferBatch)
	s.mux.HandleFunc("POST /api/file-transfer-batches/{id}/resume", fileTransfers.resumeFileTransferBatch)
	s.mux.HandleFunc("POST /api/file-transfer-batches/{id}/cancel", fileTransfers.cancelFileTransferBatch)
	s.mux.HandleFunc("POST /api/file-transfer-batches/{id}/queue", fileTransfers.updateFileTransferBatchQueue)
	s.mux.HandleFunc("POST /api/file-transfer-batches/{id}/approve", fileTransfers.approveFileTransferBatch)
	s.mux.HandleFunc("POST /api/file-transfer-batches/{id}/decline", fileTransfers.declineFileTransferBatch)
	s.mux.HandleFunc("POST /api/file-transfers/browse", fileTransfers.browseRemoteFiles)
	s.mux.HandleFunc("POST /api/file-transfers/upload", fileTransfers.startUpload)
	s.mux.HandleFunc("POST /api/file-transfers/upload-batch", fileTransfers.startUploadBatch)
	s.mux.HandleFunc("POST /api/file-transfers/download", fileTransfers.startDownload)
	s.mux.HandleFunc("POST /api/file-transfers/download-batch", fileTransfers.startDownloadBatch)
	s.mux.HandleFunc("GET /api/connectors", connectors.listConnectors)
	s.mux.HandleFunc("GET /api/connectors/{kind}", connectors.getConnector)
	s.mux.HandleFunc("GET /api/targets", targets.listTargets)
	s.mux.HandleFunc("GET /api/connector-targets", connectorTargets.listConnectorTargets)
	s.mux.HandleFunc("GET /api/connector-targets/inventory", connectorTargets.listConnectorTargetInventory)
	s.mux.HandleFunc("POST /api/connector-targets/with-profile", connectorTargets.createConnectorTargetWithProfile)
	s.mux.HandleFunc("POST /api/connector-targets", connectorTargets.createConnectorTarget)
	s.mux.HandleFunc("POST /api/connector-targets/ping", connectorTargets.pingConnectorTargetHost)
	s.mux.HandleFunc("POST /api/connector-targets/test", connectorTargets.testConnectorTargetDraft)
	s.mux.HandleFunc("GET /api/connector-targets/{id}", connectorTargets.getConnectorTarget)
	s.mux.HandleFunc("PUT /api/connector-targets/{id}/with-profile/{profile_id}", connectorTargets.updateConnectorTargetWithProfile)
	s.mux.HandleFunc("PUT /api/connector-targets/{id}", connectorTargets.updateConnectorTarget)
	s.mux.HandleFunc("DELETE /api/connector-targets/{id}", connectorTargets.deleteConnectorTarget)
	s.mux.HandleFunc("GET /api/connector-targets/{id}/profiles", connectorTargets.listConnectorCredentialProfiles)
	s.mux.HandleFunc("POST /api/connector-targets/{id}/profiles", connectorTargets.createConnectorCredentialProfile)
	s.mux.HandleFunc("POST /api/connector-targets/{id}/profiles/{profile_id}/provision", connectorTargets.provisionConnectorCredentialProfile)
	s.mux.HandleFunc("GET /api/connector-targets/{id}/profiles/{profile_id}/backup", connectorTargets.downloadConnectorProfileBackup)
	s.mux.HandleFunc("POST /api/connector-targets/{id}/profiles/{profile_id}/restore", connectorTargets.restoreConnectorProfileBackup)
	s.mux.HandleFunc("PUT /api/connector-targets/{id}/profiles/{profile_id}", connectorTargets.updateConnectorCredentialProfile)
	s.mux.HandleFunc("DELETE /api/connector-targets/{id}/profiles/{profile_id}", connectorTargets.deleteConnectorCredentialProfile)
	s.mux.HandleFunc("POST /api/connector-targets/{id}/profiles/{profile_id}/test", connectorTargets.testConnectorCredentialProfile)
	s.mux.HandleFunc("GET /api/connector-targets/{id}/profiles/{profile_id}/actions", connectorTargets.listConnectorCredentialProfileActions)
	s.mux.HandleFunc("GET /api/messages", messages.listMessages)
	s.mux.HandleFunc("POST /api/messages", messages.createMessage)
	s.mux.HandleFunc("POST /api/messages/read", messages.markMessagesRead)
	s.mux.HandleFunc("GET /api/audit-logs", audit.listAuditLogs)
	s.mux.HandleFunc("GET /api/audit-logs/{id}", audit.getAuditLog)
	s.mux.HandleFunc("GET /api/settings/mcp-runtime", mcp.getMCPRuntime)
	s.mux.HandleFunc("PUT /api/settings/mcp-runtime", mcp.updateMCPRuntime)
	s.mux.HandleFunc("GET /api/mcp/connector-targets", mcp.mcpListConnectorTargets)
	s.mux.HandleFunc("GET /api/mcp/connector-help", mcp.mcpGetConnectorHelp)
	s.mux.HandleFunc("GET /api/mcp/connector-actions", mcp.mcpGetConnectorActions)
	s.mux.HandleFunc("POST /api/mcp/connector-actions/call", mcp.mcpCallConnectorAction)
	s.mux.HandleFunc("GET /api/mcp/connector-action-requests/{id}", mcp.mcpGetConnectorActionRequest)
	registerConnectorAdapterRoutes(s.mux, s)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "aipermission",
		"status":  "running",
		"config":  s.config.PublicStatusMinimal(),
		"features": []string{
			"local-docker-runtime",
			"react-dashboard",
			"sqlcipher-sqlite-storage",
			"database-unlock-screen",
			"encrypted-vault",
			"credential-management",
			"connector-target-management",
			"api-token-management",
			"connector-action-permissions",
			"persistent-console-sessions",
			"local-node-mcp-bridge",
			"encrypted-backup-restore",
			"mcp-gateway",
		},
	})
}
