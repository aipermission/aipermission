package gatewayconnectormanagement

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func ProjectScopedSupportedConnectorPermissions(ctx context.Context, database *sql.DB, registry *connectors.Registry, tokenID int64) ([]connectortargets.ActionPermission, error) {
	return accesscontrol.ProjectScopedSupportedConnectorPermissions(ctx, database, registry, tokenID)
}

func ConnectorApprovalItemForResponse(ctx context.Context, workflow connectorapproval.Workflow, item connectortargets.ActionRequest) (connectorapproval.Item, error) {
	return connectorapproval.ItemForResponse(ctx, workflow, item)
}

func ConnectorApprovalItemFromRequest(item connectortargets.ActionRequest) connectorapproval.Item {
	return connectorapproval.ItemFromRequest(item)
}

func NewConnectorApprovalHTTPHandlers(scope connectorapproval.ScopeProvider) *connectorapproval.HTTPHandlers {
	return connectorapproval.NewHTTPHandlers(scope)
}

func NewCombinedMutationHTTPHandler(scope connectormanagement.CombinedMutationScopeProvider) *connectormanagement.CombinedMutationHTTPHandler {
	return connectormanagement.NewCombinedMutationHTTPHandler(scope)
}

func NewHTTPHandlers(scope connectormanagement.ScopeProvider) *connectormanagement.HTTPHandlers {
	return connectormanagement.NewHTTPHandlers(scope)
}

func NewHostPingHTTPHandler(scope connectormanagement.HostPingScopeProvider) *connectormanagement.HostPingHTTPHandler {
	return connectormanagement.NewHostPingHTTPHandler(scope)
}

func NewProfileBackupHTTPHandler(scope connectormanagement.ProfileBackupScopeProvider) *connectormanagement.ProfileBackupHTTPHandler {
	return connectormanagement.NewProfileBackupHTTPHandler(scope)
}

func NewProfileDeletionHTTPHandler(scope connectormanagement.ProfileDeletionScopeProvider) *connectormanagement.ProfileDeletionHTTPHandler {
	return connectormanagement.NewProfileDeletionHTTPHandler(scope)
}

func NewProfileMutationHTTPHandler(scope connectormanagement.ProfileMutationScopeProvider) *connectormanagement.ProfileMutationHTTPHandler {
	return connectormanagement.NewProfileMutationHTTPHandler(scope)
}

func NewProfileTestingHTTPHandler(scope connectormanagement.ProfileTestingScopeProvider) *connectormanagement.ProfileTestingHTTPHandler {
	return connectormanagement.NewProfileTestingHTTPHandler(scope)
}

func NewProvisioningHTTPHandler(scope connectormanagement.ProvisioningScopeProvider) *connectormanagement.ProvisioningHTTPHandler {
	return connectormanagement.NewProvisioningHTTPHandler(scope)
}

func NewTargetMutationHTTPHandler(scope connectormanagement.TargetMutationScopeProvider) *connectormanagement.TargetMutationHTTPHandler {
	return connectormanagement.NewTargetMutationHTTPHandler(scope)
}

func NormalizeTargetConfig(connector connectors.Connector, config map[string]any) (map[string]any, error) {
	return connectormanagement.NormalizeTargetConfig(connector, config)
}

func RuntimeCredentialPorts(runtime workspaceruntime.Port, capabilities connectormanagement.RuntimeCapabilities, redactResult connectormanagement.ResultRedactor, redactText connectormanagement.TextRedactor) connectormanagement.CredentialRuntimePorts {
	return connectormanagement.RuntimeCredentialPorts(runtime, capabilities, redactResult, redactText)
}

func RuntimeCredentialPreparation(runtime workspaceruntime.Port, provider connectormanagement.CredentialCanonicalizerProvider) connectormanagement.CredentialPreparationPorts {
	return connectormanagement.RuntimeCredentialPreparation(runtime, provider)
}

func NewStore(db *sql.DB) *connectortargets.Store {
	return connectortargets.NewStore(db)
}

func NewTxStore(tx *sql.Tx) *connectortargets.Store {
	return connectortargets.NewTxStore(tx)
}
