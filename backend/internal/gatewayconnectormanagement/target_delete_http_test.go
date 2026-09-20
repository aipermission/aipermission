package gatewayconnectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type targetDeleteAdapter struct {
	called bool
	err    error
}

func (adapter *targetDeleteAdapter) DeleteTarget(_ connectorapi.TargetDeletionGateway, w http.ResponseWriter, _ *http.Request, _ connectorapi.TargetLifecycleRuntime, _ connectorapi.Target) error {
	adapter.called = true
	if adapter.err != nil {
		return adapter.err
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"adapter": true})
	return nil
}

type targetDeleteGateway struct{ targetDraftPeer }

func (targetDeleteGateway) ConnectorRestartConsoleSession(context.Context, connectorapi.Principal, int64, string) (connectorapi.ConsoleRestartResult, error) {
	return connectorapi.ConsoleRestartResult{}, nil
}
func (targetDeleteGateway) ConnectorDeleteTargetRecord(context.Context, connectorapi.Target, map[string]any) error {
	return nil
}
func (targetDeleteGateway) ConnectorFinalizeDeletedTarget(context.Context, connectorapi.Target, string, map[string]any) (int64, error) {
	return 0, nil
}

type targetDeleteRuntime struct{ targetDraftRuntime }

func (targetDeleteRuntime) ConnectorConsoleSessions() connectorapi.ConsoleSessionRuntime {
	return targetDeleteConsoleRuntime{}
}
func (targetDeleteRuntime) ConnectorLocalExecutionPrincipal() (connectorapi.Principal, error) {
	return connectorapi.Principal{}, nil
}

type targetDeleteConsoleRuntime struct{}

func (targetDeleteConsoleRuntime) EnsureReady(context.Context, connectorapi.Principal, int64) (connectorapi.ConsoleSessionHandle, error) {
	return connectorapi.ConsoleSessionHandle{}, nil
}
func (targetDeleteConsoleRuntime) Exec(context.Context, connectorapi.Principal, int64, string) (connectorapi.ConsoleExecResult, error) {
	return connectorapi.ConsoleExecResult{}, nil
}
func (targetDeleteConsoleRuntime) ActiveSnapshot(context.Context, connectorapi.Principal, int64) (connectorapi.ConsoleRecord, error) {
	return connectorapi.ConsoleRecord{}, nil
}
func (targetDeleteConsoleRuntime) WaitActive(context.Context, connectorapi.Principal, connectorapi.ConsoleSessionHandle) (connectorapi.ConsoleExecResult, error) {
	return connectorapi.ConsoleExecResult{}, nil
}
func (targetDeleteConsoleRuntime) InterruptActive(context.Context, connectorapi.Principal, connectorapi.ConsoleSessionHandle) error {
	return nil
}

func TestTargetDeleteHandlerOwnsGenericLifecycleUnderExclusiveLease(t *testing.T) {
	database, registry := targetDraftFixture(t)
	target := createTargetDeleteFixture(t, database)
	const lockedName = "delete fixture after lease"
	steps := []string{}
	component := New(Dependencies{
		Active: func(http.ResponseWriter) (Workspace, bool) {
			return Workspace{
				Storage: StoragePorts{
					Database: database, Registry: registry,
					AcquireExclusive: func(context.Context) (func(), error) {
						steps = append(steps, "acquire")
						if _, err := database.ExecContext(t.Context(), `UPDATE connector_targets SET name = ? WHERE id = ?`, lockedName, target.ID); err != nil {
							t.Fatalf("update target after lease acquisition: %v", err)
						}
						return func() { steps = append(steps, "release") }, nil
					},
				},
				Lifecycle: LifecyclePorts{
					DeleteTarget: func(_ context.Context, got Target, _ map[string]any) error {
						if got.ID != target.ID || got.Name != lockedName {
							t.Fatalf("target = %#v", got)
						}
						steps = append(steps, "delete")
						return nil
					},
					FinalizeTarget: func(_ context.Context, got Target, reason string) (int64, error) {
						if got.ID != target.ID || reason != deletedTargetStaleReason {
							t.Fatalf("finalize = %#v %q", got, reason)
						}
						steps = append(steps, "finalize")
						return 1, nil
					},
				},
			}, true
		},
		Adapters: connectorapi.NewRegistry(),
	})
	response := executeTargetDelete(t, component, target.ID)
	if response.Code != http.StatusOK || strings.Join(steps, ",") != "acquire,delete,finalize,release" {
		t.Fatalf("response=%d %s steps=%v", response.Code, response.Body.String(), steps)
	}
}

