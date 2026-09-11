package api

//go:generate go run ../../cmd/openapi -routes ../gatewayinfrastructure/routes.go -output ../../../docs/api/openapi.json

import (
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

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
	mcp := mcpHandlers{s}
	accessHTTP := s.access.HTTPHandlers(gatewayaccess.HTTPScopeProviders{
		Security: s.securityPolicyHTTPScope, TokenAccess: s.accessControlScope,
		BulkConsole: s.bulkCommandHTTPScope, CommandRequests: s.commandRequestHTTPScope,
		MCPRuntime: s.mcpRuntimeHTTPScope, MCPConnectorReads: mcp.mcpConnectorReadScope,
		MCPConnectorActions: mcp.mcpConnectorActionScope,
	})
	vaultHTTP := s.vaultApplication().HTTPHandlers(gatewayvault.HTTPDependencies{
		Projects: s.projectsHTTPScope, ProjectVault: s.projectVaultHTTPScope,
		VaultApprovals: s.vaultRequestHTTPScope, MCPVault: mcp.mcpVaultScope,
	})

	gatewayinfra.Register(s.mux, gatewayinfra.Dependencies{
		Health: gatewayinfra.Health, Status: s.status, Diagnostics: (diagnosticsHandlers{s}).download,
		Security:    accessHTTP.Security,
		Retention:   observation.Retention,
		Maintenance: gatewayoperations.NewMaintenanceHTTPHandlers(s.maintenanceConsoleHTTPScope),
		Workspaces:  s.workspaceLifecycleHTTPHandlers(),

		Credentials:     connectorManagement.CredentialResources(s.connectorCredentialResourceDependencies()),
		TokenAccess:     accessHTTP.TokenAccess,
		TargetOperation: connectorHTTP.TargetOperation.Run,

		Backup: gatewayinfra.Backup{
			Download: backup.Download, Import: backup.Import,
			RestoreRemote: backup.RestoreRemote, RestoreProvider: backup.RestoreProvider,
		},
		TransientBackup: backup.Transient, BackupProviders: backup.Providers,

		Console:            connectorapi.NewLiveConsoleHTTPHandlers(s.consoleSessionHTTPScope),
		BulkConsole:        accessHTTP.BulkConsole,
		CommandRequests:    accessHTTP.CommandRequests,
		ConnectorApprovals: connectorHTTP.Approvals,
		LocalActions:       s.localConnectorActionHTTP(),
		History:            observation.History,

		Projects: vaultHTTP.Projects, VaultItems: vaultHTTP.ProjectVault, VaultApprovals: vaultHTTP.VaultApprovals,
		FileTransfers: s.fileTransferHTTPHandlers(),

		ConnectorQueries:  connectorHTTP.Queries,
		CombinedMutations: connectorHTTP.CombinedMutations,
		TargetMutations:   connectorHTTP.TargetMutations,
		HostPing:          connectorHTTP.HostPing,
		TestTarget:        connectorHTTP.TargetDraft.Test,
		DeleteTarget:      connectorHTTP.TargetDelete.Delete,
		ProfileMutations:  connectorHTTP.ProfileMutations,
		ProfileProvision:  connectorHTTP.ProfileProvision,
		ProfileBackup:     connectorHTTP.ProfileBackup,
		ProfileDelete:     connectorHTTP.ProfileDelete,
		ProfileTest:       connectorHTTP.ProfileTest,

		Messages: gatewayoperations.NewMessageHTTPHandlers(s.messageQueueScope), Audit: observation.Audit,
		MCPRuntime:          accessHTTP.MCPRuntime,
		MCPConnectorReads:   accessHTTP.MCPConnectorReads,
		MCPConnectorActions: accessHTTP.MCPConnectorActions,
		MCPVaultActions:     vaultHTTP.MCPVault,
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
