package gatewayconnectormanagement

import "github.com/aipermission/aipermission/backend/internal/connectormanagement"

func (*Component) RuntimeCredentialPorts(
	storage connectormanagement.CredentialStorage,
	capabilities connectormanagement.RuntimeCapabilities,
	redactResult connectormanagement.ResultRedactor,
	redactText connectormanagement.TextRedactor,
) connectormanagement.CredentialRuntimePorts {
	return connectormanagement.RuntimeCredentialPorts(storage, capabilities, redactResult, redactText)
}

func (*Component) RuntimeCredentialPreparation(
	storage connectormanagement.CredentialStorage,
	provider connectormanagement.CredentialCanonicalizerProvider,
) connectormanagement.CredentialPreparationPorts {
	return connectormanagement.RuntimeCredentialPreparation(storage, provider)
}
