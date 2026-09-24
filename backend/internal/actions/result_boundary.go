package actions

import (
	"context"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type CredentialBoundary = actionresult.CredentialBoundary
type Redactor = actionresult.Redactor
type Response = actionresult.Response

const (
	CredentialRedactionMarker = actionresult.CredentialRedactionMarker
	MaxStringBytes            = actionresult.MaxStringBytes
)

func NewCredentialBoundary(secrets map[string]any) CredentialBoundary {
	return actionresult.NewCredentialBoundary(secrets)
}

func CombinedCredentialBoundary(secretSets ...map[string]any) CredentialBoundary {
	return actionresult.CombinedCredentialBoundary(secretSets...)
}

func NewRedactor(
	persistText func(context.Context, string) string,
	capabilityText func(context.Context, string) string,
	inputBytes int,
) (*Redactor, error) {
	return actionresult.NewRedactor(persistText, capabilityText, inputBytes)
}

func SensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	return actionresult.SensitiveOutputFields(hints...)
}

func FromRequest(request connectortargets.ActionRequest, runningHint string) Response {
	return actionresult.FromRequest(request, runningHint)
}

func FromResult(
	request connectortargets.ActionRequest,
	result connectors.ActionResult,
	runningHint string,
) Response {
	return actionresult.FromResult(request, result, runningHint)
}

func Withhold(response *Response) {
	actionresult.Withhold(response)
}

func ResponseRunningHint(request connectortargets.ActionRequest, resolve func(connectortargets.ActionRequest) string) string {
	if request.Status != connectors.ResultRunning {
		return ""
	}
	if resolve != nil {
		if hint := strings.TrimSpace(resolve(request)); hint != "" {
			return hint
		}
	}
	return "Wait 3 seconds, then call get_connector_action_request again until this request is completed, failed, canceled, stale, or error."
}
