package api

//go:generate go run ../../cmd/openapi -routes ../gatewayroutes/routes.go -output ../../../docs/api/openapi.json

import (
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
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
	connectorQueries := connectormgmt.NewHTTPHandlers(connectorManagement.QueryScope)
	connectorTargets := connectorTargetHandlers{s}
	mcp := mcpHandlers{s}

	gatewayinfra.RegisterRoutes(s.mux, gatewayinfra.RouteDependencies{
		Health: gatewayinfra.Health, Status: s.status, Diagnostics: (diagnosticsHandlers{s}).download,
		Security:    gatewayaccess.NewSecurityHTTPHandlers(s.securityPolicyHTTPScope),
		Retention:   observation.Retention,
		Maintenance: gatewayoperations.NewMaintenanceHTTPHandlers(s.maintenanceConsoleHTTPScope),
		Workspaces:  s.workspaceLifecycleHTTPHandlers(),

		Credentials:     connectorManagement.CredentialResources(s.connectorCredentialResourceDependencies()),
		TokenAccess:     gatewayaccess.NewAccessHTTPHandlers(s.accessControlScope),
		TargetOperation: connectorTargets.runConnectorTargetOperation,

		Backup: gatewayinfra.RouteBackup{
			Download: backup.Download, Import: backup.Import,
			RestoreRemote: backup.RestoreRemote, RestoreProvider: backup.RestoreProvider,
		},
		TransientBackup: backup.Transient, BackupProviders: backup.Providers,

		Console:            connectorapi.NewLiveConsoleHTTPHandlers(s.consoleSessionHTTPScope),
		BulkConsole:        gatewayaccess.NewBulkHTTPHandlers(s.bulkCommandHTTPScope),
		CommandRequests:    gatewayaccess.NewCommandHTTPHandlers(s.commandRequestHTTPScope),
		ConnectorApprovals: connectormgmt.NewConnectorApprovalHTTPHandlers(s.connectorApprovalHTTPScope),
		LocalActions:       s.localConnectorActionHTTP(),
		History:            observation.History,

		Projects:       gatewayvault.NewProjectsHTTPHandlers(s.projectsHTTPScope),
		VaultItems:     gatewayvault.NewProjectVaultHTTPHandlers(s.projectVaultHTTPScope),
		VaultApprovals: gatewayvault.NewVaultApprovalHTTPHandlers(s.vaultRequestHTTPScope),
		FileTransfers:  s.fileTransferHTTPHandlers(),

		ConnectorQueries: connectorQueries,
		CombinedMutations: connectormgmt.NewCombinedMutationHTTPHandler(
			connectorManagement.CombinedMutationScope,
		),
		TargetMutations: connectormgmt.NewTargetMutationHTTPHandler(connectorManagement.TargetMutationScope),
		HostPing:        connectormgmt.NewHostPingHTTPHandler(connectorManagement.HostPingScope),
		TestTarget:      connectorTargets.testConnectorTargetDraft,
		DeleteTarget:    connectorTargets.deleteConnectorTarget,
		ProfileMutations: connectormgmt.NewProfileMutationHTTPHandler(
			connectorManagement.ProfileMutationScope,
		),
		ProfileProvision: connectormgmt.NewProvisioningHTTPHandler(connectorManagement.ProvisioningScope),
		ProfileBackup:    connectormgmt.NewProfileBackupHTTPHandler(connectorManagement.ProfileBackupScope),
		ProfileDelete:    connectormgmt.NewProfileDeletionHTTPHandler(connectorManagement.ProfileDeletionScope),
		ProfileTest:      connectormgmt.NewProfileTestingHTTPHandler(connectorManagement.ProfileTestingScope),

		Messages: gatewayoperations.NewMessageHTTPHandlers(s.messageQueueScope), Audit: observation.Audit,
		MCPRuntime:          gatewayaccess.NewMCPRuntimeHTTPHandlers(s.mcpRuntimeHTTPScope),
		MCPConnectorReads:   gatewayaccess.NewMCPReadHTTPHandlers(mcp.mcpConnectorReadScope),
		MCPConnectorActions: gatewayaccess.NewMCPActionHTTPHandlers(mcp.mcpConnectorActionScope),
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
