package connectors

import "context"

// Connector is the required contract for connector-shaped targets.
//
// GetHelp, GetActionList, and PrepareAction must be side-effect-free and should
// not need raw secret access. GetActionList is the permission catalog: it must
// return stable action definitions for the connector kind, and it must not hide
// actions based on network reachability or mutable target/profile state because
// permission reads, approval drift checks, and MCP discovery call it on read
// paths. ExecuteAction receives RuntimeContext only after core permission and
// approval rules allow execution.
//
// Normal structured connectors should return terminal ActionResult statuses
// from ExecuteAction. Returning ResultRunning is reserved for gateway-owned
// runtime adapters that can finalize the request, sync history, and provide
// polling hints from internal/api.
type Connector interface {
	Kind() string
	Label() string
	Version() string

	TargetSchema() Schema
	CredentialSchemas() []CredentialSchema

	GetHelp(ctx context.Context, target TargetView) (ConnectorHelp, error)
	GetActionList(ctx context.Context, target TargetView, profile CredentialProfileView) ([]ActionDefinition, error)

	PrepareAction(ctx context.Context, req ActionRequest) (PreparedAction, error)
	ExecuteAction(ctx context.Context, runtime RuntimeContext, action PreparedAction) (ActionResult, error)
}

// TestableConnector is optional because some connectors cannot test a
// connection without side effects or expensive setup.
type TestableConnector interface {
	TestConnection(ctx context.Context, runtime RuntimeContext) (TestResult, error)
}

// SecretAccessor resolves connector credential secrets at runtime.
type SecretAccessor interface {
	GetSecret(ctx context.Context, name string) (string, error)
}

// EventSink is reserved for future connector progress events.
//
// In the 0.2 connector baseline, the gateway provides a no-op sink. Connectors
// must not rely on emitted events being persisted or streamed yet; return
// terminal ActionResult values or use a reviewed runtime adapter for running
// actions.
type EventSink interface {
	Emit(ctx context.Context, event ActionEvent) error
}

// RuntimeCapability is implemented by connector-owned live/runtime services
// that are injected only for reviewed gateway adapters. Structured connectors
// should normally not need a runtime capability.
type RuntimeCapability interface {
	ConnectorRuntimeCapability() string
}

// RuntimeCapabilityResolver resolves reviewed connector-owned capabilities
// without exposing the gateway runtime or an untyped service map.
type RuntimeCapabilityResolver interface {
	RuntimeCapability(name string) RuntimeCapability
}

// RuntimeContext is available only to execution/test paths. It intentionally
// separates public target/profile metadata from secret access.
type RuntimeContext struct {
	Target  TargetView
	Profile CredentialProfileView

	Secrets SecretAccessor
	Events  EventSink
	// Capabilities is reserved for gateway-owned runtime adapters that need
	// live transports, file transfer, or other long-lived resources. Normal
	// structured connectors should use Target, Profile, Secrets, and their own
	// client code instead of depending on gateway internals.
	Capabilities RuntimeCapabilityResolver
}

func (c RuntimeContext) Capability(name string) RuntimeCapability {
	if c.Capabilities == nil {
		return nil
	}
	return c.Capabilities.RuntimeCapability(name)
}

// ActionEvent is a structured, redaction-ready progress event.
type ActionEvent struct {
	Phase   string         `json:"phase"`
	Level   string         `json:"level,omitempty"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}
