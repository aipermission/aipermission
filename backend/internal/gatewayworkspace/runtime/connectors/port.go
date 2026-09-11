// Package connectors defines the workspace boundary's connector-state port.
package connectors

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
)

type Port interface {
	ConnectorRegistry() *connectorcatalog.Registry
	ConnectorAdapterRegistry() *connectorapi.Registry
	ResourceScopes() connectorruntime.ResourceScopes
	ConsoleSessionManager() *console.Manager
	SetConsoleSessionManager(*console.Manager)
	ConnectorScope(string, connectorruntime.SecretAccessorFactory) *connectorruntime.Scope
}
