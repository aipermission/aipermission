package running

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type failingActionRequestFinisher struct {
	err         error
	hadDeadline bool
	finishes    int
}

func (f *failingActionRequestFinisher) ConnectorFinishActionRequest(ctx context.Context, _ int64, _ connectors.ResultStatus, _ any, _ string, _ string, _ ...connectors.OutputHint) (connectorapi.ActionRequest, error) {
	f.finishes++
	_, f.hadDeadline = ctx.Deadline()
	return connectorapi.ActionRequest{}, f.err
}

type cancelingConsoleSessions struct {
	cancel     context.CancelFunc
	interrupts int
}

func (*cancelingConsoleSessions) EnsureReady(context.Context, connectorapi.Principal, int64) (connectorapi.ConsoleSessionHandle, error) {
	panic("not used")
}

func (*cancelingConsoleSessions) Exec(context.Context, connectorapi.Principal, int64, string) (connectorapi.ConsoleExecResult, error) {
	panic("not used")
}

func (*cancelingConsoleSessions) ActiveSnapshot(context.Context, connectorapi.Principal, int64) (connectorapi.ConsoleRecord, error) {
	panic("not used")
}

func (sessions *cancelingConsoleSessions) WaitActive(ctx context.Context, _ connectorapi.Principal, _ connectorapi.ConsoleSessionHandle) (connectorapi.ConsoleExecResult, error) {
	sessions.cancel()
	<-ctx.Done()
	return connectorapi.ConsoleExecResult{}, ctx.Err()
}

func (sessions *cancelingConsoleSessions) InterruptActive(context.Context, connectorapi.Principal, connectorapi.ConsoleSessionHandle) error {
	sessions.interrupts++
	return nil
}

type runningActionRuntime struct {
	sessions connectorapi.ConsoleSessionRuntime
}

func (runtime runningActionRuntime) ResolveConnectorActionTarget(context.Context, string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	return connectors.TargetView{ID: 1}, connectors.CredentialProfileView{ID: 2, Label: "default"}, nil
}

func (runningActionRuntime) EnsureRuntimeSurface(_ context.Context, input connectorapi.EnsureRuntimeSurfaceInput) (connectorapi.RuntimeSurface, error) {
	return connectorapi.RuntimeSurface{ID: 9, TargetID: input.TargetID, ProfileID: input.ProfileID}, nil
}

func (runningActionRuntime) ListRuntimeSurfacesForProfile(context.Context, int64, int64, string) ([]connectorapi.RuntimeSurface, error) {
	panic("not used")
}

func (runningActionRuntime) TargetProfileByRuntimeID(context.Context, int64) (connectors.TargetView, connectors.CredentialProfileView, connectorapi.RuntimeSurface, error) {
	panic("not used")
}

func (runningActionRuntime) ListCredentialProfiles(context.Context, int64) ([]connectors.CredentialProfileView, error) {
	panic("not used")
}

func (runningActionRuntime) CredentialResources(string) connectorapi.CredentialResourceStore {
	panic("not used")
}

func (runtime runningActionRuntime) ConnectorConsoleSessions() connectorapi.ConsoleSessionRuntime {
	return runtime.sessions
}

func TestFinishRunningActionRequestPropagatesPersistenceFailure(t *testing.T) {
	want := errors.New("database unavailable")
	finisher := &failingActionRequestFinisher{err: want}
	err := finishRunningActionRequest(finisher, nil, 42, connectors.ResultError, nil, "", "failed", connectors.OutputHint{})
	if !errors.Is(err, want) {
		t.Fatalf("finish error = %v, want %v", err, want)
	}
	if !finisher.hadDeadline {
		t.Fatal("finish request did not receive a deadline")
	}
}

func TestFinishRunningDefersCanceledWorkspaceOutcomeToShutdownCoordinator(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	sessions := &cancelingConsoleSessions{cancel: cancel}
	finisher := &failingActionRequestFinisher{}
	err := (Running{}).FinishRunning(
		parent,
		finisher,
		runningActionRuntime{sessions: sessions},
		42,
		connectors.RuntimeActionContext{TargetRef: "ssh:1:2", TargetConnectorKind: "ssh", ActionName: "exec"},
		connectorapi.Principal{},
		connectors.ActionHandles{SessionID: 7, SessionGeneration: 3},
	)
	if err != nil {
		t.Fatalf("FinishRunning() error = %v", err)
	}
	if finisher.finishes != 0 {
		t.Fatalf("shutdown cancellation persisted %d terminal outcomes", finisher.finishes)
	}
	if sessions.interrupts != 0 {
		t.Fatalf("shutdown cancellation interrupted an already-closing session %d times", sessions.interrupts)
	}
}
