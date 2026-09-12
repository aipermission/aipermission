package gatewayconnectormanagement

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func (*Component) RuntimeCredentialPorts(
	storage CredentialStorage,
	capabilities connectormanagement.RuntimeCapabilities,
	redactResult func(context.Context, connectors.ActionResult, CredentialBoundary) (connectors.ActionResult, error),
	redactText connectormanagement.TextRedactor,
) CredentialRuntimePorts {
	var redactor connectormanagement.ResultRedactor
	if redactResult != nil {
		redactor = func(ctx context.Context, result connectors.ActionResult, boundary connectormanagement.CredentialBoundary) (connectors.ActionResult, error) {
			return redactResult(ctx, result, wrapCredentialBoundary(boundary))
		}
	}
	return CredentialRuntimePorts{value: connectormanagement.RuntimeCredentialPorts(
		connectormanagement.CredentialStorage(storage), capabilities, redactor, redactText,
	)}
}

func (*Component) RuntimeCredentialPreparation(
	storage CredentialStorage,
	provider func(string) CredentialCanonicalizer,
) CredentialPreparationPorts {
	var canonicalizers connectormanagement.CredentialCanonicalizerProvider
	if provider != nil {
		canonicalizers = func(kind string) connectormanagement.CredentialCanonicalizer {
			return connectormanagement.CredentialCanonicalizer(provider(kind))
		}
	}
	return CredentialPreparationPorts{value: connectormanagement.RuntimeCredentialPreparation(
		connectormanagement.CredentialStorage(storage), canonicalizers,
	)}
}
