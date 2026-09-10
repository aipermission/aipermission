// Package gatewayconnectoractions is the connector-action application contract consumed by the gateway.
package gatewayconnectoractions

import (
	"github.com/aipermission/aipermission/backend/internal/actions"
	applicationactions "github.com/aipermission/aipermission/backend/internal/applicationconnectoractions"
)

const (
	ApprovalHint                 = actions.ApprovalHint
	RunningHint                  = actions.RunningHint
	TerminalPersistenceErrorText = actions.TerminalPersistenceErrorText
)

var (
	ErrMCPExecutionStopped = actions.ErrMCPExecutionStopped
	NewCredentialBoundary  = actions.NewCredentialBoundary
	SensitiveOutputFields  = actions.SensitiveOutputFields
	Delivery               = applicationactions.Delivery
	New                    = applicationactions.New
	Prepare                = applicationactions.Prepare
	StopRecovery           = applicationactions.StopRecovery
)

type ApprovalPermissionSnapshot = actions.ApprovalPermissionSnapshot
type ApprovalTokenSnapshot = actions.ApprovalTokenSnapshot
type AuditAppender = actions.AuditAppender
type Call = actions.Call
type CallResult = actions.CallResult
type CredentialBoundary = actions.CredentialBoundary
type PrepareRequest = actions.PrepareRequest
type PreparedRequest = actions.PreparedRequest
type Redactor = actions.Redactor
type ResolvedDependency = actions.ResolvedDependency
type Runtime = actions.Runtime
type TerminalPersistenceError = actions.TerminalPersistenceError
type Component = applicationactions.Component
type Dependencies = applicationactions.Dependencies
type LocalHTTPDependencies = applicationactions.LocalHTTPDependencies
type LocalHTTPHandlers = applicationactions.LocalHTTPHandlers
type LocalRequest = applicationactions.LocalRequest
type NoopEventSink = applicationactions.NoopEventSink
type SecretAccessor = applicationactions.SecretAccessor
