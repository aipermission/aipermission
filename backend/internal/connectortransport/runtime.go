// Package connectortransport owns generic network and command transports used
// by connector actions. Connector implementations remain behind connectorapi.
package connectortransport

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type ScopeRuntime interface {
	ConnectorScope(string, connectorruntime.SecretAccessorFactory) *connectorruntime.Scope
}

type Runtime struct {
	Scopes          ScopeRuntime
	Database        *sql.DB
	AcquireDelivery func(context.Context) (func(), error)
}

type AdapterProvider func(string) connectorapi.Adapter

type Dependencies struct {
	Runtime        Runtime
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

func Scope(runtime Runtime, kind string) *connectorruntime.Scope {
	return ScopeWithSecretAccessor(runtime, kind, func(secrets map[string]any) connectors.SecretAccessor {
		return secretAccessor{values: secrets, boundary: actions.NewCredentialBoundary(secrets)}
	})
}

func ScopeWithSecretAccessor(runtime Runtime, kind string, accessor connectorruntime.SecretAccessorFactory) *connectorruntime.Scope {
	if runtime.Scopes == nil {
		return connectorruntime.NewScope(kind, connectorruntime.Dependencies{})
	}
	return runtime.Scopes.ConnectorScope(kind, accessor)
}

func DataRuntime(runtime Runtime, kind string) connectorapi.ConnectorDataRuntime {
	return Scope(runtime, kind).DataRuntime()
}

func LiveRuntime(runtime Runtime, kind string) connectorapi.LiveConsoleRuntime {
	return Scope(runtime, kind).LiveConsoleRuntime()
}

func ActionRuntime(runtime Runtime, kind string) connectorapi.ActionRuntime {
	return Scope(runtime, kind).ActionRuntime()
}

type TargetLifecycleRuntimePort struct {
	connectorapi.LiveSessionRuntime
	Principal func() (executionprincipal.Principal, error)
}

func (runtime TargetLifecycleRuntimePort) ConnectorLocalExecutionPrincipal() (executionprincipal.Principal, error) {
	if runtime.Principal == nil {
		return executionprincipal.Principal{}, fmt.Errorf("connector local execution principal is unavailable")
	}
	return runtime.Principal()
}

func TargetLifecycleRuntime(runtime Runtime, kind string, principal func() (executionprincipal.Principal, error)) connectorapi.TargetLifecycleRuntime {
	return TargetLifecycleRuntimePort{LiveSessionRuntime: Scope(runtime, kind).ActionRuntime(), Principal: principal}
}

func CredentialResourceRuntime(runtime Runtime, kind string) connectorapi.CredentialResourceRuntime {
	return Scope(runtime, kind).DataRuntime()
}

func RequireRuntimeID(ctx context.Context, runtime Runtime, kind string, runtimeID int64) error {
	return Scope(runtime, kind).RequireRuntimeID(ctx, runtimeID)
}

func RequireTargetRuntimeID(ctx context.Context, runtime Runtime, kind string, targetID, runtimeID int64) error {
	return Scope(runtime, kind).RequireTargetRuntimeID(ctx, targetID, runtimeID)
}

var _ connectorapi.TargetLifecycleRuntime = TargetLifecycleRuntimePort{}

type peerGateway struct{ trustStorePath func() string }

func (gateway peerGateway) ConnectorTrustStorePath() string {
	if gateway.trustStorePath == nil {
		return ""
	}
	return gateway.trustStorePath()
}
