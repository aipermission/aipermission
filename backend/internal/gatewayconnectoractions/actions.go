// Package gatewayconnectoractions is the connector-action application contract consumed by the gateway.
package gatewayconnectoractions

import (
	"github.com/aipermission/aipermission/backend/internal/actions"
)

const (
	ApprovalHint                 = actions.ApprovalHint
	RunningHint                  = actions.RunningHint
	TerminalPersistenceErrorText = actions.TerminalPersistenceErrorText
)

var (
	ErrMCPExecutionStopped = actions.ErrMCPExecutionStopped
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
