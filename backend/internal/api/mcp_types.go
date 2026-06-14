package api

import (
	"time"
)

const (
	mcpInitialExecTimeout       = 45 * time.Second
	mcpBackgroundCommandTimeout = 30 * time.Minute
)

type mcpAuthContext struct {
	TokenID int64
	Name    string
	runtime *databaseRuntime
}

type commandRequestRecord struct {
	ID                int64                  `json:"id"`
	TokenID           *int64                 `json:"token_id,omitempty"`
	TokenName         string                 `json:"token_name,omitempty"`
	RuntimeID         int64                  `json:"runtime_id"`
	TargetName        string                 `json:"target_name"`
	Source            string                 `json:"source"`
	Command           string                 `json:"command"`
	Reason            string                 `json:"reason"`
	Status            string                 `json:"status"`
	TrackingReason    string                 `json:"tracking_reason,omitempty"`
	OutputTruncated   bool                   `json:"output_truncated,omitempty"`
	Stdout            string                 `json:"stdout,omitempty"`
	Stderr            string                 `json:"stderr,omitempty"`
	ExitCode          *int                   `json:"exit_code,omitempty"`
	SessionID         *int64                 `json:"session_id,omitempty"`
	UserNote          *string                `json:"user_note,omitempty"`
	Error             string                 `json:"error,omitempty"`
	CreatedAt         string                 `json:"created_at"`
	CompletedAt       *string                `json:"completed_at,omitempty"`
	RetryAfterSeconds int                    `json:"retry_after_seconds,omitempty"`
	AssistantHint     string                 `json:"assistant_hint,omitempty"`
	PolicyWarnings    []commandPolicyWarning `json:"policy_warnings,omitempty"`
}

type historyLabelRecord struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}
