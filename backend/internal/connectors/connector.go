package connectors

import "context"

// Connector is the required contract for connector-shaped targets.
//
// GetHelp, GetActionList, and PrepareAction must be side-effect-free and should
// not need raw secret access. ExecuteAction receives RuntimeContext only after
// core permission and approval rules allow execution.
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

// EventSink lets long-running connectors emit progress without writing
// history/audit directly.
type EventSink interface {
	Emit(ctx context.Context, event ActionEvent) error
}

// RuntimeContext is available only to execution/test paths. It intentionally
// separates public target/profile metadata from secret access.
type RuntimeContext struct {
	Target  TargetView
	Profile CredentialProfileView

	Secrets  SecretAccessor
	Events   EventSink
	// Services is an explicit escape hatch for gateway-owned runtime adapters
	// such as SSH PTY/SFTP. Normal structured connectors should use Target,
	// Profile, Secrets, and their own client code instead of depending on
	// gateway internals.
	Services map[string]any
}

func (c RuntimeContext) Service(name string) any {
	if c.Services == nil {
		return nil
	}
	return c.Services[name]
}

// ActionEvent is a structured, redaction-ready progress event.
type ActionEvent struct {
	Phase   string         `json:"phase"`
	Level   string         `json:"level,omitempty"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}
