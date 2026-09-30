package gatewayconnectormanagement

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type admittedTargetOperationAdapter struct {
	run func(context.Context, connectorapi.Target) (connectors.ManagementResponse, error)
}

func (adapter *admittedTargetOperationAdapter) RunTargetOperation(ctx context.Context, _ connectorapi.TargetOperationGateway, _ connectorapi.ConnectorDataRuntime, target connectorapi.Target, _ string, _ any) (connectors.ManagementResponse, error) {
	return adapter.run(ctx, target)
}

type exclusiveTargetOperationAdapter struct {
	*admittedTargetOperationAdapter
}

func (*exclusiveTargetOperationAdapter) RequiresTargetOperationExclusion(operation string) bool {
	return operation == "reconcile"
}

type admittedResponseRecorder struct {
	*httptest.ResponseRecorder
	held             *bool
	unadmittedWrites int
}

func (response *admittedResponseRecorder) WriteHeader(status int) {
	if !*response.held {
		response.unadmittedWrites++
	}
	response.ResponseRecorder.WriteHeader(status)
}

func (response *admittedResponseRecorder) Write(data []byte) (int, error) {
	if !*response.held {
		response.unadmittedWrites++
	}
	return response.ResponseRecorder.Write(data)
}

func operationAdmissionFixture(t *testing.T, adapter connectorapi.Adapter) (*Component, *Workspace, int64) {
	t.Helper()
	database, registry := targetDraftFixture(t)
	target := createTargetDeleteFixture(t, database)
	adapters := connectorapi.NewRegistry()
	if err := adapters.Register(targetDraftTestKind, adapter); err != nil {
		t.Fatal(err)
	}
	workspace := &Workspace{
		Storage: StoragePorts{
			Database: database, Registry: registry, Admission: &connectors.DeliveryAdmissionIdentity{},
			AcquireDelivery:  func(context.Context) (func(), error) { return func() {}, nil },
			AcquireExclusive: func(context.Context) (func(), error) { return func() {}, nil },
		},
		Credentials: CredentialPorts{Runtime: targetManagementRuntime()},
		Adapters: TargetAdapterPorts{
			DataRuntime:      func(string) connectorapi.ConnectorDataRuntime { return targetDraftRuntime{} },
			OperationGateway: func(string, int64) connectorapi.TargetOperationGateway { return targetOperationGateway{} },
		},
	}
	component := New(Dependencies{Active: func(http.ResponseWriter) (Workspace, bool) { return *workspace, true }, Adapters: adapters})
	return component, workspace, target.ID
}

func TestTargetOperationsHoldDeclaredAdmissionAcrossFreshSnapshotDispatchAndResponse(t *testing.T) {
	for _, scenario := range []struct {
		name, operation   string
		policy, exclusive bool
	}{
		{name: "default delivery", operation: "inspect"},
		{name: "policy delivery", operation: "inspect", policy: true},
		{name: "policy exclusive", operation: "reconcile", policy: true, exclusive: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			steps := []string{}
			held := false
			adapter := &admittedTargetOperationAdapter{}
			var capability connectorapi.Adapter = adapter
			if scenario.policy {
				capability = &exclusiveTargetOperationAdapter{adapter}
			}
			component, workspace, targetID := operationAdmissionFixture(t, capability)
			const freshName = "snapshot after lifecycle admission"
			lease := func(ctx context.Context) (func(), error) {
				if ctx.Err() != nil || held {
					t.Fatal("invalid or nested target admission")
				}
				held = true
				steps = append(steps, "admit")
				if _, err := workspace.Storage.Database.ExecContext(ctx, `UPDATE connector_targets SET name = ? WHERE id = ?`, freshName, targetID); err != nil {
					t.Fatal(err)
				}
				return func() { held = false; steps = append(steps, "release") }, nil
			}
			unexpected := func(context.Context) (func(), error) { t.Fatal("wrong lifecycle admission class"); return nil, nil }
			workspace.Storage.AcquireDelivery, workspace.Storage.AcquireExclusive = lease, unexpected
			if scenario.exclusive {
				workspace.Storage.AcquireDelivery, workspace.Storage.AcquireExclusive = unexpected, lease
			}
			workspace.Adapters.OperationGateway = func(string, int64) connectorapi.TargetOperationGateway {
				if !held {
					t.Fatal("gateway created before admission")
				}
				steps = append(steps, "gateway")
				return targetOperationGateway{}
			}
			workspace.Adapters.DataRuntime = func(string) connectorapi.ConnectorDataRuntime {
				if !held {
					t.Fatal("runtime created before admission")
				}
				steps = append(steps, "runtime")
				return targetDraftRuntime{}
			}
			adapter.run = func(ctx context.Context, target connectorapi.Target) (connectors.ManagementResponse, error) {
				if !held || !connectors.DeliveryAdmissionHeld(ctx, workspace.Storage.Admission) || target.Name != freshName || target.ID != targetID {
					t.Fatalf("dispatch escaped fresh admitted snapshot: held %t, target %#v", held, target)
				}
				steps = append(steps, "dispatch")
				return connectors.ManagementResponse{StatusCode: http.StatusOK, Payload: map[string]any{"stdout": "operation-fixture-secret"}, SensitiveValues: []string{"operation-fixture-secret"}}, nil
			}
			response := &admittedResponseRecorder{ResponseRecorder: httptest.NewRecorder(), held: &held}
			component.HTTPHandlers().TargetOperation.Run(response, targetOperationRequest(targetID, " "+scenario.operation+" ", `{}`))
			wantSteps := []string{"admit", "gateway", "runtime", "dispatch", "release"}
			if response.Code != http.StatusOK || held || response.unadmittedWrites != 0 || !reflect.DeepEqual(steps, wantSteps) || strings.Contains(response.Body.String(), "operation-fixture-secret") {
				t.Fatalf("response %d %s, held %t, steps %v", response.Code, response.Body.String(), held, steps)
			}
		})
	}
}

