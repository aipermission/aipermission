package gatewayconnectormanagement

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type targetOperationAdapter struct {
	called    bool
	operation string
}

func (adapter *targetOperationAdapter) RunTargetOperation(
	_ context.Context,
	_ connectorapi.TargetOperationGateway,
	_ connectorapi.ConnectorDataRuntime,
	_ connectorapi.Target,
	operation string,
	_ any,
) (connectors.ManagementResponse, error) {
	adapter.called = true
	adapter.operation = operation
	return connectors.ManagementResponse{
		StatusCode:      http.StatusOK,
		Payload:         map[string]any{"operation": operation, "stdout": "docker-log-private-key"},
		SensitiveValues: []string{"docker-log-private-key"},
	}, nil
}

type targetOperationGateway struct{ targetDraftPeer }

func (targetOperationGateway) ConnectorWriteAudit(context.Context, string, *int64, int64, string, any) {
}

func TestTargetOperationHandlerDispatchesThroughWorkspacePorts(t *testing.T) {
	database, registry := targetDraftFixture(t)
	target := createTargetDeleteFixture(t, database)
	adapter := &targetOperationAdapter{}
	adapters := connectorapi.NewRegistry()
	if err := adapters.Register(targetDraftTestKind, adapter); err != nil {
		t.Fatal(err)
	}
	component := New(Dependencies{
		Active: func(http.ResponseWriter) (Workspace, bool) {
			return Workspace{
				Storage:     StoragePorts{Database: database, Registry: registry},
				Credentials: CredentialPorts{Runtime: targetManagementRuntime()},
				Adapters: TargetAdapterPorts{
					DataRuntime:      func(string) connectorapi.ConnectorDataRuntime { return targetDraftRuntime{} },
					OperationGateway: func(string, int64) connectorapi.TargetOperationGateway { return targetOperationGateway{} },
				},
			}, true
		},
		Adapters: adapters,
	})
	response := executeTargetOperation(t, component, target.ID, " inspect ")
	if response.Code != http.StatusOK || !adapter.called || adapter.operation != "inspect" || strings.Contains(response.Body.String(), "docker-log-private-key") {
		t.Fatalf("response=%d %s called=%t operation=%q", response.Code, response.Body.String(), adapter.called, adapter.operation)
	}
}

func TestTargetOperationHandlerRejectsUnsupportedOrIncompletePorts(t *testing.T) {
	database, registry := targetDraftFixture(t)
	target := createTargetDeleteFixture(t, database)
	adapters := connectorapi.NewRegistry()
	if err := adapters.Register(targetDraftTestKind, &targetOperationAdapter{}); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name       string
		adapters   *connectorapi.Registry
		workspace  Workspace
		targetID   int64
		wantStatus int
	}{
		{name: "unsupported adapter", adapters: connectorapi.NewRegistry(), workspace: Workspace{Storage: StoragePorts{Database: database, Registry: registry}}, targetID: target.ID, wantStatus: http.StatusBadRequest},
		{name: "missing ports", adapters: adapters, workspace: Workspace{Storage: StoragePorts{Database: database, Registry: registry}}, targetID: target.ID, wantStatus: http.StatusInternalServerError},
		{name: "unknown target", adapters: adapters, workspace: Workspace{Storage: StoragePorts{Database: database, Registry: registry}}, targetID: target.ID + 1000, wantStatus: http.StatusNotFound},
		{name: "missing storage", adapters: adapters, workspace: Workspace{}, targetID: target.ID, wantStatus: http.StatusInternalServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			component := New(Dependencies{
				Active:   func(http.ResponseWriter) (Workspace, bool) { return testCase.workspace, true },
				Adapters: testCase.adapters,
			})
			response := executeTargetOperation(t, component, testCase.targetID, "inspect")
			if response.Code != testCase.wantStatus {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func executeTargetOperation(t *testing.T, component *Component, targetID int64, operation string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/connector-targets/"+strconv.FormatInt(targetID, 10)+"/operations/run", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", strconv.FormatInt(targetID, 10))
	request.SetPathValue("operation", operation)
	component.HTTPHandlers().TargetOperation.Run(response, request)
	return response
}
