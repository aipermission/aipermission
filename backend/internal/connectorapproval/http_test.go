package connectorapproval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/restcontract"
)

type previewWorkflow struct {
	preview  map[string]any
	runError error
}

func (workflow previewWorkflow) ApprovalPreview(context.Context, connectortargets.ActionRequest) (map[string]any, error) {
	return workflow.preview, nil
}

func (workflow previewWorkflow) RunPending(context.Context, int64, string) (connectortargets.ActionRequest, error) {
	return connectortargets.ActionRequest{}, workflow.runError
}

func (previewWorkflow) DeclinePending(context.Context, int64, string) (connectortargets.ActionRequest, error) {
	return connectortargets.ActionRequest{}, nil
}

func TestItemFromRequestAddsPollingContractOnlyWhilePending(t *testing.T) {
	pending := ItemFromRequest(connectortargets.ActionRequest{
		ID: 9, TargetID: 2, ProfileID: 3, ConnectorKind: "postgres",
		Status: connectors.ResultApprovalPending,
	})
	if pending.TargetRef != "postgres:2:3" || pending.RetryAfterSeconds != 3 || pending.AssistantHint != actions.ApprovalHint {
		t.Fatalf("pending item = %#v", pending)
	}

	completed := ItemFromRequest(connectortargets.ActionRequest{
		ID: 10, TargetID: 2, ProfileID: 3, ConnectorKind: "postgres",
		Status: connectors.ResultCompleted,
	})
	if completed.RetryAfterSeconds != 0 || completed.AssistantHint != "" {
		t.Fatalf("completed item contains polling contract: %#v", completed)
	}
}

func TestItemForResponseUsesFreshApprovalPreview(t *testing.T) {
	item, err := ItemForResponse(t.Context(), previewWorkflow{preview: map[string]any{"safe": "fresh"}}, connectortargets.ActionRequest{
		Preview: map[string]any{"stale": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Preview["safe"] != "fresh" || item.Preview["stale"] != nil {
		t.Fatalf("preview = %#v", item.Preview)
	}
}

func TestRunFailsClosedWhenMCPIsStoppedBeforeReadingBody(t *testing.T) {
	workflowCalls := 0
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Workflow: func() (Workflow, error) {
				workflowCalls++
				return nil, nil
			},
			MCPStarted: func() bool { return false },
			Redact:     func(_ context.Context, value string) string { return value },
		}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/api/connector-action-approvals/4/run", strings.NewReader("not-json"))
	request.SetPathValue("id", "4")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handlers.Run(response, request)

	if response.Code != http.StatusConflict || workflowCalls != 0 {
		t.Fatalf("status=%d workflow_calls=%d body=%s", response.Code, workflowCalls, response.Body.String())
	}
}

func TestHandlersRejectIncompleteScope(t *testing.T) {
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) { return Scope{}, true })
	request := httptest.NewRequest(http.MethodGet, "/api/connector-action-approvals", nil)
	response := httptest.NewRecorder()

	handlers.List(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRunPreservesOutcomeUnknownContract(t *testing.T) {
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Workflow: func() (Workflow, error) {
				return previewWorkflow{runError: actions.NewTerminalPersistenceError(42, context.DeadlineExceeded)}, nil
			},
			MCPStarted: func() bool { return true },
			Redact:     func(_ context.Context, value string) string { return value },
		}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/api/connector-action-approvals/42/run", nil)
	request.SetPathValue("id", "42")
	response := httptest.NewRecorder()

	handlers.Run(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if err := restcontract.ValidateTypedResponse(
		http.MethodPost,
		"/api/connector-action-approvals/{id}/run",
		response.Code,
		response.Body.Bytes(),
	); err != nil {
		t.Fatalf("outcome unknown contract: %v", err)
	}
	for _, expected := range []string{`"status":"outcome_unknown"`, `"request_id":42`, `"connector_action_persistence_unknown"`, `"Do not retry automatically`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("body %q does not contain %q", response.Body.String(), expected)
		}
	}
}
