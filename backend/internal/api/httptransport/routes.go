// Package httptransport owns the local HTTP route contract.
package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	transportcontract "github.com/aipermission/aipermission/backend/internal/httptransport"
)

type Handler func(http.ResponseWriter, *http.Request)

type AdapterRoute struct {
	Kind    string
	Method  string
	Path    string
	Policy  AdapterRoutePolicy
	Handler Handler
}

type AdapterRoutePolicy string

const (
	AdapterRoutePolicyUIRead     AdapterRoutePolicy = "ui_read"
	AdapterRoutePolicyUIMutation AdapterRoutePolicy = "ui_mutation"
)

func Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
}

type Security interface {
	GetSettings(http.ResponseWriter, *http.Request)
	UpdateSettings(http.ResponseWriter, *http.Request)
	ListRules(http.ResponseWriter, *http.Request)
	CreateRule(http.ResponseWriter, *http.Request)
	UpdateRule(http.ResponseWriter, *http.Request)
	DeleteRule(http.ResponseWriter, *http.Request)
}
type Retention interface {
	Get(http.ResponseWriter, *http.Request)
	Update(http.ResponseWriter, *http.Request)
	Purge(http.ResponseWriter, *http.Request)
}
type Maintenance interface {
	Status(http.ResponseWriter, *http.Request)
	Open(http.ResponseWriter, *http.Request)
	Attach(http.ResponseWriter, *http.Request)
	Close(http.ResponseWriter, *http.Request)
}
type Workspaces interface {
	Status(http.ResponseWriter, *http.Request)
	Setup(http.ResponseWriter, *http.Request)
	Unlock(http.ResponseWriter, *http.Request)
	Lock(http.ResponseWriter, *http.Request)
	Rename(http.ResponseWriter, *http.Request)
	Delete(http.ResponseWriter, *http.Request)
	DeleteLocked(http.ResponseWriter, *http.Request)
	Switch(http.ResponseWriter, *http.Request)
	ChangePassword(http.ResponseWriter, *http.Request)
}
type Credentials interface {
	List(http.ResponseWriter, *http.Request)
	Create(http.ResponseWriter, *http.Request)
	Import(http.ResponseWriter, *http.Request)
	Get(http.ResponseWriter, *http.Request)
	Update(http.ResponseWriter, *http.Request)
	Delete(http.ResponseWriter, *http.Request)
}
type TokenAccess interface {
	ListTokens(http.ResponseWriter, *http.Request)
	CreateToken(http.ResponseWriter, *http.Request)
	RevokeToken(http.ResponseWriter, *http.Request)
	ListConnectorPermissions(http.ResponseWriter, *http.Request)
	UpdateConnectorPermissions(http.ResponseWriter, *http.Request)
	ListProjectScopes(http.ResponseWriter, *http.Request)
	UpdateProjectScopes(http.ResponseWriter, *http.Request)
	ListProjectCapabilities(http.ResponseWriter, *http.Request)
	UpdateProjectCapabilities(http.ResponseWriter, *http.Request)
}
type Backup struct {
	Download        Handler
	Import          Handler
	RestoreRemote   Handler
	RestoreProvider Handler
}
type TransientBackup interface {
	List(http.ResponseWriter, *http.Request)
}
type BackupProviders interface {
	ProviderCatalog(http.ResponseWriter, *http.Request)
	ListProviders(http.ResponseWriter, *http.Request)
	BackupFreshness(http.ResponseWriter, *http.Request)
	CreateProvider(http.ResponseWriter, *http.Request)
	UpdateProvider(http.ResponseWriter, *http.Request)
	DeleteProvider(http.ResponseWriter, *http.Request)
	EnableProvider(http.ResponseWriter, *http.Request)
	TestProvider(http.ResponseWriter, *http.Request)
	ListProviderRecords(http.ResponseWriter, *http.Request)
	UploadProviderBackup(http.ResponseWriter, *http.Request)
	PruneProviderBackups(http.ResponseWriter, *http.Request)
	BackupProviderStorage(http.ResponseWriter, *http.Request)
	BackupProviderRetention(http.ResponseWriter, *http.Request)
	PreviewBackupProviderRetention(http.ResponseWriter, *http.Request)
	UpdateBackupProviderRetention(http.ResponseWriter, *http.Request)
	DeleteProviderBackupRecords(http.ResponseWriter, *http.Request)
	DownloadProviderRecord(http.ResponseWriter, *http.Request)
}
type Console interface {
	List(http.ResponseWriter, *http.Request)
	Create(http.ResponseWriter, *http.Request)
	Get(http.ResponseWriter, *http.Request)
	Input(http.ResponseWriter, *http.Request)
	Close(http.ResponseWriter, *http.Request)
	Attach(http.ResponseWriter, *http.Request)
	Restart(http.ResponseWriter, *http.Request)
}
type BulkConsole interface {
	Run(http.ResponseWriter, *http.Request)
}
type CommandRequests interface {
	Get(http.ResponseWriter, *http.Request)
}
type Approvals interface {
	List(http.ResponseWriter, *http.Request)
	Get(http.ResponseWriter, *http.Request)
	Run(http.ResponseWriter, *http.Request)
	Decline(http.ResponseWriter, *http.Request)
}
type VaultApprovals interface {
	List(http.ResponseWriter, *http.Request)
	Run(http.ResponseWriter, *http.Request)
	Decline(http.ResponseWriter, *http.Request)
}
type LocalActions interface {
	Run(http.ResponseWriter, *http.Request)
}
type History interface {
	ListTargetFacets(http.ResponseWriter, *http.Request)
	ListEntries(http.ResponseWriter, *http.Request)
	GetEntry(http.ResponseWriter, *http.Request)
	AttachEntryLabel(http.ResponseWriter, *http.Request)
	DetachEntryLabel(http.ResponseWriter, *http.Request)
	ListLabels(http.ResponseWriter, *http.Request)
	CreateLabel(http.ResponseWriter, *http.Request)
	DeleteLabel(http.ResponseWriter, *http.Request)
}
type Projects interface {
	List(http.ResponseWriter, *http.Request)
	Create(http.ResponseWriter, *http.Request)
	Update(http.ResponseWriter, *http.Request)
	Archive(http.ResponseWriter, *http.Request)
}
type VaultItems interface {
	ListItems(http.ResponseWriter, *http.Request)
	CreateItem(http.ResponseWriter, *http.Request)
	GetItem(http.ResponseWriter, *http.Request)
	UpdateItem(http.ResponseWriter, *http.Request)
	GenerateItemPreview(http.ResponseWriter, *http.Request)
	ReplaceItemValue(http.ResponseWriter, *http.Request)
	RevealItem(http.ResponseWriter, *http.Request)
	DeleteItem(http.ResponseWriter, *http.Request)
	ListDefaultBindings(http.ResponseWriter, *http.Request)
	SaveDefaultBinding(http.ResponseWriter, *http.Request)
	DeleteDefaultBinding(http.ResponseWriter, *http.Request)
	SessionOptions(http.ResponseWriter, *http.Request)
}
type FileTransfers interface {
	ListFileTransfers(http.ResponseWriter, *http.Request)
	GetFileTransfer(http.ResponseWriter, *http.Request)
	DownloadTransferredFile(http.ResponseWriter, *http.Request)
	CancelFileTransfer(http.ResponseWriter, *http.Request)
	ListFileTransferBatches(http.ResponseWriter, *http.Request)
	GetFileTransferBatch(http.ResponseWriter, *http.Request)
	DownloadFileTransferBatch(http.ResponseWriter, *http.Request)
	PauseFileTransferBatch(http.ResponseWriter, *http.Request)
	ResumeFileTransferBatch(http.ResponseWriter, *http.Request)
	CancelFileTransferBatch(http.ResponseWriter, *http.Request)
	UpdateFileTransferBatchQueue(http.ResponseWriter, *http.Request)
	ApproveFileTransferBatch(http.ResponseWriter, *http.Request)
	DeclineFileTransferBatch(http.ResponseWriter, *http.Request)
	BrowseRemoteFiles(http.ResponseWriter, *http.Request)
	ExpandRemoteFiles(http.ResponseWriter, *http.Request)
	StartUpload(http.ResponseWriter, *http.Request)
	StartUploadBatch(http.ResponseWriter, *http.Request)
	StartDownload(http.ResponseWriter, *http.Request)
	StartDownloadBatch(http.ResponseWriter, *http.Request)
}
type ConnectorQueries interface {
	ListConnectors(http.ResponseWriter, *http.Request)
	GetConnector(http.ResponseWriter, *http.Request)
	ListTargetProfiles(http.ResponseWriter, *http.Request)
	ListTargets(http.ResponseWriter, *http.Request)
	ListTargetInventory(http.ResponseWriter, *http.Request)
	GetTarget(http.ResponseWriter, *http.Request)
	ListCredentialProfiles(http.ResponseWriter, *http.Request)
	ListCredentialProfileActions(http.ResponseWriter, *http.Request)
}
type CreateUpdate interface {
	Create(http.ResponseWriter, *http.Request)
	Update(http.ResponseWriter, *http.Request)
}
type CombinedCreateUpdate interface {
	Create(http.ResponseWriter, *http.Request)
	Update(http.ResponseWriter, *http.Request)
}
type HostPing interface {
	Ping(http.ResponseWriter, *http.Request)
}
type ProfileDelete interface {
	Delete(http.ResponseWriter, *http.Request)
}
type ProfileProvision interface {
	Provision(http.ResponseWriter, *http.Request)
}
type ProfileTest interface {
	Test(http.ResponseWriter, *http.Request)
}
type ProfileBackup interface {
	Download(http.ResponseWriter, *http.Request)
	Restore(http.ResponseWriter, *http.Request)
}
type Messages interface {
	List(http.ResponseWriter, *http.Request)
	Create(http.ResponseWriter, *http.Request)
	MarkRead(http.ResponseWriter, *http.Request)
}
type Audit interface {
	List(http.ResponseWriter, *http.Request)
	Get(http.ResponseWriter, *http.Request)
}
type MCPRuntime interface {
	Get(http.ResponseWriter, *http.Request)
	Update(http.ResponseWriter, *http.Request)
}
type MCPConnectorReads interface {
	ListTargets(http.ResponseWriter, *http.Request)
	GetHelp(http.ResponseWriter, *http.Request)
	GetActions(http.ResponseWriter, *http.Request)
}
type MCPConnectorActions interface {
	Call(http.ResponseWriter, *http.Request)
	GetRequest(http.ResponseWriter, *http.Request)
}
type MCPVaultActions interface {
	ListItems(http.ResponseWriter, *http.Request)
	Call(http.ResponseWriter, *http.Request)
	GetRequest(http.ResponseWriter, *http.Request)
	CancelRequest(http.ResponseWriter, *http.Request)
}

