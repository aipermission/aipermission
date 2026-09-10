package api

//go:generate go run ../../cmd/openapi -routes routes.go -output ../../../docs/api/openapi.json

import (
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	filetransferhttp "github.com/aipermission/aipermission/backend/internal/filetransfer/httpapi"
	historyhttp "github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
	"github.com/aipermission/aipermission/backend/internal/observability"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/retention"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type credentialHandlers struct{ *Server }
type backupHandlers struct{ *Server }
type databaseHandlers struct{ *Server }
type unlockHandlers struct{ *Server }
type connectorTargetHandlers struct{ *Server }
type mcpHandlers struct{ *Server }
type maintenanceConsoleHandlers struct{ *Server }
type diagnosticsHandlers struct{ *Server }

func (s *Server) routes() {
	s.registerSystemRoutes()
	s.registerAccessRoutes()
	s.registerBackupRoutes()
	s.registerConsoleAndActivityRoutes()
	s.registerProjectAndVaultRoutes()
	s.registerTransferRoutes()
	s.registerConnectorRoutes()
	s.registerMessageAndAuditRoutes()
	s.registerMCPRoutes()
	registerConnectorAdapterRoutes(s.mux, s)
}

func (s *Server) registerSystemRoutes() {
	securityHandlers := securitypolicy.NewHTTPHandlers(s.securityPolicyHTTPScope)
	retentionHandlers := retention.NewHTTPHandlers(s.retentionHTTPScope)
	maintenanceConsole := maintenanceConsoleHandlers{s}
	diagnostics := diagnosticsHandlers{s}
	unlock := unlockHandlers{s}

	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("GET /api/status", s.status)
	s.mux.HandleFunc("GET /api/settings/security", securityHandlers.GetSettings)
	s.mux.HandleFunc("PUT /api/settings/security", securityHandlers.UpdateSettings)
	s.mux.HandleFunc("GET /api/settings/retention", retentionHandlers.Get)
	s.mux.HandleFunc("PUT /api/settings/retention", retentionHandlers.Update)
	s.mux.HandleFunc("POST /api/settings/retention/purge", retentionHandlers.Purge)
	s.mux.HandleFunc("GET /api/settings/redaction-rules", securityHandlers.ListRules)
	s.mux.HandleFunc("POST /api/settings/redaction-rules", securityHandlers.CreateRule)
	s.mux.HandleFunc("PUT /api/settings/redaction-rules/{id}", securityHandlers.UpdateRule)
	s.mux.HandleFunc("DELETE /api/settings/redaction-rules/{id}", securityHandlers.DeleteRule)
	s.mux.HandleFunc("GET /api/settings/maintenance-console/status", maintenanceConsole.status)
	s.mux.HandleFunc("POST /api/settings/maintenance-console/open", maintenanceConsole.open)
	s.mux.HandleFunc("GET /api/settings/maintenance-console/attach", maintenanceConsole.attach)
	s.mux.HandleFunc("POST /api/settings/maintenance-console/close", maintenanceConsole.close)
	s.mux.HandleFunc("GET /api/settings/diagnostics", diagnostics.download)
	s.mux.HandleFunc("GET /api/unlock/status", unlock.unlockStatus)
	s.mux.HandleFunc("POST /api/unlock/setup", unlock.setupUnlock)
	s.mux.HandleFunc("POST /api/unlock", unlock.unlock)
	s.mux.HandleFunc("POST /api/lock", unlock.lock)
}

func (s *Server) registerAccessRoutes() {
	credentials := credentialHandlers{s}
	connectorTargets := connectorTargetHandlers{s}
	tokenAccess := accesscontrol.NewHTTPHandlers(s.accessControlScope)

	s.mux.HandleFunc("GET /api/connectors/{kind}/credentials", credentials.listCredentials)
	s.mux.HandleFunc("POST /api/connectors/{kind}/credentials", credentials.createCredential)
	s.mux.HandleFunc("POST /api/connectors/{kind}/credentials/import", credentials.importCredential)
	s.mux.HandleFunc("GET /api/connectors/{kind}/credentials/{id}", credentials.getCredential)
	s.mux.HandleFunc("PUT /api/connectors/{kind}/credentials/{id}", credentials.updateCredential)
	s.mux.HandleFunc("DELETE /api/connectors/{kind}/credentials/{id}", credentials.deleteCredential)
	s.mux.HandleFunc("POST /api/connector-targets/{id}/operations/{operation}", connectorTargets.runConnectorTargetOperation)
	s.mux.HandleFunc("GET /api/tokens", tokenAccess.ListTokens)
	s.mux.HandleFunc("POST /api/tokens", tokenAccess.CreateToken)
	s.mux.HandleFunc("POST /api/tokens/{id}/revoke", tokenAccess.RevokeToken)
	s.mux.HandleFunc("GET /api/tokens/{id}/connector-permissions", tokenAccess.ListConnectorPermissions)
	s.mux.HandleFunc("PUT /api/tokens/{id}/connector-permissions", tokenAccess.UpdateConnectorPermissions)
	s.mux.HandleFunc("GET /api/tokens/{id}/project-scopes", tokenAccess.ListProjectScopes)
	s.mux.HandleFunc("PUT /api/tokens/{id}/project-scopes", tokenAccess.UpdateProjectScopes)
	s.mux.HandleFunc("GET /api/tokens/{id}/project-capabilities", tokenAccess.ListProjectCapabilities)
	s.mux.HandleFunc("PUT /api/tokens/{id}/project-capabilities", tokenAccess.UpdateProjectCapabilities)
}

func (s *Server) registerBackupRoutes() {
	backup := backupHandlers{s}
	providers := backups.NewHTTPHandlers(s.backupProviderHTTPScope)
	databases := databaseHandlers{s}

	s.mux.HandleFunc("GET /api/backup/download", backup.downloadDatabase)
	s.mux.HandleFunc("POST /api/backup/import", backup.importDatabase)
	s.mux.HandleFunc("POST /api/backup/remote/list", backup.listTransientRemoteBackups)
	s.mux.HandleFunc("POST /api/backup/remote/restore", backup.restoreTransientRemoteBackup)
	s.mux.HandleFunc("GET /api/backup/providers/catalog", providers.ProviderCatalog)
	s.mux.HandleFunc("GET /api/backup/providers", providers.ListProviders)
	s.mux.HandleFunc("GET /api/backup/freshness", providers.BackupFreshness)
	s.mux.HandleFunc("POST /api/backup/providers", providers.CreateProvider)
	s.mux.HandleFunc("PUT /api/backup/providers/{id}", providers.UpdateProvider)
	s.mux.HandleFunc("DELETE /api/backup/providers/{id}", providers.DeleteProvider)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/enable", backup.enableProvider)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/test", providers.TestProvider)
	s.mux.HandleFunc("GET /api/backup/providers/{id}/records", providers.ListProviderRecords)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/upload", providers.UploadProviderBackup)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/prune", providers.PruneProviderBackups)
	s.mux.HandleFunc("GET /api/backup/providers/{id}/storage", providers.BackupProviderStorage)
	s.mux.HandleFunc("GET /api/backup/providers/{id}/retention", providers.BackupProviderRetention)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/retention/preview", providers.PreviewBackupProviderRetention)
	s.mux.HandleFunc("PUT /api/backup/providers/{id}/retention", providers.UpdateBackupProviderRetention)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/records/delete", providers.DeleteProviderBackupRecords)
	s.mux.HandleFunc("GET /api/backup/providers/{id}/records/{record_id}/download", providers.DownloadProviderRecord)
	s.mux.HandleFunc("POST /api/backup/providers/{id}/records/{record_id}/restore", backup.restoreProviderRecord)
	s.mux.HandleFunc("POST /api/databases/rename", databases.renameDatabase)
	s.mux.HandleFunc("POST /api/databases/delete", databases.deleteDatabase)
	s.mux.HandleFunc("POST /api/databases/delete-locked", databases.deleteLockedDatabase)
	s.mux.HandleFunc("POST /api/databases/switch", databases.switchDatabase)
	s.mux.HandleFunc("POST /api/databases/change-password", databases.changeDatabasePassword)
}

