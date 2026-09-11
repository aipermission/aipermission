package gatewayconnectoractions

import (
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func (*Component) NewCredentialBoundary(secrets map[string]any) CredentialBoundary {
	return newCredentialBoundary(actions.NewCredentialBoundary(secrets))
}

func (*Component) SensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	return actions.SensitiveOutputFields(hints...)
}
