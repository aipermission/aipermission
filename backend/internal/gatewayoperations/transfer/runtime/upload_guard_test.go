package transferruntime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestMCPUploadAuthorizationStopsActiveStream(t *testing.T) {
	var revoked atomic.Bool
	var admissions atomic.Int32
	authorize := func(ctx context.Context) (context.Context, func(), error) {
		if revoked.Load() {
			return nil, nil, errors.New("token revoked")
		}
		admissions.Add(1)
		return ctx, func() { admissions.Add(-1) }, nil
	}
	ctx, stop, err := monitorUploadAuthorization(t.Context(), authorize)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if admissions.Load() != 0 {
		t.Fatal("long-running stream holds delivery admission")
	}
	revoked.Store(true)
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("revocation did not cancel the active stream")
	}
	if admissions.Load() != 0 {
		t.Fatal("authorization monitor leaked admission")
	}
}

func TestOnlyMCPUploadsReceiveAnApprovedContentGuard(t *testing.T) {
	for _, source := range []string{filetransfer.SourceMCP, filetransfer.SourceUI} {
		for _, direction := range []string{filetransfer.DirectionUpload, filetransfer.DirectionDownload} {
			options := connectors.TransferOptions{}
			item := filetransfer.Record{Source: source, Direction: direction, SizeBytes: 0, ChecksumSHA256: "empty-checksum"}
			bindUploadContent(&options, item)
			want := source == filetransfer.SourceMCP && direction == filetransfer.DirectionUpload
			if (options.ExpectedContent != nil) != want {
				t.Fatalf("wrong content guard: source=%s direction=%s", source, direction)
			}
			if want && (options.ExpectedContent.SizeBytes != 0 || options.ExpectedContent.ChecksumSHA256 != item.ChecksumSHA256) {
				t.Fatal("empty upload guard lost immutable metadata")
			}
		}
	}
}

func TestMCPUploadAuthorizationMonitorPreservesCancellationCause(t *testing.T) {
	want := errors.New("fixture authorization lookup failed")
	var checks atomic.Int32
	authorize := func(ctx context.Context) (context.Context, func(), error) {
		if checks.Add(1) > 1 {
			return nil, nil, want
		}
		return ctx, func() {}, nil
	}
	ctx, stop, err := monitorUploadAuthorization(t.Context(), authorize)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), want) {
			t.Fatalf("authorization cause lost: %v", context.Cause(ctx))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("authorization failure did not stop the stream")
	}
}
