// Package connectors defines the workspace boundary's connector-state port.
package connectors

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type Port interface {
	ConnectorRegistry() *connectorcatalog.Registry
	ConnectorAdapterRegistry() *connectorapi.Registry
	ResourceScopes() connectorruntime.ResourceScopes
	ConsoleSessionManager() *console.Manager
	ConfigureConsoleSessions(console.RuntimeOpener, func(string) string)
	ConfigureVaultSessionAuthorizer(*vaultsessions.Store, func(context.Context, func() error, func() error) error)
	ConfigureSessionClosedHook(func(context.Context, int64, int64, int64) error)
	ConnectorScope(string, connectorruntime.SecretAccessorFactory) *connectorruntime.Scope
}
