package gatewayworkspace

import (
	accesscapability "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/capabilities/access"
	connectorcapability "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/capabilities/connector"
	observationcapability "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/capabilities/observation"
	operationcapability "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/capabilities/operation"
	vaultcapability "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/capabilities/vault"
)

type Capability[T any] struct{ resolve func() (T, bool) }

func capability[T any](resolve func() (T, bool)) Capability[T] {
	return Capability[T]{resolve: resolve}
}

func (projected Capability[T]) Current() (T, bool) {
	if projected.resolve == nil {
		var zero T
		return zero, false
	}
	return projected.resolve()
}

type Access struct {
	VaultMetadata        Capability[accesscapability.VaultMetadataCapability]
	AccessControl        Capability[accesscapability.AccessControlCapability]
	MCPTokenSource       Capability[accesscapability.MCPTokenSource]
	MCPRead              Capability[accesscapability.MCPReadCapability]
	MCPAction            Capability[accesscapability.MCPActionCapability]
	MCPRuntime           Capability[accesscapability.MCPRuntimeCapability]
	RuntimeControl       Capability[accesscapability.RuntimeControlCapability]
	ConsoleRecovery      Capability[accesscapability.ConsoleRecoveryCapability]
	SecurityPolicy       Capability[accesscapability.SecurityPolicyCapability]
	RuntimeConfiguration Capability[accesscapability.RuntimeConfigurationCapability]
	ConsoleConfiguration Capability[accesscapability.ConsoleConfigurationCapability]
}

type ConnectorActions struct {
	Action   Capability[connectorcapability.ActionCapability]
	Approval Capability[connectorcapability.ApprovalCapability]
	Tag      func([]byte) (string, error)
}

type ConnectorManagement struct {
	Catalog    Capability[connectorcapability.CatalogCapability]
	Credential Capability[connectorcapability.CredentialCapability]
	Management Capability[connectorcapability.ManagementCapability]
}

type ConnectorPorts struct {
	Transport Capability[connectorcapability.TransportCapability]
}

type Observation struct {
	Runtime Capability[observationcapability.Capability]
}

type Operations struct {
	Command            Capability[operationcapability.CommandCapability]
	CommandBulk        Capability[operationcapability.CommandBulkCapability]
	LiveConsole        Capability[operationcapability.LiveConsoleCapability]
	Backup             Capability[operationcapability.BackupCapability]
	PasswordValidation Capability[operationcapability.PasswordValidationCapability]
	Transfer           Capability[operationcapability.TransferCapability]
	PeerTrust          Capability[operationcapability.PeerTrustCapability]
	Message            Capability[accesscapability.MessageCapability]
}

type Vault struct {
	Runtime  Capability[vaultcapability.RuntimeCapability]
	Session  Capability[vaultcapability.SessionCapability]
	MCP      Capability[vaultcapability.MCPCapability]
	Approval Capability[vaultcapability.ApprovalCapability]
	Project  Capability[accesscapability.ProjectCapability]
}

type Projection struct {
	Access              Access
	ConnectorActions    ConnectorActions
	ConnectorManagement ConnectorManagement
	ConnectorPorts      ConnectorPorts
	Observation         Observation
	Operations          Operations
	Vault               Vault
}

func ProjectCapabilities(runtime *Runtime) Projection {
	if runtime == nil {
		return Projection{}
	}
	return Projection{
		Access: Access{
			VaultMetadata: capability(runtime.VaultMetadataCapability), AccessControl: capability(runtime.AccessControlCapability),
			MCPTokenSource: capability(runtime.MCPTokenSourceCapability), MCPRead: capability(runtime.MCPReadCapability),
			MCPAction: capability(runtime.MCPActionCapability), MCPRuntime: capability(runtime.MCPRuntimeCapability),
			RuntimeControl: capability(runtime.RuntimeControlCapability), ConsoleRecovery: capability(runtime.ConsoleRecoveryCapability),
			SecurityPolicy: capability(runtime.SecurityPolicyCapability), RuntimeConfiguration: capability(runtime.RuntimeConfigurationCapability),
			ConsoleConfiguration: capability(runtime.ConsoleConfigurationCapability),
		},
		ConnectorActions: ConnectorActions{
			Action: capability(runtime.ConnectorActionCapability), Approval: capability(runtime.ConnectorApprovalCapability),
			Tag: runtime.TagActionIdentity,
		},
		ConnectorManagement: ConnectorManagement{
			Catalog: capability(runtime.ConnectorCatalogCapability), Credential: capability(runtime.ConnectorCredentialCapability),
			Management: capability(runtime.ConnectorManagementCapability),
		},
		ConnectorPorts: ConnectorPorts{Transport: capability(runtime.ConnectorTransportCapability)},
		Observation:    Observation{Runtime: capability(runtime.ObservationCapability)},
		Operations: Operations{
			Command: capability(runtime.CommandCapability), CommandBulk: capability(runtime.CommandBulkCapability),
			LiveConsole: capability(runtime.LiveConsoleCapability), Backup: capability(runtime.BackupCapability),
			PasswordValidation: capability(runtime.PasswordValidationCapability), Transfer: capability(runtime.TransferCapability),
			PeerTrust: capability(runtime.PeerTrustCapability), Message: capability(runtime.MessageCapability),
		},
		Vault: Vault{
			Runtime: capability(runtime.VaultRuntimeCapability), Session: capability(runtime.VaultSessionCapability),
			MCP: capability(runtime.VaultMCPCapability), Approval: capability(runtime.VaultApprovalCapability),
			Project: capability(runtime.ProjectCapability),
		},
	}
}
