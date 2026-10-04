package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const defaultJSONBodyBytes = 1 << 20

func TestWriteErrorWithCodePreservesStableMachineContract(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeErrorWithCode(recorder, http.StatusBadRequest, "operation refused", "operation_unsupported")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response httptransport.ErrorResponse
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
	if !decodeJSON(recorder, request, &payload) {
		t.Fatalf("decode response = %s", recorder.Body.String())
	}
	if payload.Input["offset"] != json.Number("9223372036854775807") {
		t.Fatalf("offset = %#v", payload.Input["offset"])
	}
}

func TestDecodeJSONUsesConservativeDefaultLimit(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(jsonBodyLargerThan(defaultJSONBodyBytes)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	var payload map[string]any
	if decodeJSON(recorder, request, &payload) || recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDecodeJSONAllowsExplicitConnectorActionLimit(t *testing.T) {
	for _, path := range []string{"/api/connector-actions/local-run", "/api/mcp/connector-actions/call"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(jsonBodyLargerThan(defaultJSONBodyBytes)))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			var payload map[string]any
			if !decodeJSON(recorder, request, &payload) {
				t.Fatalf("decode connector action body: %s", recorder.Body.String())
			}
			request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(jsonBodyLargerThan(32<<20)))
			request.Header.Set("Content-Type", "application/json")
			recorder = httptest.NewRecorder()
			if decodeJSON(recorder, request, &payload) || recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("action route exceeded its 32 MiB limit: status=%d", recorder.Code)
			}
		})
	}
}

func TestDecodeJSONReturnsStablePayloadTooLargeResponse(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(jsonBodyLargerThan(defaultJSONBodyBytes)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	var payload map[string]any
	if decodeJSON(recorder, request, &payload) {
		t.Fatal("oversized body decoded successfully")
	}
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
	var response httptransport.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "request_body_too_large" {
		t.Fatalf("code = %q, want request_body_too_large", response.Code)
	}
}

func jsonBodyLargerThan(limit int64) []byte {
	return []byte(`{"value":"` + string(bytes.Repeat([]byte("a"), int(limit))) + `"}`)
}