type Dependencies struct {
	Health, Status, Diagnostics Handler
	Security                    Security
	Retention                   Retention
	Maintenance                 Maintenance
	Workspaces                  Workspaces
	Credentials                 Credentials
	TokenAccess                 TokenAccess
	TargetOperation             Handler
	Backup                      Backup
	TransientBackup             TransientBackup
	BackupProviders             BackupProviders
	Console                     Console
	BulkConsole                 BulkConsole
	CommandRequests             CommandRequests
	ConnectorApprovals          Approvals
	LocalActions                LocalActions
	History                     History
	Projects                    Projects
	VaultItems                  VaultItems
	VaultApprovals              VaultApprovals
	FileTransfers               FileTransfers
	ConnectorQueries            ConnectorQueries
	CombinedMutations           CombinedCreateUpdate
	TargetMutations             CreateUpdate
	HostPing                    HostPing
	TestTarget, DeleteTarget    Handler
	ProfileMutations            CreateUpdate
	ProfileProvision            ProfileProvision
	ProfileBackup               ProfileBackup
	ProfileDelete               ProfileDelete
	ProfileTest                 ProfileTest
	Messages                    Messages
	Audit                       Audit
	MCPRuntime                  MCPRuntime
	MCPConnectorReads           MCPConnectorReads
	MCPConnectorActions         MCPConnectorActions
	MCPVaultActions             MCPVaultActions
	AdapterRoutes               []AdapterRoute
}