func (s *Server) registerConsoleAndActivityRoutes() {
	console := connectorapi.NewLiveConsoleHTTPHandlers(s.consoleSessionHTTPScope)
	bulkConsole := commandrequests.NewBulkHTTPHandlers(s.bulkCommandHTTPScope)
	commandRequests := commandrequests.NewHTTPHandlers(s.commandRequestHTTPScope)
	connectorApprovals := connectorActionApprovalHandlers{s}
	connectorActions := connectorActionHandlers{s}
	historyHandlers := historyhttp.New(s.historyHTTPScope)

	s.mux.HandleFunc("POST /api/console/bulk-exec", bulkConsole.Run)
	s.mux.HandleFunc("GET /api/console/sessions", console.List)
	s.mux.HandleFunc("POST /api/console/sessions", console.Create)
	s.mux.HandleFunc("GET /api/console/sessions/{id}", console.Get)
	s.mux.HandleFunc("POST /api/console/sessions/{id}/input", console.Input)
	s.mux.HandleFunc("POST /api/console/sessions/{id}/close", console.Close)
	s.mux.HandleFunc("GET /api/console/sessions/{id}/attach", console.Attach)
	s.mux.HandleFunc("POST /api/console/runtime-surfaces/{id}/restart", console.Restart)
	s.mux.HandleFunc("POST /api/console/targets/{id}/restart", console.Restart)
	s.mux.HandleFunc("GET /api/console/command-requests/{id}", commandRequests.Get)
	s.mux.HandleFunc("GET /api/connector-action-approvals", connectorApprovals.listConnectorActionApprovals)
	s.mux.HandleFunc("GET /api/connector-action-approvals/{id}", connectorApprovals.getConnectorActionApproval)
	s.mux.HandleFunc("POST /api/connector-action-approvals/{id}/run", connectorApprovals.runConnectorActionApproval)
	s.mux.HandleFunc("POST /api/connector-action-approvals/{id}/decline", connectorApprovals.declineConnectorActionApproval)
	s.mux.HandleFunc("POST /api/connector-actions/local-run", connectorActions.runLocalConnectorAction)
	s.mux.HandleFunc("GET /api/history/targets", historyHandlers.ListTargetFacets)
	s.mux.HandleFunc("GET /api/history", historyHandlers.ListEntries)
	s.mux.HandleFunc("GET /api/history/{id}", historyHandlers.GetEntry)
	s.mux.HandleFunc("POST /api/history/{id}/labels", historyHandlers.AttachEntryLabel)
	s.mux.HandleFunc("DELETE /api/history/{id}/labels/{label_id}", historyHandlers.DetachEntryLabel)
	s.mux.HandleFunc("GET /api/history-labels", historyHandlers.ListLabels)
	s.mux.HandleFunc("POST /api/history-labels", historyHandlers.CreateLabel)
	s.mux.HandleFunc("DELETE /api/history-labels/{id}", historyHandlers.DeleteLabel)
}

