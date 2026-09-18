package gatewaytransfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
)

type successfulBatchUploadAdapter struct {
	rejectingTransferAdapter
	calls atomic.Int32
}

func (adapter *successfulBatchUploadAdapter) UploadFile(_ context.Context, _ connectorapi.FileTransferGateway, _ connectorapi.TransferRuntime, _ int64, localPath string, _ string, _ bool, _ connectors.TransferOptions) (connectors.TransferResult, error) {
	adapter.calls.Add(1)
	contents, err := os.ReadFile(localPath)
	if err != nil {
		return connectors.TransferResult{}, err
	}
	checksum := sha256.Sum256(contents)
	return connectors.TransferResult{Bytes: int64(len(contents)), ChecksumSHA256: hex.EncodeToString(checksum[:])}, nil
}

func TestCompletedUploadBatchReplayReturnsOriginalWithoutLaunching(t *testing.T) {
	adapter := &successfulBatchUploadAdapter{}
	fixture := newTransferTestFixtureWithAdapter(t, adapter)
	fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) {
		return fixture.runtime, true
	}

	first := invokeTestUploadBatch(t, fixture)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first start status = %d, body = %s", first.Code, first.Body.String())
	}
	var original filetransfer.BatchRecord
	if err := json.Unmarshal(first.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	if !fixture.jobs.Wait(t.Context()) {
		t.Fatal("first batch runner did not drain")
	}
	completed, err := fixture.store.GetBatch(t.Context(), original.ID)
	if err != nil || completed.Status != filetransfer.StatusCompleted {
		t.Fatalf("first batch was not completed: %#v, %v", completed, err)
	}
	fixture.jobs.Close()

	replay := invokeTestUploadBatch(t, fixture)
	if replay.Code != http.StatusAccepted {
		t.Fatalf("replay status = %d, body = %s", replay.Code, replay.Body.String())
	}
	var returned filetransfer.BatchRecord
	if err := json.Unmarshal(replay.Body.Bytes(), &returned); err != nil {
		t.Fatal(err)
	}
	if returned.ID != original.ID || returned.Status != filetransfer.StatusCompleted {
		t.Fatalf("replay returned %#v instead of completed batch %d", returned, original.ID)
	}
	if calls := adapter.calls.Load(); calls != 1 {
		t.Fatalf("upload calls = %d, want 1", calls)
	}
}
