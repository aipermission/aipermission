package gatewaytransfer

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
)

func TestUploadBatchRejectsLossyRelativePathJSONBeforeCreation(t *testing.T) {
	for _, raw := range []string{`["invoice\ud800.txt"]`, `["invoice\udc00.txt"]`, `["invoice` + string([]byte{0xff}) + `.txt"]`} {
		t.Run(raw, func(t *testing.T) {
			fixture := newTransferTestFixture(t)
			fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) {
				return fixture.runtime, true
			}
			request := newMultipartTransferRequest(t, fixture.runtimeID, "", "/uploads", map[string]string{"relative_paths": raw})
			response := httptest.NewRecorder()
			fixture.handlers.StartUploadBatch(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("lossy path accepted: status=%d body=%s", response.Code, response.Body.String())
			}
			_, total, err := fixture.store.ListBatches(t.Context(), filetransfer.BatchListFilter{})
			if err != nil || total != 0 {
				t.Fatalf("upload was persisted before rejection: count=%d err=%v", total, err)
			}
		})
	}
}

func TestUploadBatchPreservesValidUnicodeRelativePaths(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{`["invoice\ufffd.txt"]`, "/uploads/invoice\ufffd.txt"},
		{`["folder/invoice\ud83d\ude00.txt"]`, "/uploads/folder/invoice\U0001f600.txt"},
	} {
		t.Run(test.raw, func(t *testing.T) {
			fixture := newTransferTestFixture(t)
			fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) {
				return fixture.runtime, true
			}
			request := newMultipartTransferRequest(t, fixture.runtimeID, "", "/uploads", map[string]string{"relative_paths": test.raw})
			response := httptest.NewRecorder()
			fixture.handlers.StartUploadBatch(response, request)
			if response.Code != http.StatusAccepted {
				t.Fatalf("valid path rejected: status=%d body=%s", response.Code, response.Body.String())
			}
			if !fixture.jobs.Wait(t.Context()) {
				t.Fatal("fixture jobs did not drain")
			}
			batches, total, err := fixture.store.ListBatches(t.Context(), filetransfer.BatchListFilter{})
			if err != nil || total != 1 || len(batches) != 1 || len(batches[0].Items) != 1 || batches[0].Items[0].RemotePath != test.want {
				t.Fatalf("path identity changed: batches=%#v total=%d err=%v", batches, total, err)
			}
		})
	}
}
