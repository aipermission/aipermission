package api

//go:generate go run ../../cmd/openapi -routes ../gatewayroutes/routes.go -output ../../../docs/api/openapi.json

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	consolehttp "github.com/aipermission/aipermission/backend/internal/console/httpapi"
	"github.com/aipermission/aipermission/backend/internal/gatewayroutes"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type connectorTargetHandlers struct{ *Server }
type mcpHandlers struct{ *Server }
type diagnosticsHandlers struct{ *Server }

func (s *Server) routes() {
	observation := s.observation.HTTPHandlers(s.activeRuntimeOrLocked)
	backup := s.backupApplication().HTTPHandlers()
	connectorManagement := s.connectorManagementApplication()
	connectorQueries := connectormanagement.NewHTTPHandlers(connectorManagement.QueryScope)
	connectorTargets := connectorTargetHandlers{s}
	mcp := mcpHandlers{s}

	gatewayroutes.Register(s.mux, gatewayroutes.Dependencies{
		Health: gatewayroutes.Health, Status: s.status, Diagnostics: (diagnosticsHandlers{s}).download,
		Security:    securitypolicy.NewHTTPHandlers(s.securityPolicyHTTPScope),
		Retention:   observation.Retention,
		Maintenance: consolehttp.NewMaintenanceHTTPHandlers(s.maintenanceConsoleHTTPScope),
		Workspaces:  s.workspaceLifecycleHTTPHandlers(),

		Credentials:     connectorManagement.CredentialResources(s.connectorCredentialResourceDependencies()),
		TokenAccess:     accesscontrol.NewHTTPHandlers(s.accessControlScope),
		TargetOperation: connectorTargets.runConnectorTargetOperation,

		Backup: gatewayroutes.Backup{
			Download: backup.Download, Import: backup.Import,
			RestoreRemote: backup.RestoreRemote, RestoreProvider: backup.RestoreProvider,
		},
		TransientBackup: backup.Transient, BackupProviders: backup.Providers,

		Console:            connectorapi.NewLiveConsoleHTTPHandlers(s.consoleSessionHTTPScope),
		BulkConsole:        commandrequests.NewBulkHTTPHandlers(s.bulkCommandHTTPScope),
		CommandRequests:    commandrequests.NewHTTPHandlers(s.commandRequestHTTPScope),
		ConnectorApprovals: connectorapproval.NewHTTPHandlers(s.connectorApprovalHTTPScope),
		LocalActions:       s.localConnectorActionHTTP(),
		History:            observation.History,

		Projects:       projectstore.NewHTTPHandlers(s.projectsHTTPScope),
		VaultItems:     projectvault.NewHTTPHandlers(s.projectVaultHTTPScope),
		VaultApprovals: vaultrequests.NewHTTPHandlers(s.vaultRequestHTTPScope),
		FileTransfers:  s.fileTransferHTTPHandlers(),

		ConnectorQueries: connectorQueries,
		CombinedMutations: connectormanagement.NewCombinedMutationHTTPHandler(
			connectorManagement.CombinedMutationScope,
		),
		TargetMutations: connectormanagement.NewTargetMutationHTTPHandler(connectorManagement.TargetMutationScope),
		HostPing:        connectormanagement.NewHostPingHTTPHandler(connectorManagement.HostPingScope),
		TestTarget:      connectorTargets.testConnectorTargetDraft,
		DeleteTarget:    connectorTargets.deleteConnectorTarget,
		ProfileMutations: connectormanagement.NewProfileMutationHTTPHandler(
			connectorManagement.ProfileMutationScope,
		),
		ProfileProvision: connectormanagement.NewProvisioningHTTPHandler(connectorManagement.ProvisioningScope),
		ProfileBackup:    connectormanagement.NewProfileBackupHTTPHandler(connectorManagement.ProfileBackupScope),
		ProfileDelete:    connectormanagement.NewProfileDeletionHTTPHandler(connectorManagement.ProfileDeletionScope),
		ProfileTest:      connectormanagement.NewProfileTestingHTTPHandler(connectorManagement.ProfileTestingScope),

		Messages: messagequeue.NewHTTPHandlers(s.messageQueueScope), Audit: observation.Audit,
		MCPRuntime:          runtimecontrol.NewMCPHTTPHandlers(s.mcpRuntimeHTTPScope),
		MCPConnectorReads:   mcpconnector.NewHTTPHandlers(mcp.mcpConnectorReadScope),
		MCPConnectorActions: mcpconnector.NewActionHTTPHandlers(mcp.mcpConnectorActionScope),
		MCPVaultActions:     vaultrequests.NewMCPHTTPHandlers(mcp.mcpVaultScope),
		RegisterAdapterRoutes: func(mux *http.ServeMux) {
			registerConnectorAdapterRoutes(mux, s)
		},
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "aipermission", "status": "running",
		"audit":  s.observation.HealthSnapshot(r.Context(), s.activeRuntime()),
		"config": s.config.PublicStatusMinimal(),
		"features": []string{
			"local-docker-runtime", "react-dashboard", "sqlcipher-sqlite-storage", "database-unlock-screen",
			"encrypted-vault", "credential-management", "connector-target-management", "api-token-management",
			"connector-action-permissions", "persistent-console-sessions", "local-node-mcp-bridge",
			"encrypted-backup-restore", "mcp-gateway",
		},
	})
}
