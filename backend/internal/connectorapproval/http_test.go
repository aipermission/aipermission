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

type requestStoreStub struct {
	item connectortargets.ActionRequest
	err  error
}

func (store requestStoreStub) ListActionRequests(context.Context, connectortargets.ActionRequestFilter) ([]connectortargets.ActionRequest, error) {
	return []connectortargets.ActionRequest{store.item}, store.err
}

func (store requestStoreStub) GetActionRequest(context.Context, int64) (connectortargets.ActionRequest, error) {
	return store.item, store.err
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
	const contextHash = "approval-context"
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Requests: requestStoreStub{item: connectortargets.ActionRequest{ID: 42, ApprovalContextHash: contextHash}},
			Workflow: func() (Workflow, error) {
				return previewWorkflow{runError: actions.NewTerminalPersistenceError(42, context.DeadlineExceeded)}, nil
			},
			MCPStarted: func() bool { return true },
			Redact:     func(_ context.Context, value string) string { return value },
		}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/api/connector-action-approvals/42/run", strings.NewReader(`{"approval_context_hash":"`+contextHash+`"}`))
	request.Header.Set("Content-Type", "application/json")
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

func TestRunRejectsApprovalContextThatWasNotDisplayed(t *testing.T) {
	called := false
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Requests: requestStoreStub{item: connectortargets.ActionRequest{ID: 42, ApprovalContextHash: "current-context"}},
			Workflow: func() (Workflow, error) {
				called = true
				return previewWorkflow{}, nil
			},
			MCPStarted: func() bool { return true },
			Redact:     func(_ context.Context, value string) string { return value },
		}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/api/connector-action-approvals/42/run", strings.NewReader(`{"approval_context_hash":"displayed-context"}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", "42")
	response := httptest.NewRecorder()

	handlers.Run(response, request)

	if response.Code != http.StatusConflict || called || !strings.Contains(response.Body.String(), `"code":"approval_context_changed"`) {
		t.Fatalf("status=%d workflow_called=%t body=%s", response.Code, called, response.Body.String())
	}
}

func TestKnownPendingConflictUsesStableErrorCode(t *testing.T) {
	response := httptest.NewRecorder()
	if !writeKnownError(response, connectortargets.ErrActionRequestNotPending) {
		t.Fatal("pending conflict was not handled")
	}
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"approval_not_pending"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestKnownCapacityErrorUsesRetryableBackpressureContract(t *testing.T) {
	response := httptest.NewRecorder()
	if !writeKnownError(response, connectortargets.ErrActionRequestCapacity) {
		t.Fatal("capacity error was not handled")
	}
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "60" ||
		!strings.Contains(response.Body.String(), `"code":"connector_action_backpressure"`) {
		t.Fatalf("response = %d retry=%q body=%s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
	if err := restcontract.ValidateTypedResponse(
		http.MethodPost,
		"/api/connector-action-approvals/{id}/run",
		response.Code,
		response.Body.Bytes(),
	); err != nil {
		t.Fatalf("backpressure contract: %v", err)
	}
}