func (s *Server) registerProjectAndVaultRoutes() {
	projects := projectstore.NewHTTPHandlers(s.projectsHTTPScope)
	vaultItems := projectvault.NewHTTPHandlers(s.projectVaultHTTPScope)
	vaultApprovals := vaultrequests.NewHTTPHandlers(s.vaultRequestHTTPScope)

	s.mux.HandleFunc("GET /api/projects", projects.List)
	s.mux.HandleFunc("POST /api/projects", projects.Create)
	s.mux.HandleFunc("PUT /api/projects/{id}", projects.Update)
	s.mux.HandleFunc("DELETE /api/projects/{id}", projects.Archive)
	s.mux.HandleFunc("GET /api/vault-items", vaultItems.ListItems)
	s.mux.HandleFunc("POST /api/vault-items", vaultItems.CreateItem)
	s.mux.HandleFunc("GET /api/vault-items/{id}", vaultItems.GetItem)
	s.mux.HandleFunc("PUT /api/vault-items/{id}", vaultItems.UpdateItem)
	s.mux.HandleFunc("POST /api/vault-items/{id}/generate-preview", vaultItems.GenerateItemPreview)
	s.mux.HandleFunc("POST /api/vault-items/{id}/value", vaultItems.ReplaceItemValue)
	s.mux.HandleFunc("POST /api/vault-items/{id}/reveal", vaultItems.RevealItem)
	s.mux.HandleFunc("POST /api/vault-items/{id}/delete", vaultItems.DeleteItem)
	s.mux.HandleFunc("GET /api/vault-default-bindings", vaultItems.ListDefaultBindings)
	s.mux.HandleFunc("PUT /api/vault-default-bindings", vaultItems.SaveDefaultBinding)
	s.mux.HandleFunc("POST /api/vault-default-bindings/{id}/delete", vaultItems.DeleteDefaultBinding)
	s.mux.HandleFunc("GET /api/vault-session-options", vaultItems.SessionOptions)
	s.mux.HandleFunc("GET /api/vault-action-approvals", vaultApprovals.List)
	s.mux.HandleFunc("POST /api/vault-action-approvals/{id}/run", vaultApprovals.Run)
	s.mux.HandleFunc("POST /api/vault-action-approvals/{id}/decline", vaultApprovals.Decline)
}

