package connectormanagement

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProfileTestingHandlerRejectsIncompleteScope(t *testing.T) {
	handler := NewProfileTestingHTTPHandler(func(http.ResponseWriter) (ProfileTestingScope, bool) {
		return ProfileTestingScope{}, true
	})
	response := httptest.NewRecorder()
	handler.Test(response, httptest.NewRequest(http.MethodPost, "/api/connector-targets/1/profiles/2/test", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
