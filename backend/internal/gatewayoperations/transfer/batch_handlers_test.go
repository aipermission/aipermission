package gatewaytransfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
)

func TestApproveBatchTerminalizesAcceptedItemsWhenExecutionResolutionFails(t *testing.T) {
	fixture := newTransferTestFixture(t)
	fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) {
		return fixture.runtime, true
	}
	batch, err := fixture.store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: fixture.runtimeID,
		Direction: filetransfer.DirectionDownload,
		Source:    filetransfer.SourceMCP,
		Status:    filetransfer.StatusPendingApproval,
		Items: []filetransfer.CreateRequest{
			{RemotePath: "/report.log", FileName: "report.log"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.resolver.resolve = func(context.Context, int64) (transferapp.ConnectorPorts, error) {
		return transferapp.ConnectorPorts{}, errors.New("connector unavailable")
	}
	body, err := json.Marshal(approveFileTransferBatchRequest{ItemIDs: []int64{batch.Items[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/batches/"+strconv.FormatInt(batch.ID, 10)+"/approve", bytes.NewReader(body))
	request.SetPathValue("id", strconv.FormatInt(batch.ID, 10))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fixture.handlers.ApproveFileTransferBatch(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	stored, err := fixture.store.GetBatch(t.Context(), batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != filetransfer.StatusFailed || stored.FailureKind != filetransfer.FailureKindInterrupted {
		t.Fatalf("batch status = %q, failure kind = %q", stored.Status, stored.FailureKind)
	}
}
