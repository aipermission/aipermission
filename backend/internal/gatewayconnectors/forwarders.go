package gatewayconnectors

import (
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func ErrorCode(err error) string {
	return connectors.ErrorCode(err)
}

func ErrorStatus(err error) connectors.ResultStatus {
	return connectors.ErrorStatus(err)
}

func FormatTargetRef(connectorKind string, targetID int64, profileID int64) string {
	return connectors.FormatTargetRef(connectorKind, targetID, profileID)
}

func NewRegistry() *connectors.Registry {
	return connectors.NewRegistry()
}

func NewApproved(dependencies []actions.ResolvedDependency) connectortransport.Approved {
	return connectortransport.NewApproved(dependencies)
}

func ScopeWithSecretAccessor(runtime workspaceruntime.Port, kind string, accessor connectorruntime.SecretAccessorFactory) *connectorruntime.Scope {
	return connectortransport.ScopeWithSecretAccessor(runtime, kind, accessor)
}
