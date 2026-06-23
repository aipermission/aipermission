package connectors

import (
	"context"
	"net"
)

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

// CredentialProvisioner is an optional connector contract for operator-driven
// credential profile provisioning. Core owns profile persistence and vault
// writes; the connector owns external service changes such as creating or
// dropping a database role.
type CredentialProvisioner interface {
	ProvisionCredentialProfile(ctx context.Context, runtime RuntimeContext, input map[string]any) (ProvisionedCredentialProfile, error)
	CleanupProvisionedCredentialProfile(ctx context.Context, runtime RuntimeContext, profile CredentialProfileView) (ActionResult, error)
}

// BackupRestorer is an optional connector contract for operator-driven backup
// and restore flows. Core owns HTTP upload/download, confirmation, and audit;
// the connector owns the external service dump/restore implementation.
type BackupRestorer interface {
	Backup(ctx context.Context, runtime RuntimeContext, request BackupRequest) (BackupArtifact, error)
	Restore(ctx context.Context, runtime RuntimeContext, request RestoreRequest) (ActionResult, error)
}

// ProvisionedCredentialProfile is returned by connector provisioning code after
// it has completed the external service change. Secret is encrypted by core
// before persistence and must never be returned to UI or MCP list responses.
type ProvisionedCredentialProfile struct {
	Kind      string
	Label     string
	Public    map[string]any
	Secret    map[string]any
	RiskLabel string
	Result    ActionResult
}

type BackupRequest struct {
	Format string
}

type BackupArtifact struct {
	Filename    string
	ContentType string
	Data        []byte
	Metadata    map[string]any
}

type RestoreRequest struct {
	Filename string
	Data     []byte
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

const (
	NetworkTransportCapabilityName = "network_transport"
	CommandTransportCapabilityName = "command_transport"
)

// NetworkDialRequest describes a connector-owned network connection request.
//
// Connectors use this capability when the endpoint may be reached either
// directly from the local gateway or through another reviewed connector-backed
// transport such as SSH. The connector remains responsible for its protocol;
// the gateway only opens the TCP pipe.
type NetworkDialRequest struct {
	Mode               string
	Host               string
	Port               int
	TransportTargetRef string
}

// NetworkTransport is a generic TCP transport capability injected by the
// gateway. It intentionally exposes net.Conn rather than connector internals so
// protocol connectors such as Redis can stay independent from SSH.
type NetworkTransport interface {
	RuntimeCapability
	DialConnectorTCP(ctx context.Context, request NetworkDialRequest) (net.Conn, error)
}

// CommandRunRequest describes a connector-owned command execution request.
//
// Connectors use this capability when a bounded, connector-owned command
// template must run through another reviewed connector transport such as SSH.
// The caller connector still owns the command shape, output parsing, and
// safety limits; the gateway only routes the command to the selected transport.
type CommandRunRequest struct {
	Mode               string
	TransportTargetRef string
	Command            string
	TimeoutSeconds     int
}

// CommandRunResult is the captured result of one connector transport command.
type CommandRunResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
}

// CommandTransport is a generic command transport capability injected by the
// gateway. It intentionally returns only captured process output so structured
// connectors such as Docker can stay independent from SSH internals.
type CommandTransport interface {
	RuntimeCapability
	RunConnectorCommand(ctx context.Context, request CommandRunRequest) (CommandRunResult, error)
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
