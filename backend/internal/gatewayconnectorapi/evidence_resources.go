package gatewayconnectorapi

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// CredentialResourceReader exposes only public evidence. Unlike the mutable
// store it cannot decrypt, create, update, delete or enumerate credentials.
type CredentialResourceReader interface {
	Get(context.Context, int64) (CredentialResource, error)
}

// EvidenceResourceRuntime is scoped to a connector by core. Its concrete
// readers, not merely their static interfaces, must exclude mutable methods.
type EvidenceResourceRuntime interface {
	CredentialResources(resourceKind string) CredentialResourceReader
}

type EvidenceCapabilityProvider interface {
	EvidenceCapabilities(EvidenceResourceRuntime) map[string]connectors.RuntimeCapability
}
