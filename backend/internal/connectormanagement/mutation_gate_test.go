package connectormanagement

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestAcquireLifecycleMutationReleasesPartialAcquisition(t *testing.T) {
	released := 0
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	release, ok := acquireLifecycleMutation(response, request, func(context.Context) (func(), error) {
		return func() { released++ }, errors.New("acquisition canceled")
	}, "mutation was canceled")
	if ok || release != nil {
		t.Fatalf("acquisition unexpectedly succeeded: ok=%t release=%v", ok, release != nil)
	}
	if released != 1 || response.Code != http.StatusRequestTimeout {
		t.Fatalf("released=%d status=%d", released, response.Code)
	}
}

func TestCommittedLifecycleErrorIsExplicitlyNonRetryable(t *testing.T) {
	response := httptest.NewRecorder()
	WriteCommittedLifecycleError(response, errors.Join(
		connectortargets.ErrLifecycleFinalizationPending,
		errors.New("private cleanup detail"),
	))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"code":"connector_lifecycle_finalization_pending"`) ||
		!strings.Contains(body, "mutation committed") || strings.Contains(body, "private cleanup detail") {
		t.Fatalf("response = %s", body)
	}
}

func TestCommittedLifecycleErrorTreatsEveryPostCommitFailureAsNonRetryable(t *testing.T) {
	response := httptest.NewRecorder()
	WriteCommittedLifecycleError(response, errors.New("finalization store unavailable"))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"connector_lifecycle_finalization_pending"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestAcquireLifecycleMutationMarksExactAdmission(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	identity := &connectors.DeliveryAdmissionIdentity{}
	release, ok := acquireLifecycleMutation(response, request, func(context.Context) (func(), error) {
		return func() {}, nil
	}, identity, "mutation was canceled")
	if !ok || release == nil {
		t.Fatalf("acquisition failed: ok=%t release=%v", ok, release != nil)
	}
	defer release()
	if !connectors.DeliveryAdmissionHeld(request.Context(), identity) {
		t.Fatal("request context does not carry lifecycle admission")
	}
	if connectors.DeliveryAdmissionHeld(request.Context(), &connectors.DeliveryAdmissionIdentity{}) {
		t.Fatal("request context admitted a different coordinator")
	}
}

func TestFinalizeLifecycleMutationSurvivesRequestCancellation(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	var observedDeadline time.Time
	err := finalizeLifecycleMutation(requestCtx, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			t.Fatalf("finalization inherited request cancellation: %v", err)
		}
		var ok bool
		observedDeadline, ok = ctx.Deadline()
		if !ok {
			t.Fatal("finalization has no deadline")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	remaining := time.Until(observedDeadline)
	if remaining <= 0 || remaining > lifecycleFinalizationTimeout {
		t.Fatalf("finalization deadline remaining = %v", remaining)
	}
}
