package api

import (
	"bytes"
	"encoding/json"
	"errors"
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
	err := decodeJSONBody(recorder, request, &payload)
	var maxBytesErr *http.MaxBytesError
	if !errors.As(err, &maxBytesErr) {
		t.Fatalf("decode error = %v, want MaxBytesError", err)
	}
	if maxBytesErr.Limit != defaultJSONBodyBytes {
		t.Fatalf("limit = %d, want %d", maxBytesErr.Limit, defaultJSONBodyBytes)
	}
}

func TestDecodeJSONAllowsExplicitConnectorActionLimit(t *testing.T) {
	for _, path := range []string{"/api/connector-actions/local-run", "/api/mcp/connector-actions/call"} {
		t.Run(path, func(t *testing.T) {
			if got := jsonBodyLimitForPath(path); got != connectorActionJSONBodyBytes {
				t.Fatalf("limit = %d, want %d", got, connectorActionJSONBodyBytes)
			}
			request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(jsonBodyLargerThan(defaultJSONBodyBytes)))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			var payload map[string]any
			if err := decodeJSONBody(recorder, request, &payload); err != nil {
				t.Fatalf("decode connector action body: %v", err)
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
	var response errorResponse
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
