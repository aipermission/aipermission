// Package gatewayconnectors exposes connector-neutral runtime and transport contracts to the gateway.
package gatewayconnectors

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
)

const (
	CommandTransportCapabilityName   = connectors.CommandTransportCapabilityName
	NetworkTransportCapabilityName   = connectors.NetworkTransportCapabilityName
	SessionEnvironmentCapabilityName = connectors.SessionEnvironmentCapabilityName
	MaxCommandTimeout                = connectortransport.MaxCommandTimeout
)

var (
	ErrSessionEnvironmentUnsupported = connectors.ErrSessionEnvironmentUnsupported
	ErrorCode                        = connectors.ErrorCode
	ErrorStatus                      = connectors.ErrorStatus
	FormatTargetRef                  = connectors.FormatTargetRef
	NewRegistry                      = connectors.NewRegistry
	ErrApprovalChanged               = connectortransport.ErrApprovalChanged
	NewApproved                      = connectortransport.NewApproved
	ScopeWithSecretAccessor          = connectortransport.ScopeWithSecretAccessor
)

type ActionHandles = connectors.ActionHandles
type ActionResult = connectors.ActionResult
type CommandRunRequest = connectors.CommandRunRequest
type CommandRunResult = connectors.CommandRunResult
type CredentialProfileView = connectors.CredentialProfileView
type NetworkDialRequest = connectors.NetworkDialRequest
type OutputHint = connectors.OutputHint
type Registry = connectors.Registry
type ResultStatus = connectors.ResultStatus
type RuntimeCapability = connectors.RuntimeCapability
type RuntimeCapabilityResolver = connectors.RuntimeCapabilityResolver
type SecretAccessor = connectors.SecretAccessor
type SessionEnvironmentCapability = connectors.SessionEnvironmentCapability
type TargetView = connectors.TargetView
type AdapterProvider = connectortransport.AdapterProvider
type Approved = connectortransport.Approved
type Dependencies = connectortransport.Dependencies
type Command = connectortransport.Command
type Network = connectortransport.Network

const ResultOutcomeUnknown = connectors.ResultOutcomeUnknown
