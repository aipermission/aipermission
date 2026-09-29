package vaultrequests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

func TestMCPCallPreservesResolvedProjectReference(t *testing.T) {
	for _, action := range []string{ActionGenerateItem, ActionRestartSession} {
		for _, always := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/always=%v", action, always), func(t *testing.T) {
				harness := newRuntimeHarness(t)
				harness.runAlways = always
				refs := []string{
					"slug:vault-runtime", fmt.Sprintf("id:%d", harness.projectID),
					"vault-runtime", harness.projectRef, fmt.Sprintf("  id:+%d  ", harness.projectID),
				}
				if action == ActionRestartSession {
					refs[0], refs[1] = refs[1], refs[0]
				}
				harness.projectRef = refs[0]
				handlers := referenceTestHandlers(harness)
				input := map[string]any{"name": "PROJECT_KEY", "generator_kind": "hex_32"}
				if action == ActionRestartSession {
					input = map[string]any{
						"target_ref": "ssh:1:1",
						"items":      []map[string]any{{"item_id": 1, "source_project_id": harness.projectID}},
					}
				}
				var requestID any
				for _, ref := range refs {
					response := referenceTestCall(t, handlers, MCPActionCallRequest{
						ProjectRef: ref, ActionName: action, Input: input,
						Reason: "project reference regression", IdempotencyKey: "reference-replay",
					})
					if response.Code != http.StatusOK {
						t.Fatalf("ref %q: %d %s", ref, response.Code, response.Body.String())
					}
					var payload map[string]any
					if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
						t.Fatal(err)
					}
					if payload["project_ref"] != strings.TrimSpace(ref) || payload["action_name"] != action || payload["secret_values_returned"] != false {
						t.Fatalf("ref %q: unexpected response %#v", ref, payload)
					}
					status := StatusApprovalPending
					if always {
						status = StatusCompleted
					}
					if payload["status"] != status {
						t.Fatalf("status = %v, want %s", payload["status"], status)
					}
					if requestID != nil && payload["request_id"] != requestID {
						t.Fatal("project alias replay created another request")
					}
					requestID = payload["request_id"]
				}
				wantExecutions := 0
				if always {
					wantExecutions = 1
				}
				if harness.prepareCalls != 1 || harness.executeCalls != wantExecutions {
					t.Fatalf("prepare=%d execute=%d", harness.prepareCalls, harness.executeCalls)
				}
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodGet, "/api/mcp/vault-action-requests/1", nil)
				request.SetPathValue("id", fmt.Sprintf("%.0f", requestID))
				handlers.GetRequest(response, request)
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"project_ref":"vault-runtime"`) {
					t.Fatalf("request read = %d %s", response.Code, response.Body.String())
				}
				if always {
					harness.authorization = OutputWithheld
					response = referenceTestCall(t, handlers, MCPActionCallRequest{
						ProjectRef: refs[0], ActionName: action, Input: input,
						Reason: "project reference regression", IdempotencyKey: "reference-replay",
					})
					var payload map[string]any
					if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
						t.Fatal(err)
					}
					if response.Code != http.StatusOK || payload["project_ref"] != refs[0] || payload["output_withheld"] != true || payload["output"] != nil {
						t.Fatalf("unauthorized output = %d %#v", response.Code, payload)
					}
					if harness.executeCalls != 1 {
						t.Fatal("withheld replay executed again")
					}
				}
			})
		}
	}
}

func TestMCPCallRejectsDifferentProjectOnReferenceReplay(t *testing.T) {
	harness := newRuntimeHarness(t)
	handlers := referenceTestHandlers(harness)
	input := MCPActionCallRequest{
		ProjectRef: harness.projectRef, ActionName: ActionGenerateItem,
		Input:  map[string]any{"name": "PROJECT_KEY", "generator_kind": "hex_32"},
		Reason: "project reference regression", IdempotencyKey: "reference-conflict",
	}
	if response := referenceTestCall(t, handlers, input); response.Code != http.StatusOK {
		t.Fatalf("initial call = %d %s", response.Code, response.Body.String())
	}
	other, err := projectstore.NewStore(harness.database).Create(t.Context(), "Other Project")
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{fmt.Sprintf("id:%d", other.ID), "slug:" + other.Slug} {
		input.ProjectRef = ref
		if response := referenceTestCall(t, handlers, input); response.Code != http.StatusConflict {
			t.Fatalf("different project %q = %d %s", ref, response.Code, response.Body.String())
		}
	}
	input.ProjectRef = "slug:missing-project"
	input.IdempotencyKey = "missing-project"
	if response := referenceTestCall(t, handlers, input); response.Code != http.StatusNotFound {
		t.Fatalf("missing project = %d %s", response.Code, response.Body.String())
	}
	if harness.prepareCalls != 1 || harness.executeCalls != 0 {
		t.Fatalf("invalid reference reached execution: prepare=%d execute=%d", harness.prepareCalls, harness.executeCalls)
	}
}

func referenceTestHandlers(harness *runtimeHarness) *MCPHTTPHandlers {
	return NewMCPHTTPHandlers(func(http.ResponseWriter, *http.Request) (MCPHTTPScope, bool) {
		return MCPHTTPScope{
			TokenID: harness.tokenID, MCPStarted: func() bool { return true },
			Runtime: func(context.Context) (Application, error) { return harness.runtime, nil },
		}, true
	})
}

func referenceTestCall(t *testing.T, handlers *MCPHTTPHandlers, input MCPActionCallRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/mcp/vault-actions/call", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handlers.Call(response, request)
	return response
}