func Register(mux *http.ServeMux, d Dependencies) {
	mux.HandleFunc("GET /health", d.Health)
	mux.HandleFunc("GET /api/status", d.Status)
	mux.HandleFunc("GET /api/settings/security", d.Security.GetSettings)
	mux.HandleFunc("PUT /api/settings/security", d.Security.UpdateSettings)
	mux.HandleFunc("GET /api/settings/retention", d.Retention.Get)
	mux.HandleFunc("PUT /api/settings/retention", d.Retention.Update)
	mux.HandleFunc("POST /api/settings/retention/purge", d.Retention.Purge)
	mux.HandleFunc("GET /api/settings/redaction-rules", d.Security.ListRules)
	mux.HandleFunc("POST /api/settings/redaction-rules", d.Security.CreateRule)
	mux.HandleFunc("PUT /api/settings/redaction-rules/{id}", d.Security.UpdateRule)
	mux.HandleFunc("DELETE /api/settings/redaction-rules/{id}", d.Security.DeleteRule)
	mux.HandleFunc("GET /api/settings/maintenance-console/status", d.Maintenance.Status)
	mux.HandleFunc("POST /api/settings/maintenance-console/open", d.Maintenance.Open)
	mux.HandleFunc("GET /api/settings/maintenance-console/attach", d.Maintenance.Attach)
	mux.HandleFunc("POST /api/settings/maintenance-console/close", d.Maintenance.Close)
	mux.HandleFunc("GET /api/settings/diagnostics", d.Diagnostics)
	mux.HandleFunc("GET /api/unlock/status", d.Workspaces.Status)
	mux.HandleFunc("POST /api/unlock/setup", d.Workspaces.Setup)
	mux.HandleFunc("POST /api/unlock", d.Workspaces.Unlock)
	mux.HandleFunc("POST /api/lock", d.Workspaces.Lock)
	mux.HandleFunc("GET /api/connectors/{kind}/credentials", d.Credentials.List)
	mux.HandleFunc("POST /api/connectors/{kind}/credentials", d.Credentials.Create)
	mux.HandleFunc("POST /api/connectors/{kind}/credentials/import", d.Credentials.Import)
	mux.HandleFunc("GET /api/connectors/{kind}/credentials/{id}", d.Credentials.Get)
	mux.HandleFunc("PUT /api/connectors/{kind}/credentials/{id}", d.Credentials.Update)
	mux.HandleFunc("DELETE /api/connectors/{kind}/credentials/{id}", d.Credentials.Delete)
	mux.HandleFunc("POST /api/connector-targets/{id}/operations/{operation}", d.TargetOperation)
	mux.HandleFunc("GET /api/tokens", d.TokenAccess.ListTokens)
	mux.HandleFunc("POST /api/tokens", d.TokenAccess.CreateToken)
	mux.HandleFunc("POST /api/tokens/{id}/revoke", d.TokenAccess.RevokeToken)
	mux.HandleFunc("GET /api/tokens/{id}/connector-permissions", d.TokenAccess.ListConnectorPermissions)
	mux.HandleFunc("PUT /api/tokens/{id}/connector-permissions", d.TokenAccess.UpdateConnectorPermissions)
	mux.HandleFunc("GET /api/tokens/{id}/project-scopes", d.TokenAccess.ListProjectScopes)
	mux.HandleFunc("PUT /api/tokens/{id}/project-scopes", d.TokenAccess.UpdateProjectScopes)
	mux.HandleFunc("GET /api/tokens/{id}/project-capabilities", d.TokenAccess.ListProjectCapabilities)
	mux.HandleFunc("PUT /api/tokens/{id}/project-capabilities", d.TokenAccess.UpdateProjectCapabilities)
	mux.HandleFunc("GET /api/backup/download", d.Backup.Download)
	mux.HandleFunc("POST /api/backup/import", d.Backup.Import)
	mux.HandleFunc("POST /api/backup/remote/list", d.TransientBackup.List)
	mux.HandleFunc("POST /api/backup/remote/restore", d.Backup.RestoreRemote)
	mux.HandleFunc("GET /api/backup/providers/catalog", d.BackupProviders.ProviderCatalog)
	mux.HandleFunc("GET /api/backup/providers", d.BackupProviders.ListProviders)
	mux.HandleFunc("GET /api/backup/freshness", d.BackupProviders.BackupFreshness)
	mux.HandleFunc("POST /api/backup/providers", d.BackupProviders.CreateProvider)
	mux.HandleFunc("PUT /api/backup/providers/{id}", d.BackupProviders.UpdateProvider)
	mux.HandleFunc("DELETE /api/backup/providers/{id}", d.BackupProviders.DeleteProvider)
	mux.HandleFunc("POST /api/backup/providers/{id}/enable", d.BackupProviders.EnableProvider)
	mux.HandleFunc("POST /api/backup/providers/{id}/test", d.BackupProviders.TestProvider)
	mux.HandleFunc("GET /api/backup/providers/{id}/records", d.BackupProviders.ListProviderRecords)
	mux.HandleFunc("POST /api/backup/providers/{id}/upload", d.BackupProviders.UploadProviderBackup)
	mux.HandleFunc("POST /api/backup/providers/{id}/prune", d.BackupProviders.PruneProviderBackups)
	mux.HandleFunc("GET /api/backup/providers/{id}/storage", d.BackupProviders.BackupProviderStorage)
	mux.HandleFunc("GET /api/backup/providers/{id}/retention", d.BackupProviders.BackupProviderRetention)
	mux.HandleFunc("POST /api/backup/providers/{id}/retention/preview", d.BackupProviders.PreviewBackupProviderRetention)
	mux.HandleFunc("PUT /api/backup/providers/{id}/retention", d.BackupProviders.UpdateBackupProviderRetention)
	mux.HandleFunc("POST /api/backup/providers/{id}/records/delete", d.BackupProviders.DeleteProviderBackupRecords)
	mux.HandleFunc("GET /api/backup/providers/{id}/records/{record_id}/download", d.BackupProviders.DownloadProviderRecord)
	mux.HandleFunc("POST /api/backup/providers/{id}/records/{record_id}/restore", d.Backup.RestoreProvider)
	mux.HandleFunc("POST /api/databases/rename", d.Workspaces.Rename)
	mux.HandleFunc("POST /api/databases/delete", d.Workspaces.Delete)
	mux.HandleFunc("POST /api/databases/delete-locked", d.Workspaces.DeleteLocked)
	mux.HandleFunc("POST /api/databases/switch", d.Workspaces.Switch)
	mux.HandleFunc("POST /api/databases/change-password", d.Workspaces.ChangePassword)
	mux.HandleFunc("POST /api/console/bulk-exec", d.BulkConsole.Run)
	mux.HandleFunc("GET /api/console/sessions", d.Console.List)
	mux.HandleFunc("POST /api/console/sessions", d.Console.Create)
	mux.HandleFunc("GET /api/console/sessions/{id}", d.Console.Get)
	mux.HandleFunc("POST /api/console/sessions/{id}/input", d.Console.Input)
	mux.HandleFunc("POST /api/console/sessions/{id}/close", d.Console.Close)
	mux.HandleFunc("GET /api/console/sessions/{id}/attach", d.Console.Attach)
	mux.HandleFunc("POST /api/console/runtime-surfaces/{id}/restart", d.Console.Restart)
	mux.HandleFunc("POST /api/console/targets/{id}/restart", d.Console.Restart)
	mux.HandleFunc("GET /api/console/command-requests/{id}", d.CommandRequests.Get)
	mux.HandleFunc("GET /api/connector-action-approvals", d.ConnectorApprovals.List)
	mux.HandleFunc("GET /api/connector-action-approvals/{id}", d.ConnectorApprovals.Get)
	mux.HandleFunc("POST /api/connector-action-approvals/{id}/run", d.ConnectorApprovals.Run)
	mux.HandleFunc("POST /api/connector-action-approvals/{id}/decline", d.ConnectorApprovals.Decline)
	mux.HandleFunc("POST /api/connector-actions/local-run", d.LocalActions.Run)
	mux.HandleFunc("GET /api/history/targets", d.History.ListTargetFacets)
	mux.HandleFunc("GET /api/history", d.History.ListEntries)
	mux.HandleFunc("GET /api/history/{id}", d.History.GetEntry)
	mux.HandleFunc("POST /api/history/{id}/labels", d.History.AttachEntryLabel)
	mux.HandleFunc("DELETE /api/history/{id}/labels/{label_id}", d.History.DetachEntryLabel)
	mux.HandleFunc("GET /api/history-labels", d.History.ListLabels)
	mux.HandleFunc("POST /api/history-labels", d.History.CreateLabel)
	mux.HandleFunc("DELETE /api/history-labels/{id}", d.History.DeleteLabel)
	mux.HandleFunc("GET /api/projects", d.Projects.List)
	mux.HandleFunc("POST /api/projects", d.Projects.Create)
	mux.HandleFunc("PUT /api/projects/{id}", d.Projects.Update)
	mux.HandleFunc("DELETE /api/projects/{id}", d.Projects.Archive)
	mux.HandleFunc("GET /api/vault-items", d.VaultItems.ListItems)
	mux.HandleFunc("POST /api/vault-items", d.VaultItems.CreateItem)
	mux.HandleFunc("GET /api/vault-items/{id}", d.VaultItems.GetItem)
	mux.HandleFunc("PUT /api/vault-items/{id}", d.VaultItems.UpdateItem)
	mux.HandleFunc("POST /api/vault-items/{id}/generate-preview", d.VaultItems.GenerateItemPreview)
	mux.HandleFunc("POST /api/vault-items/{id}/value", d.VaultItems.ReplaceItemValue)
	mux.HandleFunc("POST /api/vault-items/{id}/reveal", d.VaultItems.RevealItem)
	mux.HandleFunc("POST /api/vault-items/{id}/delete", d.VaultItems.DeleteItem)
	mux.HandleFunc("GET /api/vault-default-bindings", d.VaultItems.ListDefaultBindings)
	mux.HandleFunc("PUT /api/vault-default-bindings", d.VaultItems.SaveDefaultBinding)
	mux.HandleFunc("POST /api/vault-default-bindings/{id}/delete", d.VaultItems.DeleteDefaultBinding)
	mux.HandleFunc("GET /api/vault-session-options", d.VaultItems.SessionOptions)
	mux.HandleFunc("GET /api/vault-action-approvals", d.VaultApprovals.List)
	mux.HandleFunc("POST /api/vault-action-approvals/{id}/run", d.VaultApprovals.Run)
	mux.HandleFunc("POST /api/vault-action-approvals/{id}/decline", d.VaultApprovals.Decline)
	registerTransfers(mux, d.FileTransfers)
	registerConnectors(mux, d)
	mux.HandleFunc("GET /api/messages", d.Messages.List)
	mux.HandleFunc("POST /api/messages", d.Messages.Create)
	mux.HandleFunc("POST /api/messages/read", d.Messages.MarkRead)
	mux.HandleFunc("GET /api/audit-logs", d.Audit.List)
	mux.HandleFunc("GET /api/audit-logs/{id}", d.Audit.Get)
	mux.HandleFunc("GET /api/settings/mcp-runtime", d.MCPRuntime.Get)
	mux.HandleFunc("PUT /api/settings/mcp-runtime", d.MCPRuntime.Update)
	mux.HandleFunc("GET /api/mcp/connector-targets", d.MCPConnectorReads.ListTargets)
	mux.HandleFunc("GET /api/mcp/connector-help", d.MCPConnectorReads.GetHelp)
	mux.HandleFunc("GET /api/mcp/connector-actions", d.MCPConnectorReads.GetActions)
	mux.HandleFunc("POST /api/mcp/connector-actions/call", d.MCPConnectorActions.Call)
	mux.HandleFunc("GET /api/mcp/connector-action-requests/{id}", d.MCPConnectorActions.GetRequest)
	mux.HandleFunc("GET /api/mcp/vault-items", d.MCPVaultActions.ListItems)
	mux.HandleFunc("POST /api/mcp/vault-actions/call", d.MCPVaultActions.Call)
	mux.HandleFunc("GET /api/mcp/vault-action-requests/{id}", d.MCPVaultActions.GetRequest)
	mux.HandleFunc("POST /api/mcp/vault-action-requests/{id}/cancel", d.MCPVaultActions.CancelRequest)
	registerAdapterRoutes(mux, d.AdapterRoutes)
}

