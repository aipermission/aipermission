// Package gatewaystate groups process-scoped workspace, connector, and control state.
package gatewaystate

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/gatewaystate/catalog"
	"github.com/aipermission/aipermission/backend/internal/gatewaystate/controls"
	"github.com/aipermission/aipermission/backend/internal/gatewaystate/workspaces"
)

const (
	AuthLockoutFailures      = controls.AuthLockoutFailures
	MCPGlobalDelayFailures   = controls.MCPGlobalDelayFailures
	MCPGlobalLockoutFailures = controls.MCPGlobalLockoutFailures
)

type WorkspaceState = workspaces.State
type ConnectorState = catalog.State
type ControlState = controls.State

func NewConnectorState(registry *connectors.Registry, adapters *connectorapi.Registry) ConnectorState {
	return catalog.New(registry, adapters)
}

func NewControlState(frontendPort string, maintenance console.MaintenanceConsoleRuntime) ControlState {
	return controls.New(frontendPort, maintenance)
}
