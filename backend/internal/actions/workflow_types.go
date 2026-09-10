package actions

import (
	"errors"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

var (
	ErrMCPExecutionStopped           = errors.New("MCP execution is stopped")
	ErrConnectorAuthorizationChanged = errors.New("connector authorization changed before dispatch")
)

const (
	SourceMCP                    = "mcp"
	SourceManual                 = "manual"
	ActionToolName               = "connector.call_action"
	ApprovalHint                 = "Wait 3 seconds, then poll this connector action request until it is completed, failed, declined, stale, blocked, or outcome_unknown."
	HandlePersistenceError       = "connector action returned a session handle that could not be persisted; inspect the target state before retrying because the remote outcome is unknown"
	RunningHint                  = "Wait 3 seconds, then call get_connector_action_request again. Use the connector-specific read or recovery actions when the connector exposes them."
	MissingPermissionError       = "This token is not allowed to run this connector action for the selected target/profile"
	TerminalPersistenceErrorText = "connector action may have been dispatched, but its final state could not be persisted; inspect request history before retrying"
)

type Call struct {
	Source         string
	TokenID        int64
	TargetRef      string
	ActionName     string
	Input          map[string]any
	Reason         string
	IdempotencyKey string
}

type CallResult struct {
	Request    connectortargets.ActionRequest
	Permission connectortargets.ActionPermission
	Result     connectors.ActionResult
	Replayed   bool
}

type ExecutionOptions struct {
	Permission              connectortargets.ActionPermission
	RequiredPermissionRule  connectortargets.ActionPermissionRule
	UnsupportedRunningError string
	ApprovalPendingError    string
	FollowupTool            string
}

type ExecutionEnvelope struct {
	Input                map[string]any `json:"input"`
	Payload              map[string]any `json:"payload"`
	ApprovalPreview      map[string]any `json:"approval_preview,omitempty"`
	SensitiveInputFields []string       `json:"sensitive_input_fields,omitempty"`
	Reason               string         `json:"reason,omitempty"`
}

type ExecutionSnapshot struct {
	Secrets            map[string]any
	CredentialBoundary actionresult.CredentialBoundary
}

type TerminalPersistenceError struct {
	RequestID int64
	Err       error
}

func (err *TerminalPersistenceError) Error() string { return TerminalPersistenceErrorText }
func (err *TerminalPersistenceError) Unwrap() error { return err.Err }

func NewTerminalPersistenceError(requestID int64, err error) error {
	if err == nil {
		return nil
	}
	return &TerminalPersistenceError{RequestID: requestID, Err: err}
}
