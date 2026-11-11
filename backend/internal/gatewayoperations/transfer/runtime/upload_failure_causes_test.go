package transferruntime

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type canceledUploadAdapter struct {
	connectorapi.FileTransferAdapter
	outcomeUnknown bool
}

func (adapter canceledUploadAdapter) UploadFile(ctx context.Context, _ connectorapi.FileTransferGateway, _ connectorapi.TransferRuntime, _ int64, _, _ string, _ bool, _ connectors.TransferOptions) (connectors.TransferResult, error) {
	<-ctx.Done()
	err := ctx.Err()
	if adapter.outcomeUnknown {
		err = connectors.ClassifyOutcomeUnknown("remote_commit", nil, err)
	}
	return connectors.TransferResult{Bytes: 37}, err
}

func TestMCPUploadReturnsAuthorizationCauseWithoutLosingEvidence(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		var checks atomic.Int32
		execution := Execution{
			Adapter: canceledUploadAdapter{outcomeUnknown: unknown},
			AuthorizeUpload: func(ctx context.Context) (context.Context, func(), error) {
				if checks.Add(1) > 1 {
					return nil, nil, context.DeadlineExceeded
				}
				return ctx, func() {}, nil
			},
		}
		item := filetransfer.Record{Source: filetransfer.SourceMCP, Direction: filetransfer.DirectionUpload, TempExpiresAt: time.Now().Add(time.Minute).Format(time.RFC3339Nano)}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		result, err := runBoundUpload(ctx, execution, item, false, connectors.TransferOptions{})
		parentError := ctx.Err()
		cancel()
		if result.Bytes != 37 || parentError != nil {
			t.Fatalf("authorization failure lost evidence or canceled owner: bytes=%d parent=%v err=%v", result.Bytes, parentError, err)
		}
		if unknown {
			if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
				t.Fatalf("authorization failure hid unknown publication: %v", err)
			}
		} else if !errors.Is(err, context.DeadlineExceeded) || connectors.ErrorCode(err) != "upload_authorization_failed" {
			t.Fatalf("authorization cause lost at adapter boundary: %v", err)
		}
	}
}

func TestMCPUploadAuthorizationTimeoutKeepsItsStageInHistory(t *testing.T) {
	runner, runtime, batch, _ := newNativeDownloadBudgetFixture(t, 1)
	id := batch.Items[0].ID
	if _, err := runtime.store.MarkRunning(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	execution := &Execution{Boundary: actionresult.NewCredentialBoundary(nil)}
	if !runner.finishFileTransferError(runtime, id, t.Context(), execution, uploadAuthorizationError(context.DeadlineExceeded)) {
		t.Fatal("authorization timeout was not persisted")
	}
	item, err := runtime.store.Get(t.Context(), id)
	if err != nil || item.Status != filetransfer.StatusFailed || item.FailureKind != filetransfer.FailureKindTimeout {
		t.Fatalf("unexpected terminal outcome: item=%#v err=%v", item, err)
	}
	if !strings.Contains(item.Error, "upload authorization check failed") || item.Error == "file transfer timed out" {
		t.Fatalf("authorization timeout mislabeled as transfer budget: %q", item.Error)
	}
	if item.FailureDetails["dispatch_stage"] != "upload_authorization" || item.FailureDetails["retry_safe"] != false {
		t.Fatalf("authorization failure evidence lost: %#v", item.FailureDetails)
	}
}

func TestMCPUploadOwnerCancelDuringFirstCheckNeverDispatches(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	execution := Execution{
		Adapter: canceledUploadAdapter{},
		AuthorizeUpload: func(ctx context.Context) (context.Context, func(), error) {
			cancel()
			return nil, nil, ctx.Err()
		},
	}
	item := filetransfer.Record{Source: filetransfer.SourceMCP, Direction: filetransfer.DirectionUpload, TempExpiresAt: time.Now().Add(time.Minute).Format(time.RFC3339Nano)}
	result, err := runBoundUpload(ctx, execution, item, false, connectors.TransferOptions{})
	if !errors.Is(err, context.Canceled) || connectors.ErrorCode(err) == "upload_authorization_failed" || result.Bytes != 0 {
		t.Fatalf("owner cancellation became authorization failure or dispatched: result=%#v err=%v", result, err)
	}
}

type directAuthorizationUploadAdapter struct {
	connectorapi.FileTransferAdapter
}

func (directAuthorizationUploadAdapter) UploadFile(ctx context.Context, _ connectorapi.FileTransferGateway, _ connectorapi.TransferRuntime, _ int64, _, _ string, _ bool, options connectors.TransferOptions) (connectors.TransferResult, error) {
	_, release, err := options.AuthorizePublish(ctx)
	if release != nil {
		release()
	}
	return connectors.TransferResult{}, err
}

func TestMCPUploadDirectAuthorizationKeepsFailureStage(t *testing.T) {
	var calls atomic.Int32
	execution := Execution{
		Adapter: directAuthorizationUploadAdapter{},
		AuthorizeUpload: func(ctx context.Context) (context.Context, func(), error) {
			if calls.Add(1) > 1 {
				return nil, nil, context.DeadlineExceeded
			}
			return ctx, func() {}, nil
		},
	}
	item := filetransfer.Record{Source: filetransfer.SourceMCP, Direction: filetransfer.DirectionUpload, TempExpiresAt: time.Now().Add(time.Minute).Format(time.RFC3339Nano)}
	_, err := runBoundUpload(t.Context(), execution, item, false, connectors.TransferOptions{})
	if !errors.Is(err, context.DeadlineExceeded) || connectors.ErrorCode(err) != "upload_authorization_failed" {
		t.Fatalf("direct authorization error lost stage: %v", err)
	}
}
