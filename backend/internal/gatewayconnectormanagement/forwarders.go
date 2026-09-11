package gatewayconnectormanagement

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (*Component) ConnectorApprovalItemForResponse(ctx context.Context, workflow connectorapproval.Workflow, item connectortargets.ActionRequest) (connectorapproval.Item, error) {
	return connectorapproval.ItemForResponse(ctx, workflow, item)
}

func (*Component) ConnectorApprovalItemFromRequest(item connectortargets.ActionRequest) connectorapproval.Item {
	return connectorapproval.ItemFromRequest(item)
}

func (*Component) NormalizeTargetConfig(connector connectors.Connector, config map[string]any) (map[string]any, error) {
	return connectormanagement.NormalizeTargetConfig(connector, config)
}

func (*Component) RuntimeCredentialPorts(storage connectormanagement.CredentialStorage, capabilities connectormanagement.RuntimeCapabilities, redactResult connectormanagement.ResultRedactor, redactText connectormanagement.TextRedactor) connectormanagement.CredentialRuntimePorts {
	return connectormanagement.RuntimeCredentialPorts(storage, capabilities, redactResult, redactText)
}

func (*Component) RuntimeCredentialPreparation(storage connectormanagement.CredentialStorage, provider connectormanagement.CredentialCanonicalizerProvider) connectormanagement.CredentialPreparationPorts {
	return connectormanagement.RuntimeCredentialPreparation(storage, provider)
}