func (s *Server) registerTransferRoutes() {
	fileTransfers := s.fileTransferHTTPHandlers()

	registerFileTransferRoutes(s.mux, fileTransfers)
}

func registerFileTransferRoutes(mux *http.ServeMux, handlers *filetransferhttp.Handlers) {
	mux.HandleFunc("GET /api/file-transfers", handlers.ListFileTransfers)
	mux.HandleFunc("GET /api/file-transfers/{id}", handlers.GetFileTransfer)
	mux.HandleFunc("GET /api/file-transfers/{id}/download", handlers.DownloadTransferredFile)
	mux.HandleFunc("POST /api/file-transfers/{id}/cancel", handlers.CancelFileTransfer)
	mux.HandleFunc("GET /api/file-transfer-batches", handlers.ListFileTransferBatches)
	mux.HandleFunc("GET /api/file-transfer-batches/{id}", handlers.GetFileTransferBatch)
	mux.HandleFunc("GET /api/file-transfer-batches/{id}/download", handlers.DownloadFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/pause", handlers.PauseFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/resume", handlers.ResumeFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/cancel", handlers.CancelFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/queue", handlers.UpdateFileTransferBatchQueue)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/approve", handlers.ApproveFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/decline", handlers.DeclineFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfers/browse", handlers.BrowseRemoteFiles)
	mux.HandleFunc("POST /api/file-transfers/expand", handlers.ExpandRemoteFiles)
	mux.HandleFunc("POST /api/file-transfers/upload", handlers.StartUpload)
	mux.HandleFunc("POST /api/file-transfers/upload-batch", handlers.StartUploadBatch)
	mux.HandleFunc("POST /api/file-transfers/download", handlers.StartDownload)
	mux.HandleFunc("POST /api/file-transfers/download-batch", handlers.StartDownloadBatch)
}

