package api

//go:generate go run ../../cmd/openapi -routes ../gatewayinfrastructure/routes.go -output ../../../docs/api/openapi.json

import (
	"net/http"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type connectorTargetHandlers struct{ *Server }
type mcpHandlers struct{ *Server }
type diagnosticsHandlers struct{ *Server }

func (s *Server) routes() {
	observation := s.observation.HTTPHandlers(func(w http.ResponseWriter) (gatewayoperations.ObservationRuntime, bool) {
		runtime, ok := s.activeRuntimeOrLocked(w)
		return observationRuntime(runtime), ok
	})
	backup := s.backupApplication().HTTPHandlers()
	connectorManagement := s.connectorManagementApplication()
	connectorHTTP := connectorManagement.HTTPHandlers()
	connectorTargets := connectorTargetHandlers{s}
	mcp := mcpHandlers{s}

	gatewayinfra.Register(s.mux, gatewayinfra.Dependencies{
		Health: gatewayinfra.Health, Status: s.status, Diagnostics: (diagnosticsHandlers{s}).download,
		Security:    s.access.NewSecurityHTTPHandlers(s.securityPolicyHTTPScope),
		Retention:   observation.Retention,
		Maintenance: gatewayoperations.NewMaintenanceHTTPHandlers(s.maintenanceConsoleHTTPScope),
		Workspaces:  s.workspaceLifecycleHTTPHandlers(),

		Credentials:     connectorManagement.CredentialResources(s.connectorCredentialResourceDependencies()),
		TokenAccess:     s.access.NewAccessHTTPHandlers(s.accessControlScope),
		TargetOperation: connectorTargets.runConnectorTargetOperation,

		Backup: gatewayinfra.Backup{
			Download: backup.Download, Import: backup.Import,
			RestoreRemote: backup.RestoreRemote, RestoreProvider: backup.RestoreProvider,
		},
		TransientBackup: backup.Transient, BackupProviders: backup.Providers,

		Console:            connectorapi.NewLiveConsoleHTTPHandlers(s.consoleSessionHTTPScope),
		BulkConsole:        s.access.NewBulkHTTPHandlers(s.bulkCommandHTTPScope),
		CommandRequests:    s.access.NewCommandHTTPHandlers(s.commandRequestHTTPScope),
		ConnectorApprovals: connectorHTTP.Approvals,
		LocalActions:       s.localConnectorActionHTTP(),
		History:            observation.History,

		Projects:       gatewayvault.NewProjectsHTTPHandlers(s.projectsHTTPScope),
		VaultItems:     gatewayvault.NewProjectVaultHTTPHandlers(s.projectVaultHTTPScope),
		VaultApprovals: gatewayvault.NewVaultApprovalHTTPHandlers(s.vaultRequestHTTPScope),
		FileTransfers:  s.fileTransferHTTPHandlers(),

		ConnectorQueries:  connectorHTTP.Queries,
		CombinedMutations: connectorHTTP.CombinedMutations,
		TargetMutations:   connectorHTTP.TargetMutations,
		HostPing:          connectorHTTP.HostPing,
		TestTarget:        connectorTargets.testConnectorTargetDraft,
		DeleteTarget:      connectorTargets.deleteConnectorTarget,
		ProfileMutations:  connectorHTTP.ProfileMutations,
		ProfileProvision:  connectorHTTP.ProfileProvision,
		ProfileBackup:     connectorHTTP.ProfileBackup,
		ProfileDelete:     connectorHTTP.ProfileDelete,
		ProfileTest:       connectorHTTP.ProfileTest,

		Messages: gatewayoperations.NewMessageHTTPHandlers(s.messageQueueScope), Audit: observation.Audit,
		MCPRuntime:          s.access.NewMCPRuntimeHTTPHandlers(s.mcpRuntimeHTTPScope),
		MCPConnectorReads:   s.access.NewMCPReadHTTPHandlers(mcp.mcpConnectorReadScope),
		MCPConnectorActions: s.access.NewMCPActionHTTPHandlers(mcp.mcpConnectorActionScope),
		MCPVaultActions:     gatewayvault.NewVaultMCPHTTPHandlers(mcp.mcpVaultScope),
		RegisterAdapterRoutes: func(mux *http.ServeMux) {
			registerConnectorAdapterRoutes(mux, s)
		},
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "aipermission", "status": "running",
		"audit":  s.observation.HealthSnapshot(r.Context(), observationRuntime(s.activeRuntime())),
		"config": s.config.PublicStatusMinimal(),
		"features": []string{
			"local-docker-runtime", "react-dashboard", "sqlcipher-sqlite-storage", "database-unlock-screen",
			"encrypted-vault", "credential-management", "connector-target-management", "api-token-management",
			"connector-action-permissions", "persistent-console-sessions", "local-node-mcp-bridge",
			"encrypted-backup-restore", "mcp-gateway",
		},
	})
}
