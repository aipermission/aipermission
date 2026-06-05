package api

import (
	"net/http"
	"time"
)

type tokenHandlers struct{ *Server }
type sshKeyHandlers struct{ *Server }
type sshConfigHandlers struct{ *Server }
type sshHostKeyHandlers struct{ *Server }
type serverResourceHandlers struct{ *Server }
type serverConnectionHandlers struct{ *Server }
type consoleHandlers struct{ *Server }
type securityHandlers struct{ *Server }
type retentionHandlers struct{ *Server }
type redactionRuleHandlers struct{ *Server }
type auditHandlers struct{ *Server }
type backupHandlers struct{ *Server }
type databaseHandlers struct{ *Server }
type unlockHandlers struct{ *Server }
type messageHandlers struct{ *Server }
type approvalHandlers struct{ *Server }
type historyLabelHandlers struct{ *Server }
type fileTransferHandlers struct{ *Server }
type mcpHandlers struct{ *Server }

func (s *Server) routes() {
	unlock := unlockHandlers{s}
	security := securityHandlers{s}
	retention := retentionHandlers{s}
	redactionRules := redactionRuleHandlers{s}
	serverResources := serverResourceHandlers{s}
	serverConnections := serverConnectionHandlers{s}
	sshHostKeys := sshHostKeyHandlers{s}
	sshKeys := sshKeyHandlers{s}
	sshConfig := sshConfigHandlers{s}
	tokens := tokenHandlers{s}
	backup := backupHandlers{s}
	databases := databaseHandlers{s}
	console := consoleHandlers{s}
	approvals := approvalHandlers{s}
	messages := messageHandlers{s}
	audit := auditHandlers{s}
	historyLabels := historyLabelHandlers{s}
	fileTransfers := fileTransferHandlers{s}
	mcp := mcpHandlers{s}

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
	s.mux.HandleFunc("GET /api/unlock/status", unlock.unlockStatus)
	s.mux.HandleFunc("POST /api/unlock/setup", unlock.setupUnlock)
	s.mux.HandleFunc("POST /api/unlock", unlock.unlock)
	s.mux.HandleFunc("POST /api/lock", unlock.lock)
	s.mux.HandleFunc("GET /api/servers", serverResources.listServers)
	s.mux.HandleFunc("POST /api/servers", serverResources.createServer)
	s.mux.HandleFunc("GET /api/servers/{id}", serverResources.getServer)
	s.mux.HandleFunc("PUT /api/servers/{id}", serverResources.updateServer)
	s.mux.HandleFunc("DELETE /api/servers/{id}", serverResources.deleteServer)
	s.mux.HandleFunc("POST /api/servers/{id}/test", serverConnections.testServer)
	s.mux.HandleFunc("POST /api/servers/{id}/docker-check", serverConnections.checkDocker)
	s.mux.HandleFunc("POST /api/servers/{id}/docker-logs", serverConnections.readDockerLogs)
	s.mux.HandleFunc("POST /api/servers/test-connection", serverConnections.testServerConnection)
	s.mux.HandleFunc("POST /api/ssh-host-keys/approve", sshHostKeys.approveSSHHostKey)
	s.mux.HandleFunc("GET /api/ssh-keys", sshKeys.listSSHKeys)
	s.mux.HandleFunc("POST /api/ssh-keys", sshKeys.createSSHKey)
	s.mux.HandleFunc("POST /api/ssh-keys/import", sshKeys.importSSHKey)
	s.mux.HandleFunc("GET /api/ssh-keys/{id}", sshKeys.getSSHKey)
	s.mux.HandleFunc("DELETE /api/ssh-keys/{id}", sshKeys.deleteSSHKey)
	s.mux.HandleFunc("GET /api/ssh-config/discover", sshConfig.discoverSSHConfig)
	s.mux.HandleFunc("POST /api/ssh-config/parse", sshConfig.parseSSHConfig)
	s.mux.HandleFunc("GET /api/tokens", tokens.listTokens)
	s.mux.HandleFunc("POST /api/tokens", tokens.createToken)
	s.mux.HandleFunc("POST /api/tokens/{id}/revoke", tokens.revokeToken)
	s.mux.HandleFunc("GET /api/tokens/{id}/permissions", tokens.listTokenPermissions)
	s.mux.HandleFunc("PUT /api/tokens/{id}/permissions", tokens.updateTokenPermissions)
	s.mux.HandleFunc("GET /api/backup/download", backup.downloadDatabase)
	s.mux.HandleFunc("POST /api/backup/import", backup.importDatabase)
	s.mux.HandleFunc("POST /api/databases/rename", databases.renameDatabase)
	s.mux.HandleFunc("POST /api/databases/delete", databases.deleteDatabase)
	s.mux.HandleFunc("POST /api/databases/switch", databases.switchDatabase)
	s.mux.HandleFunc("POST /api/databases/change-password", databases.changeDatabasePassword)
	s.mux.HandleFunc("POST /api/console/exec", serverConnections.consoleExec)
	s.mux.HandleFunc("GET /api/console/sessions", console.listConsoleSessions)
	s.mux.HandleFunc("POST /api/console/sessions", console.createConsoleSession)
	s.mux.HandleFunc("GET /api/console/sessions/{id}", console.getConsoleSession)
	s.mux.HandleFunc("POST /api/console/sessions/{id}/input", console.inputConsoleSession)
	s.mux.HandleFunc("POST /api/console/sessions/{id}/close", console.closeConsoleSession)
	s.mux.HandleFunc("GET /api/console/sessions/{id}/attach", console.attachConsoleSession)
	s.mux.HandleFunc("GET /api/approvals", approvals.listApprovals)
	s.mux.HandleFunc("GET /api/approvals/{id}", approvals.getApproval)
	s.mux.HandleFunc("POST /api/approvals/{id}/run", approvals.runApproval)
	s.mux.HandleFunc("POST /api/approvals/{id}/decline", approvals.declineApproval)
	s.mux.HandleFunc("POST /api/approvals/{id}/labels", historyLabels.attachHistoryLabel)
	s.mux.HandleFunc("DELETE /api/approvals/{id}/labels/{label_id}", historyLabels.detachHistoryLabel)
	s.mux.HandleFunc("GET /api/history-labels", historyLabels.listHistoryLabels)
	s.mux.HandleFunc("POST /api/history-labels", historyLabels.createHistoryLabel)
	s.mux.HandleFunc("DELETE /api/history-labels/{id}", historyLabels.deleteHistoryLabel)
	s.mux.HandleFunc("GET /api/file-transfers", fileTransfers.listFileTransfers)
	s.mux.HandleFunc("GET /api/file-transfers/{id}", fileTransfers.getFileTransfer)
	s.mux.HandleFunc("GET /api/file-transfers/{id}/download", fileTransfers.downloadTransferredFile)
	s.mux.HandleFunc("POST /api/file-transfers/{id}/cancel", fileTransfers.cancelFileTransfer)
	s.mux.HandleFunc("POST /api/file-transfers/browse", fileTransfers.browseRemoteFiles)
	s.mux.HandleFunc("POST /api/file-transfers/upload", fileTransfers.startUpload)
	s.mux.HandleFunc("POST /api/file-transfers/download", fileTransfers.startDownload)
	s.mux.HandleFunc("GET /api/messages", messages.listMessages)
	s.mux.HandleFunc("POST /api/messages", messages.createMessage)
	s.mux.HandleFunc("POST /api/messages/read", messages.markMessagesRead)
	s.mux.HandleFunc("GET /api/audit-logs", audit.listAuditLogs)
	s.mux.HandleFunc("GET /api/audit-logs/{id}", audit.getAuditLog)
	s.mux.HandleFunc("GET /api/settings/mcp-runtime", mcp.getMCPRuntime)
	s.mux.HandleFunc("PUT /api/settings/mcp-runtime", mcp.updateMCPRuntime)
	s.mux.HandleFunc("GET /api/mcp/servers", mcp.mcpListServers)
	s.mux.HandleFunc("POST /api/mcp/exec", mcp.mcpExec)
	s.mux.HandleFunc("GET /api/mcp/console", mcp.mcpReadConsole)
	s.mux.HandleFunc("GET /api/mcp/requests", mcp.mcpListRequests)
	s.mux.HandleFunc("GET /api/mcp/requests/{id}", mcp.mcpGetRequest)
	s.mux.HandleFunc("POST /api/mcp/messages", mcp.mcpCreateMessage)
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
			"ssh-key-management",
			"server-management",
			"api-token-management",
			"token-server-permissions",
			"persistent-console-sessions",
			"local-node-mcp-bridge",
			"encrypted-backup-restore",
			"mcp-gateway",
		},
	})
}