func (s *Server) registerConnectorRoutes() {
	queries := connectormanagement.NewHTTPHandlers(s.connectorManagementScope)
	hostPing := connectormanagement.NewHostPingHTTPHandler(s.connectorPingHTTPScope)
	targetMutations := connectormanagement.NewTargetMutationHTTPHandler(s.connectorTargetMutationHTTPScope)
	profileMutations := connectormanagement.NewProfileMutationHTTPHandler(s.connectorProfileMutationHTTPScope)
	profileDeletion := connectormanagement.NewProfileDeletionHTTPHandler(s.connectorProfileDeletionHTTPScope)
	connectorTargets := connectorTargetHandlers{s}

	s.mux.HandleFunc("GET /api/connectors", queries.ListConnectors)
	s.mux.HandleFunc("GET /api/connectors/{kind}", queries.GetConnector)
	s.mux.HandleFunc("GET /api/targets", queries.ListTargetProfiles)
	s.mux.HandleFunc("GET /api/connector-targets", queries.ListTargets)
	s.mux.HandleFunc("GET /api/connector-targets/inventory", queries.ListTargetInventory)
	s.mux.HandleFunc("POST /api/connector-targets/with-profile", connectorTargets.createConnectorTargetWithProfile)
	s.mux.HandleFunc("POST /api/connector-targets", targetMutations.Create)
	s.mux.HandleFunc("POST /api/connector-targets/ping", hostPing.Ping)
	s.mux.HandleFunc("POST /api/connector-targets/test", connectorTargets.testConnectorTargetDraft)
	s.mux.HandleFunc("GET /api/connector-targets/{id}", queries.GetTarget)
	s.mux.HandleFunc("PUT /api/connector-targets/{id}/with-profile/{profile_id}", connectorTargets.updateConnectorTargetWithProfile)
	s.mux.HandleFunc("PUT /api/connector-targets/{id}", targetMutations.Update)
	s.mux.HandleFunc("DELETE /api/connector-targets/{id}", connectorTargets.deleteConnectorTarget)
	s.mux.HandleFunc("GET /api/connector-targets/{id}/profiles", queries.ListCredentialProfiles)
	s.mux.HandleFunc("POST /api/connector-targets/{id}/profiles", profileMutations.Create)
	s.mux.HandleFunc("POST /api/connector-targets/{id}/profiles/{profile_id}/provision", connectorTargets.provisionConnectorCredentialProfile)
	s.mux.HandleFunc("GET /api/connector-targets/{id}/profiles/{profile_id}/backup", connectorTargets.downloadConnectorProfileBackup)
	s.mux.HandleFunc("POST /api/connector-targets/{id}/profiles/{profile_id}/restore", connectorTargets.restoreConnectorProfileBackup)
	s.mux.HandleFunc("PUT /api/connector-targets/{id}/profiles/{profile_id}", profileMutations.Update)
	s.mux.HandleFunc("DELETE /api/connector-targets/{id}/profiles/{profile_id}", profileDeletion.Delete)
	s.mux.HandleFunc("POST /api/connector-targets/{id}/profiles/{profile_id}/test", connectorTargets.testConnectorCredentialProfile)
	s.mux.HandleFunc("GET /api/connector-targets/{id}/profiles/{profile_id}/actions", queries.ListCredentialProfileActions)
}

func (s *Server) registerMessageAndAuditRoutes() {
	messages := messagequeue.NewHTTPHandlers(s.messageQueueScope)
	audit := observability.NewHTTPHandlers(s.auditHTTPScope)

	s.mux.HandleFunc("GET /api/messages", messages.List)
	s.mux.HandleFunc("POST /api/messages", messages.Create)
	s.mux.HandleFunc("POST /api/messages/read", messages.MarkRead)
	s.mux.HandleFunc("GET /api/audit-logs", audit.List)
	s.mux.HandleFunc("GET /api/audit-logs/{id}", audit.Get)
}

func (s *Server) registerMCPRoutes() {
	mcp := mcpHandlers{s}

	s.mux.HandleFunc("GET /api/settings/mcp-runtime", mcp.getMCPRuntime)
	s.mux.HandleFunc("PUT /api/settings/mcp-runtime", mcp.updateMCPRuntime)
	s.mux.HandleFunc("GET /api/mcp/connector-targets", mcp.mcpListConnectorTargets)
	s.mux.HandleFunc("GET /api/mcp/connector-help", mcp.mcpGetConnectorHelp)
	s.mux.HandleFunc("GET /api/mcp/connector-actions", mcp.mcpGetConnectorActions)
	s.mux.HandleFunc("POST /api/mcp/connector-actions/call", mcp.mcpCallConnectorAction)
	s.mux.HandleFunc("GET /api/mcp/connector-action-requests/{id}", mcp.mcpGetConnectorActionRequest)
	s.mux.HandleFunc("GET /api/mcp/vault-items", mcp.mcpListVaultItems)
	s.mux.HandleFunc("POST /api/mcp/vault-actions/call", mcp.mcpCallVaultAction)
	s.mux.HandleFunc("GET /api/mcp/vault-action-requests/{id}", mcp.mcpGetVaultActionRequest)
	s.mux.HandleFunc("POST /api/mcp/vault-action-requests/{id}/cancel", mcp.mcpCancelVaultActionRequest)
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
		"audit":   s.auditHealthSnapshot(r.Context()),
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
