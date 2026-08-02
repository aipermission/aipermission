package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorWithCodePreservesStableMachineContract(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeErrorWithCode(recorder, http.StatusBadRequest, "operation refused", "operation_unsupported")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != "operation refused" || response.Code != "operation_unsupported" {
		t.Fatalf("response = %#v", response)
	}
}
