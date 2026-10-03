package backup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderRestoreDoesNotAdmitWriterBeforeSelection(t *testing.T) {
	reads, mutations := 0, 0
	component := New(Dependencies{
		Lifecycle: lifecycleStub{
			acquire:         func(context.Context) (func(), error) { reads++; return func() {}, nil },
			acquireMutation: func(context.Context) (func(), error) { mutations++; return func() {}, nil },
		},
		AcquireOperation:    (&OperationLimiter{}).Acquire,
		AuthorizeImport:     func(http.ResponseWriter, *http.Request) (func() bool, bool) { return func() bool { return true }, true },
		AuthorizeOperation:  func(http.ResponseWriter, *http.Request) bool { return true },
		ActiveRuntime:       func(http.ResponseWriter) (Runtime, bool) { return Runtime{}, true },
		CurrentDatabaseName: func() string { return "Test" },
	})
	request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/records/2/restore", strings.NewReader(`{"database_name":"Copy","database_password":"FixturePassword123"}`))
	request.SetPathValue("id", "1")
	request.SetPathValue("record_id", "2")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	component.HTTPHandlers().RestoreProvider(response, request)
	if mutations != 0 || reads != 1 {
		t.Fatalf("restore selected under writer: reads=%d mutations=%d status=%d", reads, mutations, response.Code)
	}
}

func TestRestoreDestinationValidationPrecedesAdmission(t *testing.T) {
	component := New(Dependencies{AcquireOperation: func(context.Context) (func(), error) {
		t.Fatal("invalid restore acquired an operation slot")
		return nil, nil
	}})
	for _, handler := range []http.HandlerFunc{component.HTTPHandlers().RestoreRemote, component.HTTPHandlers().RestoreProvider} {
		for _, body := range []string{`{"database_name":"Copy"}`, `{"database_password":"FixturePassword123"}`} {
			request := httptest.NewRequest(http.MethodPost, "/restore", strings.NewReader(body))
			request.SetPathValue("id", "1")
			request.SetPathValue("record_id", "2")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid destination response=%d %s", response.Code, response.Body.String())
			}
		}
	}
}
