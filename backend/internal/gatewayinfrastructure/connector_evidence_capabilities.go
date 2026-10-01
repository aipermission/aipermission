package gatewayinfrastructure

import (
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectorcapabilities"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
)

// CleanupEvidenceCapabilities excludes mutation, secret and transport authority,
// including action/session providers. Only public evidence can be read.
func (application *ConnectorRuntimeApplication) CleanupEvidenceCapabilities(handle *WorkspaceHandle, kind string) (connectors.RuntimeCapabilityResolver, error) {
	workspace, ok := application.workspace(handle, false, nil, ConnectorTargetWorkflowPorts{})
	if !ok {
		return nil, errors.New("connector resource runtime is unavailable")
	}
	return connectorcapabilities.Evidence(application.adapters.For(kind), connectorruntime.EvidenceResources(connectorports.ScopedResourceRuntime(workspace, kind)))
}