func TestTargetDeleteHandlerDispatchesConnectorAdapter(t *testing.T) {
	database, registry := targetDraftFixture(t)
	target := createTargetDeleteFixture(t, database)
	adapter := &targetDeleteAdapter{}
	adapters := connectorapi.NewRegistry()
	if err := adapters.Register(targetDraftTestKind, adapter); err != nil {
		t.Fatal(err)
	}
	component := New(Dependencies{
		Active: func(http.ResponseWriter) (Workspace, bool) {
			return Workspace{
				Storage: StoragePorts{Database: database, Registry: registry, AcquireExclusive: func(context.Context) (func(), error) { return func() {}, nil }},
				Adapters: TargetAdapterPorts{
					DeletionGateway:  func(string, int64) connectorapi.TargetDeletionGateway { return targetDeleteGateway{} },
					LifecycleRuntime: func(string) connectorapi.TargetLifecycleRuntime { return targetDeleteRuntime{} },
				},
			}, true
		},
		Adapters: adapters,
	})
	response := executeTargetDelete(t, component, target.ID)
	if response.Code != http.StatusOK || !adapter.called {
		t.Fatalf("response=%d %s called=%t", response.Code, response.Body.String(), adapter.called)
	}
}

func TestTargetDeleteHandlerMapsAdapterPostCommitFailureToConflict(t *testing.T) {
	database, registry := targetDraftFixture(t)
	target := createTargetDeleteFixture(t, database)
	adapter := &targetDeleteAdapter{err: errors.New("private finalization failure")}
	adapters := connectorapi.NewRegistry()
	if err := adapters.Register(targetDraftTestKind, adapter); err != nil {
		t.Fatal(err)
	}
	component := New(Dependencies{
		Active: func(http.ResponseWriter) (Workspace, bool) {
			return Workspace{
				Storage: StoragePorts{Database: database, Registry: registry, AcquireExclusive: func(context.Context) (func(), error) { return func() {}, nil }},
				Adapters: TargetAdapterPorts{
					DeletionGateway:  func(string, int64) connectorapi.TargetDeletionGateway { return targetDeleteGateway{} },
					LifecycleRuntime: func(string) connectorapi.TargetLifecycleRuntime { return targetDeleteRuntime{} },
				},
			}, true
		},
		Adapters: adapters,
	})
	response := executeTargetDelete(t, component, target.ID)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"connector_lifecycle_finalization_pending"`) || strings.Contains(response.Body.String(), "private finalization failure") {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
}

func TestTargetDeleteHandlerFailsClosedForLeaseFailures(t *testing.T) {
	database, registry := targetDraftFixture(t)
	target := createTargetDeleteFixture(t, database)
	releasedAfterError := false
	for _, testCase := range []struct {
		name   string
		lease  func(context.Context) (func(), error)
		status int
	}{
		{name: "canceled", lease: func(context.Context) (func(), error) { return nil, errors.New("canceled") }, status: http.StatusRequestTimeout},
		{name: "acquired then canceled", lease: func(context.Context) (func(), error) {
			return func() { releasedAfterError = true }, errors.New("canceled")
		}, status: http.StatusRequestTimeout},
		{name: "nil release", lease: func(context.Context) (func(), error) { return nil, nil }, status: http.StatusInternalServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			component := New(Dependencies{
				Active: func(http.ResponseWriter) (Workspace, bool) {
					return Workspace{Storage: StoragePorts{Database: database, Registry: registry, AcquireExclusive: testCase.lease}}, true
				},
				Adapters: connectorapi.NewRegistry(),
			})
			response := executeTargetDelete(t, component, target.ID)
			if response.Code != testCase.status {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
		})
	}
	if !releasedAfterError {
		t.Fatal("lease returned with an error was not released")
	}
}

func createTargetDeleteFixture(t *testing.T, database *sql.DB) connectortargets.Target {
	t.Helper()
	target, err := connectortargets.NewStore(database).CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: targetDraftTestKind, Name: "delete fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func executeTargetDelete(t *testing.T, component *Component, targetID int64) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/connector-targets/"+strconv.FormatInt(targetID, 10), nil)
	request.SetPathValue("id", strconv.FormatInt(targetID, 10))
	component.HTTPHandlers().TargetDelete.Delete(response, request)
	return response
}
