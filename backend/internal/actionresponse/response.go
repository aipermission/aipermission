// Package actionresponse owns the public projection of persisted connector
// action requests. Transport handlers decide whether a response may be
// delivered; this package decides how the safe projection is represented.
package actionresponse

import (
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const (
	ApprovalPendingHint = "Wait 3 seconds, then poll this connector action request until it is completed, failed, declined, stale, blocked, or outcome_unknown."
	OutcomeUnknownHint  = "Do not retry this action automatically. The operation may have been dispatched; inspect external state with a connector-specific read-only action first."
	WithheldHint        = "Current Vault session authorization no longer permits returning this connector output."
)

// Response is the transport-neutral public representation of one action
// request and its latest persisted result.
type Response struct {
	Status            string                 `json:"status"`
	RequestID         int64                  `json:"request_id,omitempty"`
	TargetRef         string                 `json:"target_ref"`
	TargetName        string                 `json:"target_name,omitempty"`
	ConnectorKind     string                 `json:"connector_kind"`
	ProfileLabel      string                 `json:"profile_label,omitempty"`
	ActionName        string                 `json:"action_name"`
	Input             map[string]any         `json:"input,omitempty"`
	Output            any                    `json:"output,omitempty"`
	DisplayText       string                 `json:"display_text,omitempty"`
	Error             string                 `json:"error,omitempty"`
	RetryPolicy       connectors.RetryPolicy `json:"retry_policy"`
	RetryAfterSeconds int                    `json:"retry_after_seconds,omitempty"`
	AssistantHint     string                 `json:"assistant_hint,omitempty"`
	OutputWithheld    bool                   `json:"output_withheld,omitempty"`
	Replayed          bool                   `json:"replayed,omitempty"`
}

// FromRequest projects a persisted request. runningHint is connector-owned and
// is used only while the request is still running.
func FromRequest(request connectortargets.ActionRequest, runningHint string) Response {
	response := Response{
		Status:        string(request.Status),
		RequestID:     request.ID,
		TargetRef:     connectortargets.ConnectorTargetRef(request.ConnectorKind, request.TargetID, request.ProfileID),
		TargetName:    request.TargetName,
		ConnectorKind: request.ConnectorKind,
		ProfileLabel:  request.ProfileLabel,
		ActionName:    request.ActionName,
		Input:         request.Input,
		Output:        request.Output,
		DisplayText:   request.DisplayText,
		Error:         request.Error,
		RetryPolicy:   request.RetryPolicy,
	}
	switch request.Status {
	case connectors.ResultApprovalPending:
		response.RetryAfterSeconds = 3
		response.AssistantHint = ApprovalPendingHint
	case connectors.ResultRunning:
		response.RetryAfterSeconds = 3
		response.AssistantHint = strings.TrimSpace(runningHint)
	case connectors.ResultOutcomeUnknown:
		response.AssistantHint = OutcomeUnknownHint
	}
	return response
}

// FromResult overlays the latest safe result fields on the canonical request
// projection. The persisted request remains the source of status and identity.
func FromResult(request connectortargets.ActionRequest, result connectors.ActionResult, runningHint string) Response {
	response := FromRequest(request, runningHint)
	response.Output = result.Output
	if result.DisplayText != "" {
		response.DisplayText = result.DisplayText
	}
	if result.Error != "" {
		response.Error = result.Error
	}
	return response
}

// Withhold removes request/result content after delivery authorization is
// lost while preserving status and safety guidance.
func Withhold(response *Response) {
	if response == nil {
		return
	}
	response.Input = nil
	response.Output = nil
	response.DisplayText = ""
	response.Error = ""
	response.OutputWithheld = true
	if response.AssistantHint == "" {
		response.AssistantHint = WithheldHint
	} else if !strings.Contains(response.AssistantHint, WithheldHint) {
		response.AssistantHint += " " + WithheldHint
	}
}
