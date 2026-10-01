// Package apiadapter supplies the Postgres connector's scoped domain resources.
package apiadapter

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type adapter struct{}

func New() connectorapi.Adapter { return adapter{} }

func (adapter) ScopedResourceCapabilities(runtime resourcecontract.ScopedResourceRuntime) map[string]connectors.RuntimeCapability {
	var store resourcecontract.CredentialResourceStore
	if !resourcecontract.IsNilDependency(runtime) {
		store = runtime.CredentialResources(rolejournal.ResourceKind)
	}
	return map[string]connectors.RuntimeCapability{
		rolejournal.CapabilityName: rolejournal.New(store),
	}
}

var _ connectorapi.ScopedResourceCapabilityProvider = adapter{}

func (adapter) EvidenceCapabilities(runtime resourcecontract.EvidenceResourceRuntime) map[string]connectors.RuntimeCapability {
	var reader resourcecontract.CredentialResourceReader
	if !resourcecontract.IsNilDependency(runtime) {
		reader = runtime.CredentialResources(rolejournal.ResourceKind)
	}
	return map[string]connectors.RuntimeCapability{
		rolejournal.CleanupEvidenceCapabilityName: rolejournal.NewCleanupEvidence(reader),
	}
}

var _ connectorapi.EvidenceCapabilityProvider = adapter{}
