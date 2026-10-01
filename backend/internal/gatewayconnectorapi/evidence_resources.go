package gatewayconnectorapi

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type EvidenceCapabilityProvider interface {
	EvidenceCapabilities(resourcecontract.EvidenceResourceRuntime) map[string]connectors.RuntimeCapability
}