func registerAdapterRoutes(mux *http.ServeMux, routes []AdapterRoute) {
	for _, route := range routes {
		method := strings.ToUpper(strings.TrimSpace(route.Method))
		path := strings.TrimSpace(route.Path)
		if method == "" || route.Handler == nil {
			panic(fmt.Sprintf("invalid connector adapter route %q %q", method, path))
		}
		if err := transportcontract.ValidateConnectorOwnedRoutePath(route.Kind, path); err != nil {
			panic(fmt.Sprintf("invalid connector adapter route %q %q: %v", method, path, err))
		}
		if err := validateAdapterRoutePolicy(method, route.Policy); err != nil {
			panic(fmt.Sprintf("invalid connector adapter route %q %q: %v", method, path, err))
		}
		mux.HandleFunc(method+" "+path, route.Handler)
	}
}

func validateAdapterRoutePolicy(method string, policy AdapterRoutePolicy) error {
	switch policy {
	case AdapterRoutePolicyUIRead:
		if method != http.MethodGet && method != http.MethodHead {
			return fmt.Errorf("ui_read policy requires GET or HEAD")
		}
	case AdapterRoutePolicyUIMutation:
		if !IsStateChangingMethod(method) {
			return fmt.Errorf("ui_mutation policy requires a state-changing method")
		}
	default:
		return fmt.Errorf("route policy is required")
	}
	return nil
}

