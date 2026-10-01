package connectorports

import (
	"context"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type evidenceResourceRuntime struct {
	resources resourcecontract.ScopedResourceRuntime
}
type evidenceResourceReader struct {
	reader resourcecontract.CredentialResourceReader
}

// evidenceResources narrows actual runtime method sets, preventing an adapter
// from recovering mutation/secret authority by type-asserting a read interface.
func evidenceResources(resources resourcecontract.ScopedResourceRuntime) resourcecontract.EvidenceResourceRuntime {
	if resourcecontract.IsNilDependency(resources) {
		return nil
	}
	return evidenceResourceRuntime{resources: resources}
}

func (runtime evidenceResourceRuntime) CredentialResources(kind string) resourcecontract.CredentialResourceReader {
	store := runtime.resources.CredentialResources(kind)
	if resourcecontract.IsNilDependency(store) {
		return nil
	}
	return evidenceResourceReader{reader: store}
}

func (reader evidenceResourceReader) Get(ctx context.Context, id int64) (resourcecontract.CredentialResource, error) {
	return reader.reader.Get(ctx, id)
}
