package api

import (
	"bytes"
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

func TestDecodeJSONPreservesExactNumbersInsideDynamicObjects(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"input":{"offset":9223372036854775807}}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	var payload struct {
		Input map[string]any `json:"input"`
	}
	if err := decodeJSON(recorder, request, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Input["offset"] != json.Number("9223372036854775807") {
		t.Fatalf("offset = %#v", payload.Input["offset"])
	}
}