func registerTransfers(mux *http.ServeMux, t FileTransfers) {
	mux.HandleFunc("GET /api/file-transfers", t.ListFileTransfers)
	mux.HandleFunc("GET /api/file-transfers/{id}", t.GetFileTransfer)
	mux.HandleFunc("GET /api/file-transfers/{id}/download", t.DownloadTransferredFile)
	mux.HandleFunc("POST /api/file-transfers/{id}/cancel", t.CancelFileTransfer)
	mux.HandleFunc("GET /api/file-transfer-batches", t.ListFileTransferBatches)
	mux.HandleFunc("GET /api/file-transfer-batches/{id}", t.GetFileTransferBatch)
	mux.HandleFunc("GET /api/file-transfer-batches/{id}/download", t.DownloadFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/pause", t.PauseFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/resume", t.ResumeFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/cancel", t.CancelFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/queue", t.UpdateFileTransferBatchQueue)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/approve", t.ApproveFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfer-batches/{id}/decline", t.DeclineFileTransferBatch)
	mux.HandleFunc("POST /api/file-transfers/browse", t.BrowseRemoteFiles)
	mux.HandleFunc("POST /api/file-transfers/expand", t.ExpandRemoteFiles)
	mux.HandleFunc("POST /api/file-transfers/upload", t.StartUpload)
	mux.HandleFunc("POST /api/file-transfers/upload-batch", t.StartUploadBatch)
	mux.HandleFunc("POST /api/file-transfers/download", t.StartDownload)
	mux.HandleFunc("POST /api/file-transfers/download-batch", t.StartDownloadBatch)
}

