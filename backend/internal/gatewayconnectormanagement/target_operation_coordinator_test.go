package gatewayconnectormanagement

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type coordinatorResponseRecorder struct {
	*httptest.ResponseRecorder
	probe      func() error
	probed     bool
	probeError error
}

type observedWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (ctx *observedWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.entered) })
	return ctx.Context.Done()
}

func (response *coordinatorResponseRecorder) Write(data []byte) (int, error) {
	if !response.probed {
		response.probed = true
		response.probeError = response.probe()
	}
	return response.ResponseRecorder.Write(data)
}

func TestTargetOperationRealAdmissionBlocksCompetingWriterThroughResponse(t *testing.T) {
	for _, exclusive := range []bool{false, true} {
		t.Run(map[bool]string{false: "delivery", true: "exclusive"}[exclusive], func(t *testing.T) {
			coordinator := &vaultsessions.DeliveryCoordinator{}
			adapter := &admittedTargetOperationAdapter{run: func(ctx context.Context, _ connectorapi.Target) (connectors.ManagementResponse, error) {
				if !connectors.DeliveryAdmissionHeld(ctx, coordinator.AdmissionIdentity()) {
					t.Fatal("real coordinator identity missing from dispatch")
				}
				return connectors.ManagementResponse{StatusCode: http.StatusOK, Payload: map[string]any{"ok": true}}, nil
			}}
			var capability connectorapi.Adapter = adapter
			operation := "inspect"
			if exclusive {
				capability = &exclusiveTargetOperationAdapter{adapter}
				operation = "reconcile"
			}
			component, workspace, targetID := operationAdmissionFixture(t, capability)
			workspace.Storage.Admission = coordinator.AdmissionIdentity()
			workspace.Storage.AcquireDelivery = coordinator.AcquireDelivery
			workspace.Storage.AcquireExclusive = coordinator.AcquireExclusive
			response := &coordinatorResponseRecorder{ResponseRecorder: httptest.NewRecorder(), probe: func() error {
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer cancel()
				release, err := coordinator.AcquireExclusive(ctx)
				if release != nil {
					release()
				}
				return err
			}}
			component.HTTPHandlers().TargetOperation.Run(response, targetOperationRequest(targetID, operation, `{}`))
			if response.Code != http.StatusOK || !response.probed || !errors.Is(response.probeError, context.DeadlineExceeded) {
				t.Fatalf("response %d %s, writer probe %t %v", response.Code, response.Body.String(), response.probed, response.probeError)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			release, err := coordinator.AcquireExclusive(ctx)
			if err != nil || release == nil {
				t.Fatalf("post-response admission leaked: %v", err)
			}
			release()
		})
	}
}

func TestTargetOperationCanceledRealWaitDoesNotDispatchOrLeakAdmission(t *testing.T) {
	coordinator := &vaultsessions.DeliveryCoordinator{}
	held, err := coordinator.AcquireExclusive(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(held)
	adapter := &admittedTargetOperationAdapter{run: func(context.Context, connectorapi.Target) (connectors.ManagementResponse, error) {
		t.Error("canceled real admission wait reached dispatch")
		return connectors.ManagementResponse{StatusCode: http.StatusOK, Payload: map[string]any{}}, nil
	}}
	component, workspace, targetID := operationAdmissionFixture(t, adapter)
	workspace.Storage.Admission = coordinator.AdmissionIdentity()
	waitEntered := make(chan struct{})
	workspace.Storage.AcquireDelivery = func(ctx context.Context) (func(), error) {
		return coordinator.AcquireDelivery(&observedWaitContext{Context: ctx, entered: waitEntered})
	}
	workspace.Storage.AcquireExclusive = coordinator.AcquireExclusive
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		component.HTTPHandlers().TargetOperation.Run(response, targetOperationRequest(targetID, "inspect", `{}`).WithContext(ctx))
	}()
	t.Cleanup(func() {
		cancel()
		held()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("request worker did not finish during cleanup")
		}
	})
	wait, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	select {
	case <-waitEntered:
	case <-wait.Done():
		t.Fatal("request did not reach admission")
	}
	select {
	case <-done:
		t.Fatal("operation bypassed the held writer")
	default:
	}
	cancel()
	select {
	case <-done:
	case <-wait.Done():
		t.Fatal("canceled admission did not return")
	}
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("response %d %s", response.Code, response.Body.String())
	}
	held()
	release, err := coordinator.AcquireExclusive(wait)
	if err != nil || release == nil {
		t.Fatalf("canceled admission leaked: %v", err)
	}
	release()
}
