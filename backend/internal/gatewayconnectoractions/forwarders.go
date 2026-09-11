package gatewayconnectoractions

import (
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func NewCredentialBoundary(secrets map[string]any) actions.CredentialBoundary {
	return actions.NewCredentialBoundary(secrets)
}

func SensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	return actions.SensitiveOutputFields(hints...)
}
