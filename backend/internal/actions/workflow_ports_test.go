package actions

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type workflowTestTokenReader struct{}

func (workflowTestTokenReader) Get(context.Context, int64, time.Time) (AuthorizationToken, error) {
	return AuthorizationToken{}, ErrTokenNotFound
}

type workflowTestDelivery struct {
	err   error
	calls int
}

func (d *workflowTestDelivery) Acquire(context.Context) (func(), error) {
	d.calls++
	if d.err != nil {
		return nil, d.err
	}
	return func() {}, nil
}

type workflowTestSealedRecords struct{}

func (workflowTestSealedRecords) SealActionRequest(int64, ExecutionEnvelope) (string, error) {
	return "sealed", nil
}

func (workflowTestSealedRecords) OpenActionRequest(int64, string) (ExecutionEnvelope, error) {
	return ExecutionEnvelope{}, nil
}

func (workflowTestSealedRecords) OpenCredentialProfile(int64, string) (map[string]any, error) {
	return map[string]any{}, nil
}

type workflowTestMutations struct{}

func (workflowTestMutations) WithMutation(context.Context, string, *int64, int64, string, func() any, func(*sql.Tx) error) error {
	return nil
}

func (workflowTestMutations) WithTransaction(context.Context, func(*sql.Tx, AuditAppender) error) error {
	return nil
}

func (workflowTestMutations) Observe(context.Context, string, *int64, int64, string, any) {}

type workflowTestRunningActions struct{}

func (workflowTestRunningActions) SupportsRunning(PreparedRequest) bool { return false }

func (workflowTestRunningActions) FinishRunning(context.Context, int64, PreparedRequest, executionprincipal.Principal, connectors.ActionHandles) {
}

func workflowTestDependencies(delivery DeliveryGate) RuntimeDependencies {
	redactor, err := actionresult.NewRedactor(
		func(_ context.Context, value string) string { return value },
		func(_ context.Context, value string) string { return value },
		1024,
	)
	if err != nil {
		panic(err)
	}
	return RuntimeDependencies{
		Database: &sql.DB{},
		Tokens:   workflowTestTokenReader{},
		Registry: connectors.NewRegistry(),
		Targets:  &fakeResolver{},
		IdentityTag: func(value []byte) (string, error) {
			return IdentityTag(make([]byte, 32), value)
		},
		Delivery:      delivery,
		MCPStarted:    func() bool { return true },
		Identity:      func() (string, string, error) { return "workspace", "runtime", nil },
		Redactor:      redactor,
		SealedRecords: workflowTestSealedRecords{},
		Mutations:     workflowTestMutations{},
		Capabilities: func(string, []ResolvedDependency) connectors.RuntimeCapabilityResolver {
			return nil
		},
		RunningActions: workflowTestRunningActions{},
	}
}

func TestNewRuntimeRejectsMissingIdentityTagger(t *testing.T) {
	dependencies := workflowTestDependencies(&workflowTestDelivery{})
	dependencies.IdentityTag = nil
	if _, err := NewRuntime(dependencies); !errors.Is(err, ErrWorkflowUnavailable) {
		t.Fatalf("NewRuntime() error = %v, want %v", err, ErrWorkflowUnavailable)
	}
}

func TestStopFinalizersCancelsDrainsAndClosesAdmission(t *testing.T) {
	runtime, err := NewRuntime(workflowTestDependencies(&workflowTestDelivery{}))
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finished := make(chan struct{})
	if !runtime.launchFinalizer(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(finished)
	}) {
		t.Fatal("finalizer was not admitted before shutdown")
	}
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := runtime.StopFinalizers(ctx); err != nil {
		t.Fatalf("StopFinalizers() error = %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("StopFinalizers returned before the active finalizer exited")
	}
	if runtime.launchFinalizer(func(context.Context) {}) {
		t.Fatal("finalizer was admitted after shutdown")
	}
}

func TestStopFinalizersReportsBoundedWaitWithoutLosingDrain(t *testing.T) {
	runtime, err := NewRuntime(workflowTestDependencies(&workflowTestDelivery{}))
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	if !runtime.launchFinalizer(func(context.Context) { <-release }) {
		t.Fatal("finalizer was not admitted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := runtime.StopFinalizers(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StopFinalizers() error = %v, want deadline exceeded", err)
	}
	close(release)
	drainCtx, drainCancel := context.WithTimeout(t.Context(), time.Second)
	defer drainCancel()
	if err := runtime.StopFinalizers(drainCtx); err != nil {
		t.Fatalf("second StopFinalizers() did not observe eventual drain: %v", err)
	}
}

func TestApprovalPreviewAcquiresDeliveryLeaseBeforeOpeningSealedData(t *testing.T) {
	want := errors.New("workspace is locking")
	delivery := &workflowTestDelivery{err: want}
	runtime, err := NewRuntime(workflowTestDependencies(delivery))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.ApprovalPreview(t.Context(), connectortargets.ActionRequest{
		ID: 7, Status: connectors.ResultApprovalPending, EncryptedPayloadJSON: "sealed",
	})
	if !errors.Is(err, want) || delivery.calls != 1 {
		t.Fatalf("ApprovalPreview() error = %v, lease calls = %d", err, delivery.calls)
	}
}

func TestDeclinePendingAcquiresDeliveryLeaseBeforeRedaction(t *testing.T) {
	want := errors.New("workspace is locking")
	delivery := &workflowTestDelivery{err: want}
	runtime, err := NewRuntime(workflowTestDependencies(delivery))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.DeclinePending(t.Context(), 7, "not now")
	if !errors.Is(err, want) || delivery.calls != 1 {
		t.Fatalf("DeclinePending() error = %v, lease calls = %d", err, delivery.calls)
	}
}

func TestValidateApprovalNoteUsesEncodedByteLimit(t *testing.T) {
	if err := ValidateApprovalNote(strings.Repeat("a", approvalNoteMaxBytes)); err != nil {
		t.Fatalf("limit-sized note rejected: %v", err)
	}
	if err := ValidateApprovalNote(strings.Repeat("\xc5\x9f", approvalNoteMaxBytes/2+1)); err == nil {
		t.Fatal("over-limit multi-byte note accepted")
	}
}
