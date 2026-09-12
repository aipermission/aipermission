package gatewayaccess

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

var ErrVaultMetadataAccessUnavailable = errors.New("Vault metadata access is unavailable")

type VaultMetadataReader interface {
	CanRead(context.Context, int64, int64, time.Time) (bool, error)
}

type VaultMetadataReaderFactory interface {
	ForDatabase(*sql.DB) VaultMetadataReader
}

type MCPMetadataAdapter interface {
	LiveConsoleTargetMetadata(connectors.TargetView, connectors.CredentialProfileView) map[string]any
}

// MCPMetadataResolver keeps connector metadata adaptation inside the access
// owner while the process composition layer only carries the capability.
type MCPMetadataResolver struct {
	adapterFor func(string) MCPMetadataAdapter
}

func NewMCPMetadataResolver(adapterFor func(string) MCPMetadataAdapter) MCPMetadataResolver {
	return MCPMetadataResolver{adapterFor: adapterFor}
}

func (resolver MCPMetadataResolver) Ready() bool { return resolver.adapterFor != nil }

func (resolver MCPMetadataResolver) Resolve(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	if resolver.adapterFor == nil {
		return nil
	}
	if adapter := resolver.adapterFor(target.ConnectorKind); adapter != nil {
		return adapter.LiveConsoleTargetMetadata(target, profile)
	}
	return nil
}
