// Package connectors defines the workspace boundary's connector-state port.
package connectors

import (
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type Port interface {
	ConnectorRegistry() *connectorcatalog.Registry
	ConnectorAdapterRegistry() *connectorapi.Registry
	ResourceScopes() connectorruntime.ResourceScopes
	ConsoleSessionManager() *console.Manager
	ConfigureConsoleSessions(console.RuntimeOpener, func(string) string)
	ConnectorScope(string, connectorruntime.SecretAccessorFactory) *connectorruntime.Scope
}
