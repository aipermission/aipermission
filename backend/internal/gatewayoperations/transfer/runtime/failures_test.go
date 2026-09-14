package transferruntime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

func TestClassifyFileTransferInterruption(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want fileTransferInterruption
	}{
		{name: "deadline context", ctx: deadlineCtx, err: context.Canceled, want: fileTransferTimedOut},
		{name: "deadline error", ctx: context.Background(), err: context.DeadlineExceeded, want: fileTransferTimedOut},
		{name: "user cancellation", ctx: canceledCtx, err: context.Canceled, want: fileTransferCanceledByUser},
		{name: "connector cancellation without local cancel", ctx: context.Background(), err: context.Canceled, want: fileTransferNotInterrupted},
		{name: "ordinary failure", ctx: context.Background(), err: errors.New("network failed"), want: fileTransferNotInterrupted},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := classifyFileTransferInterruption(testCase.ctx, testCase.err); got != testCase.want {
				t.Fatalf("classification=%d want=%d", got, testCase.want)
			}
		})
	}
}

func TestFileTransferFailureMessageKeepsTimeoutExplicit(t *testing.T) {
	if got := fileTransferFailureMessage(errFileTransferTimedOut); got != "file transfer timed out" {
		t.Fatalf("timeout message=%q", got)
	}
}

func TestOutcomeUnknownConnectorFailurePersistsOnTransferRecord(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	targets := connectortargets.NewStore(database)
	target, err := targets.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "fixture", Name: "fixture", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targets.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "fixture", Label: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := targets.EnsureRuntimeSurface(t.Context(), connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: target.ConnectorKind, TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: connectortargets.RuntimeCapabilityFileTransfer, Label: "fixture transfer",
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs := &transferjobs.Registry{}
	finalization := transferjobs.NewFinalizationLifetime()
	t.Cleanup(func() {
		jobs.Close()
		finalization.Stop()
	})
	runtime, err := NewRuntime(RuntimeDependencies{
		StorageID: "workspace",
		Database:  database, Jobs: jobs, Finalization: finalization,
		Observe:        func(context.Context, string, *int64, int64, string, any) {},
		ConnectorPorts: func(context.Context, int64) (ConnectorPorts, error) { return ConnectorPorts{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := runtime.store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: surface.ID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		LocalPath: "artifact.zip", RemotePath: "/tmp/artifact.zip", FileName: "artifact.zip",
	})
	if err != nil {
		t.Fatal(err)
	}
	connectorErr := connectors.ClassifyOutcomeUnknown("remote_commit", map[string]any{
		"escaped": strings.Repeat("\"\\\n", 18*1024),
	}, errors.New("rename acknowledgement lost"))
	execution := &Execution{Boundary: actionresult.NewCredentialBoundary(nil)}
	if ok := (Runner{}).finishFileTransferError(runtime, record.ID, context.Background(), execution, connectorErr); !ok {
		t.Fatal("outcome-unknown transfer was not terminalized")
	}
	stored, err := runtime.store.Get(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != filetransfer.StatusFailed || stored.FailureKind != filetransfer.FailureKindOutcomeUnknown {
		t.Fatalf("stored transfer = %#v", stored)
	}
}

func TestSafeTransferFailureDetailsBoundsEncodedJSON(t *testing.T) {
	execution := &Execution{Boundary: actionresult.NewCredentialBoundary(nil)}
	err := connectors.ClassifyOutcomeUnknown("dispatch", map[string]any{
		"escaped": strings.Repeat("\"\\\n", 18*1024),
		"hint":    "inspect destination",
	}, errors.New("lost reply"))

	details := safeTransferFailureDetails(execution, err)
	encoded, marshalErr := json.Marshal(details)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if len(encoded) > filetransfer.MaxFailureDetailsJSONBytes {
		t.Fatalf("encoded failure details bytes=%d, max=%d", len(encoded), filetransfer.MaxFailureDetailsJSONBytes)
	}
	if details["retry_safe"] != false || details["dispatch_stage"] != "dispatch" {
		t.Fatalf("bounded details lost outcome evidence: %#v", details)
	}
}

func TestSafeTransferFailureDetailsPreservesUTF8Boundaries(t *testing.T) {
	execution := &Execution{Boundary: actionresult.NewCredentialBoundary(nil)}
	err := connectors.ClassifyOutcomeUnknown("dispatch", map[string]any{
		"detail": strings.Repeat("ş", 20*1024),
	}, errors.New("lost reply"))

	encoded, marshalErr := json.Marshal(safeTransferFailureDetails(execution, err))
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if len(encoded) > filetransfer.MaxFailureDetailsJSONBytes || !json.Valid(encoded) {
		t.Fatalf("invalid bounded UTF-8 details: bytes=%d valid=%v", len(encoded), json.Valid(encoded))
	}
}
