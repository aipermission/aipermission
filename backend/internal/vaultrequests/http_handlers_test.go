package vaultrequests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestHTTPHandlersListAndRunPendingRequest(t *testing.T) {
	harness := newRuntimeHarness(t)
	created := harness.call(t, "http-run")
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return testApprovalHTTPScope(harness), true
	})

	listed := httptest.NewRecorder()
	handlers.List(listed, httptest.NewRequest(http.MethodGet, "/api/vault-action-approvals?status=approval_pending", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"status":"approval_pending"`) {
		t.Fatalf("list response = %d %s", listed.Code, listed.Body.String())
	}

	run := approvalHTTPRequest(http.MethodPost, `/api/vault-action-approvals/1/run`, `{"user_note":" approved ","approval_context_hash":"`+created.Request.ApprovalContextHash+`"}`)
	run.SetPathValue("id", strconv.FormatInt(created.Request.ID, 10))
	response := httptest.NewRecorder()
	handlers.Run(response, run)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"completed"`) ||
		!strings.Contains(response.Body.String(), `"user_note":"approved"`) {
		t.Fatalf("run response = %d %s", response.Code, response.Body.String())
	}
}

func TestHTTPHandlersPreserveStoppedMCPPrecedence(t *testing.T) {
	harness := newRuntimeHarness(t)
	harness.mcpStarted = false
	runtimeCalls := 0
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{
			MCPStarted: func() bool { return false },
			Runtime: func(context.Context) (Application, error) {
				runtimeCalls++
				return harness.runtime, nil
			},
		}, true
	})
	request := approvalHTTPRequest(http.MethodPost, "/api/vault-action-approvals/1/run", `{"unknown":true}`)
	request.SetPathValue("id", "1")
	response := httptest.NewRecorder()
	handlers.Run(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), ErrMCPExecutionStopped.Error()) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if runtimeCalls != 0 {
		t.Fatalf("stopped MCP constructed runtime %d times", runtimeCalls)
	}
}

func TestHTTPHandlersValidateDecisionIDBeforeResolvingWorkspace(t *testing.T) {
	scopeCalls := 0
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		scopeCalls++
		return HTTPScope{}, false
	})
	for _, run := range []func(http.ResponseWriter, *http.Request){handlers.Run, handlers.Decline} {
		request := httptest.NewRequest(http.MethodPost, "/api/vault-action-approvals/nope", nil)
		request.SetPathValue("id", "nope")
		response := httptest.NewRecorder()
		run(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", response.Code)
		}
	}
	if scopeCalls != 0 {
		t.Fatalf("scope resolved %d times for invalid IDs", scopeCalls)
	}
}

func TestHTTPHandlersDeclineAndValidateNotes(t *testing.T) {
	harness := newRuntimeHarness(t)
	created := harness.call(t, "http-decline")
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return testApprovalHTTPScope(harness), true
	})

	decline := approvalHTTPRequest(http.MethodPost, "/api/vault-action-approvals/1/decline", `{"approval_context_hash":"`+created.Request.ApprovalContextHash+`"}`)
	decline.SetPathValue("id", strconv.FormatInt(created.Request.ID, 10))
	response := httptest.NewRecorder()
	handlers.Decline(response, decline)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"declined"`) {
		t.Fatalf("decline response = %d %s", response.Code, response.Body.String())
	}

	created = harness.call(t, "http-long-note")
	tooLong := approvalHTTPRequest(
		http.MethodPost, "/api/vault-action-approvals/1/decline",
		`{"user_note":"`+strings.Repeat("x", maxUserNoteBytes+1)+`"}`,
	)
	tooLong.SetPathValue("id", strconv.FormatInt(created.Request.ID, 10))
	invalid := httptest.NewRecorder()
	handlers.Decline(invalid, tooLong)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("long note response = %d %s", invalid.Code, invalid.Body.String())
	}
}

func TestHTTPHandlersRejectApprovalContextThatWasNotDisplayed(t *testing.T) {
	harness := newRuntimeHarness(t)
	created := harness.call(t, "http-stale-context")
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return testApprovalHTTPScope(harness), true
	})
	request := approvalHTTPRequest(http.MethodPost, "/api/vault-action-approvals/1/run", `{"approval_context_hash":"different-context"}`)
	request.SetPathValue("id", strconv.FormatInt(created.Request.ID, 10))
	response := httptest.NewRecorder()

	handlers.Run(response, request)

	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"approval_context_changed"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	stored, err := harness.runtime.Get(t.Context(), created.Request.ID)
	if err != nil || stored.Status != StatusApprovalPending {
		t.Fatalf("stored request = %#v err=%v", stored, err)
	}
}

func TestDecisionPendingConflictUsesStableErrorCode(t *testing.T) {
	response := httptest.NewRecorder()
	if !writeDecisionHTTPError(response, ErrNotPending) {
		t.Fatal("pending conflict was not handled")
	}
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"approval_not_pending"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func approvalHTTPRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func testApprovalHTTPScope(harness *runtimeHarness) HTTPScope {
	return HTTPScope{
		MCPStarted: func() bool { return harness.mcpStarted },
		Runtime: func(context.Context) (Application, error) {
			return harness.runtime, nil
		},
	}
}
