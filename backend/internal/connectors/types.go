package connectors

import "time"

// RiskLevel classifies an action for display, approval previews, and policy
// presets. It is not a security boundary by itself.
type RiskLevel string

const (
	RiskRead                RiskLevel = "read"
	RiskWrite               RiskLevel = "write"
	RiskDestructive         RiskLevel = "destructive"
	RiskCredentialSensitive RiskLevel = "credential_sensitive"
)

// ResultStatus is the generic lifecycle state for connector action requests.
type ResultStatus string

const (
	ResultCompleted       ResultStatus = "completed"
	ResultFailed          ResultStatus = "failed"
	ResultCanceled        ResultStatus = "canceled"
	ResultRunning         ResultStatus = "running"
	ResultApprovalPending ResultStatus = "approval_pending"
	ResultBlocked         ResultStatus = "blocked"
	ResultStale           ResultStatus = "stale"
	ResultDeclined        ResultStatus = "declined"
	ResultError           ResultStatus = "error"
)

// ConnectorHelp is AI-readable guidance for one target. It may mention actions,
// but GetActionList remains the executable contract.
type ConnectorHelp struct {
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Usage       []string `json:"usage,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	Connector   string   `json:"connector"`
	ConnectorID string   `json:"connector_id"`
}

// TargetView is the public, non-secret view of a configured target.
type TargetView struct {
	ID            int64          `json:"id"`
	Ref           string         `json:"ref"`
	ConnectorKind string         `json:"connector_kind"`
	Name          string         `json:"name"`
	Config        map[string]any `json:"config,omitempty"`
}

// CredentialProfileView is the public, non-secret view of a credential profile
// bound to a target.
type CredentialProfileView struct {
	ID            int64          `json:"id"`
	TargetID      int64          `json:"target_id"`
	ConnectorKind string         `json:"connector_kind"`
	Kind          string         `json:"kind"`
	Label         string         `json:"label"`
	Public        map[string]any `json:"public,omitempty"`
}

// ActionDefinition is the machine-readable action contract returned by a
// connector.
type ActionDefinition struct {
	Name        string     `json:"name"`
	Label       string     `json:"label"`
	Description string     `json:"description"`
	Category    string     `json:"category,omitempty"`
	Risk        RiskLevel  `json:"risk"`
	InputSchema Schema     `json:"input_schema"`
	OutputHint  OutputHint `json:"output_hint,omitempty"`
}

// ActionRequest is a side-effect-free request to prepare a target action.
type ActionRequest struct {
	Source  string                `json:"source,omitempty"`
	Target  TargetView            `json:"target"`
	Profile CredentialProfileView `json:"profile"`

	ActionName string         `json:"action_name"`
	Input      map[string]any `json:"input,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	CreatedAt  time.Time      `json:"created_at,omitempty"`
}

// PreparedAction is the immutable execution payload produced before permission
// evaluation and approval. Preview is safe for display; Payload is the raw
// connector-specific execution payload that core may encrypt for later use.
type PreparedAction struct {
	ConnectorKind string    `json:"connector_kind"`
	TargetRef     string    `json:"target_ref"`
	ProfileID     int64     `json:"profile_id"`
	ActionName    string    `json:"action_name"`
	Risk          RiskLevel `json:"risk"`

	Title   string         `json:"title"`
	Summary string         `json:"summary"`
	Preview map[string]any `json:"preview,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`

	ContextMaterial map[string]any `json:"context_material,omitempty"`
}

// ActionHandles points callers at follow-up resources for asynchronous actions.
type ActionHandles struct {
	RequestID    int64  `json:"request_id,omitempty"`
	SessionID    int64  `json:"session_id,omitempty"`
	BatchID      int64  `json:"batch_id,omitempty"`
	FollowupTool string `json:"followup_tool,omitempty"`
}

// ActionResult is the connector execution result after core has allowed the
// action to run.
type ActionResult struct {
	Status      ResultStatus   `json:"status"`
	Output      any            `json:"output,omitempty"`
	DisplayText string         `json:"display_text,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Handles     ActionHandles  `json:"handles,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// TestStatus is a structured connection-test status.
type TestStatus string

const (
	TestOK               TestStatus = "ok"
	TestFailedAuth       TestStatus = "failed_auth"
	TestFailedNetwork    TestStatus = "failed_network"
	TestFailedTLS        TestStatus = "failed_tls"
	TestFailedPermission TestStatus = "failed_permission"
	TestUnknownError     TestStatus = "unknown_error"
)

// TestResult describes an optional connector connection test.
type TestResult struct {
	Status  TestStatus     `json:"status"`
	Message string         `json:"message,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}
