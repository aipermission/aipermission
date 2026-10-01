package gatewayconnectorapi

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// CredentialTargetOperationRunner is optional for authenticated local operator
// operations requiring one current credential. Core binds target/profile and
// owns exclusive admission, credential decryption and response/audit masking.
// Implementations receive no raw database, Vault or profile publication port.
type CredentialTargetOperationRunner interface {
	// SupportsCredentialTargetOperation must be side-effect-free and stable.
	SupportsCredentialTargetOperation(operation string) bool
	RunCredentialTargetOperation(ctx context.Context, gateway TargetOperationGateway, runtime connectors.RuntimeContext, operation string, input map[string]any) (connectors.ManagementResponse, error)
}
