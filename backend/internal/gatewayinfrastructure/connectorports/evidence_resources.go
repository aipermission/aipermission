package connectorports

import (
	"context"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type evidenceResourceRuntime struct {
	resources connectorapi.ScopedResourceRuntime
}
type evidenceResourceReader struct {
	reader connectorapi.CredentialResourceReader
}

// evidenceResources narrows actual runtime method sets, preventing an adapter
// from recovering mutation/secret authority by type-asserting a read interface.
func evidenceResources(resources connectorapi.ScopedResourceRuntime) connectorapi.EvidenceResourceRuntime {
	if connectorapi.IsNilDependency(resources) {
		return nil
	}
	return evidenceResourceRuntime{resources: resources}
}

func (runtime evidenceResourceRuntime) CredentialResources(kind string) connectorapi.CredentialResourceReader {
	store := runtime.resources.CredentialResources(kind)
	if connectorapi.IsNilDependency(store) {
		return nil
	}
	return evidenceResourceReader{reader: store}
}

func (reader evidenceResourceReader) Get(ctx context.Context, id int64) (connectorapi.CredentialResource, error) {
	return reader.reader.Get(ctx, id)
}