func registerConnectors(mux *http.ServeMux, d Dependencies) {
	mux.HandleFunc("GET /api/connectors", d.ConnectorQueries.ListConnectors)
	mux.HandleFunc("GET /api/connectors/{kind}", d.ConnectorQueries.GetConnector)
	mux.HandleFunc("GET /api/targets", d.ConnectorQueries.ListTargetProfiles)
	mux.HandleFunc("GET /api/connector-targets", d.ConnectorQueries.ListTargets)
	mux.HandleFunc("GET /api/connector-targets/inventory", d.ConnectorQueries.ListTargetInventory)
	mux.HandleFunc("POST /api/connector-targets/with-profile", d.CombinedMutations.Create)
	mux.HandleFunc("POST /api/connector-targets", d.TargetMutations.Create)
	mux.HandleFunc("POST /api/connector-targets/ping", d.HostPing.Ping)
	mux.HandleFunc("POST /api/connector-targets/test", d.TestTarget)
	mux.HandleFunc("GET /api/connector-targets/{id}", d.ConnectorQueries.GetTarget)
	mux.HandleFunc("PUT /api/connector-targets/{id}/with-profile/{profile_id}", d.CombinedMutations.Update)
	mux.HandleFunc("PUT /api/connector-targets/{id}", d.TargetMutations.Update)
	mux.HandleFunc("DELETE /api/connector-targets/{id}", d.DeleteTarget)
	mux.HandleFunc("GET /api/connector-targets/{id}/profiles", d.ConnectorQueries.ListCredentialProfiles)
	mux.HandleFunc("POST /api/connector-targets/{id}/profiles", d.ProfileMutations.Create)
	mux.HandleFunc("POST /api/connector-targets/{id}/profiles/{profile_id}/provision", d.ProfileProvision.Provision)
	mux.HandleFunc("GET /api/connector-targets/{id}/profiles/{profile_id}/backup", d.ProfileBackup.Download)
	mux.HandleFunc("POST /api/connector-targets/{id}/profiles/{profile_id}/restore", d.ProfileBackup.Restore)
	mux.HandleFunc("PUT /api/connector-targets/{id}/profiles/{profile_id}", d.ProfileMutations.Update)
	mux.HandleFunc("DELETE /api/connector-targets/{id}/profiles/{profile_id}", d.ProfileDelete.Delete)
	mux.HandleFunc("POST /api/connector-targets/{id}/profiles/{profile_id}/test", d.ProfileTest.Test)
	mux.HandleFunc("GET /api/connector-targets/{id}/profiles/{profile_id}/actions", d.ConnectorQueries.ListCredentialProfileActions)
}