func TestTargetOperationRejectsInvalidBodyBeforeAcquiringLifecycleAdmission(t *testing.T) {
	adapter := &admittedTargetOperationAdapter{run: func(context.Context, connectorapi.Target) (connectors.ManagementResponse, error) {
		t.Fatal("invalid operation body reached dispatch")
		return connectors.ManagementResponse{}, nil
	}}
	component, workspace, targetID := operationAdmissionFixture(t, &exclusiveTargetOperationAdapter{adapter})
	unexpected := func(context.Context) (func(), error) { t.Fatal("invalid body acquired admission"); return nil, nil }
	workspace.Storage.AcquireDelivery, workspace.Storage.AcquireExclusive = unexpected, unexpected
	response := httptest.NewRecorder()
	component.HTTPHandlers().TargetOperation.Run(response, targetOperationRequest(targetID, "reconcile", `{`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response %d %s", response.Code, response.Body.String())
	}
}

func TestTargetOperationRefusesConnectorKindChangeWhileWaitingForAdmission(t *testing.T) {
	adapter := &admittedTargetOperationAdapter{run: func(context.Context, connectorapi.Target) (connectors.ManagementResponse, error) {
		t.Fatal("operation dispatched under the wrong connector admission policy")
		return connectors.ManagementResponse{}, nil
	}}
	component, workspace, targetID := operationAdmissionFixture(t, &exclusiveTargetOperationAdapter{adapter})
	releases := 0
	workspace.Storage.AcquireExclusive = func(ctx context.Context) (func(), error) {
		if _, err := workspace.Storage.Database.ExecContext(ctx, `UPDATE connector_targets SET connector_kind = ? WHERE id = ?`, "changed_kind", targetID); err != nil {
			t.Fatal(err)
		}
		return func() { releases++ }, nil
	}
	workspace.Adapters.DataRuntime = func(string) connectorapi.ConnectorDataRuntime {
		t.Fatal("runtime created for changed kind")
		return nil
	}
	workspace.Adapters.OperationGateway = func(string, int64) connectorapi.TargetOperationGateway {
		t.Fatal("gateway created for changed kind")
		return nil
	}
	response := executeTargetOperation(t, component, targetID, "reconcile")
	if response.Code != http.StatusConflict || releases != 1 {
		t.Fatalf("response %d %s, releases %d", response.Code, response.Body.String(), releases)
	}
}

func TestTargetOperationAdmissionFailuresDoNotConstructRuntimeOrDispatch(t *testing.T) {
	for _, exclusive := range []bool{false, true} {
		for _, scenario := range []struct {
			name         string
			setup        func(*Workspace, *int)
			status       int
			wantReleases int
		}{
			{name: "missing delivery port", setup: func(w *Workspace, _ *int) { w.Storage.AcquireDelivery, w.Storage.AcquireExclusive = nil, nil }, status: http.StatusInternalServerError},
			{name: "missing admission identity", setup: func(w *Workspace, _ *int) { w.Storage.Admission = nil }, status: http.StatusInternalServerError},
			{name: "canceled", setup: func(w *Workspace, _ *int) {
				lease := func(context.Context) (func(), error) { return nil, context.Canceled }
				w.Storage.AcquireDelivery, w.Storage.AcquireExclusive = lease, lease
			}, status: http.StatusRequestTimeout},
			{name: "partial acquisition", setup: func(w *Workspace, released *int) {
				lease := func(context.Context) (func(), error) {
					return func() { *released++ }, errors.New("private acquisition failure")
				}
				w.Storage.AcquireDelivery, w.Storage.AcquireExclusive = lease, lease
			}, status: http.StatusRequestTimeout, wantReleases: 1},
			{name: "missing release", setup: func(w *Workspace, _ *int) {
				lease := func(context.Context) (func(), error) { return nil, nil }
				w.Storage.AcquireDelivery, w.Storage.AcquireExclusive = lease, lease
			}, status: http.StatusInternalServerError},
		} {
			t.Run(scenario.name+map[bool]string{false: "/delivery", true: "/exclusive"}[exclusive], func(t *testing.T) {
				adapter := &admittedTargetOperationAdapter{run: func(context.Context, connectorapi.Target) (connectors.ManagementResponse, error) {
					t.Fatal("dispatch after failed admission")
					return connectors.ManagementResponse{}, nil
				}}
				var capability connectorapi.Adapter = adapter
				operation := "inspect"
				if exclusive {
					capability = &exclusiveTargetOperationAdapter{adapter}
					operation = "reconcile"
				}
				component, workspace, targetID := operationAdmissionFixture(t, capability)
				released := 0
				scenario.setup(workspace, &released)
				workspace.Adapters.DataRuntime = func(string) connectorapi.ConnectorDataRuntime { t.Fatal("runtime after failed admission"); return nil }
				workspace.Adapters.OperationGateway = func(string, int64) connectorapi.TargetOperationGateway {
					t.Fatal("gateway after failed admission")
					return nil
				}
				response := executeTargetOperation(t, component, targetID, operation)
				if response.Code != scenario.status || released != scenario.wantReleases || strings.Contains(response.Body.String(), "private acquisition failure") {
					t.Fatalf("response %d %s, releases %d", response.Code, response.Body.String(), released)
				}
			})
		}
	}
}

func TestTargetOperationReleasesAdmissionAfterTargetDisappearsOrRuntimeUnavailable(t *testing.T) {
	for _, scenario := range []string{"deleted target", "nil gateway", "nil runtime", "adapter error", "adapter panic"} {
		t.Run(scenario, func(t *testing.T) {
			calls, releases := 0, 0
			adapter := &admittedTargetOperationAdapter{run: func(context.Context, connectorapi.Target) (connectors.ManagementResponse, error) {
				calls++
				if scenario == "adapter panic" {
					panic("operator fixture panic")
				}
				return connectors.ManagementResponse{}, errors.New("operator fixture error")
			}}
			component, workspace, targetID := operationAdmissionFixture(t, adapter)
			workspace.Storage.AcquireDelivery = func(ctx context.Context) (func(), error) {
				if scenario == "deleted target" {
					if _, err := workspace.Storage.Database.ExecContext(ctx, `DELETE FROM connector_targets WHERE id = ?`, targetID); err != nil {
						t.Fatal(err)
					}
				}
				return func() { releases++ }, nil
			}
			if scenario == "nil gateway" {
				workspace.Adapters.OperationGateway = func(string, int64) connectorapi.TargetOperationGateway { return nil }
			}
			if scenario == "nil runtime" {
				workspace.Adapters.DataRuntime = func(string) connectorapi.ConnectorDataRuntime { return nil }
			}
			if scenario == "adapter panic" {
				func() {
					defer func() {
						if recover() != "operator fixture panic" {
							t.Fatal("adapter panic not propagated")
						}
					}()
					executeTargetOperation(t, component, targetID, "inspect")
				}()
			} else {
				response := executeTargetOperation(t, component, targetID, "inspect")
				wantStatus := http.StatusInternalServerError
				if scenario == "deleted target" {
					wantStatus = http.StatusNotFound
				}
				if response.Code != wantStatus {
					t.Fatalf("response %d %s", response.Code, response.Body.String())
				}
			}
			wantCalls := 0
			if scenario == "adapter error" || scenario == "adapter panic" {
				wantCalls = 1
			}
			if releases != 1 || calls != wantCalls {
				t.Fatalf("calls %d, releases %d", calls, releases)
			}
		})
	}
}
