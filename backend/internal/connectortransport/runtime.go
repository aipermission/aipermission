// Package connectortransport owns generic network and command transports used
// by connector actions. Connector implementations remain behind connectorapi.
package connectortransport

import (
	"context"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type AdapterProvider func(string) connectorapi.Adapter

type Dependencies struct {
	Runtime        *workspaceruntime.Runtime
	AdapterFor     AdapterProvider
	TrustStorePath func() string
}

type Capabilities map[string]connectors.RuntimeCapability

func (capabilities Capabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	return capabilities[name]
}

func NewCapabilities(dependencies Dependencies, approvedDependencies []actions.ResolvedDependency) Capabilities {
	approved := NewApproved(approvedDependencies)
	return Capabilities{
		connectors.NetworkTransportCapabilityName: Network{Dependencies: dependencies, Approved: approved},
		connectors.CommandTransportCapabilityName: Command{Dependencies: dependencies, Approved: approved},
	}
}

type secretAccessor struct {
	values   map[string]any
	boundary actions.CredentialBoundary
}

func (accessor secretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := accessor.values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	text := fmt.Sprint(value)
	accessor.boundary.Add(text)
	return text, nil
}

func (accessor secretAccessor) RegisterSensitiveValue(value string) { accessor.boundary.Add(value) }

func liveRuntime(runtime *workspaceruntime.Runtime, kind string) connectorapi.LiveConsoleRuntime {
	if runtime == nil {
		return connectorruntime.NewScope(kind, connectorruntime.Dependencies{}).LiveConsoleRuntime()
	}
	return connectorruntime.NewScope(kind, connectorruntime.Dependencies{
		Database: runtime.Storage.Database, Vault: runtime.Storage.Vault, WorkspaceID: runtime.WorkspaceUUID,
		Resources: runtime.Connectors.Resources, ConsoleSessions: runtime.Connectors.ConsoleSessions,
		SecretAccessor: func(secrets map[string]any) connectors.SecretAccessor {
			return secretAccessor{values: secrets, boundary: actions.NewCredentialBoundary(secrets)}
		},
	}).LiveConsoleRuntime()
}

type peerGateway struct{ trustStorePath func() string }

func (gateway peerGateway) ConnectorTrustStorePath() string {
	if gateway.trustStorePath == nil {
		return ""
	}
	return gateway.trustStorePath()
}
