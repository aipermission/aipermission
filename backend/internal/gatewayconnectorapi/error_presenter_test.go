package gatewayconnectorapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testErrorPresenter struct {
	handled bool
}

func (presenter testErrorPresenter) WriteConnectorError(w http.ResponseWriter, _ error) bool {
	if !presenter.handled {
		return false
	}
	w.WriteHeader(http.StatusConflict)
	return true
}

func (testErrorPresenter) ConnectorErrorMessage(prefix string, _ error) string {
	return prefix + ": presented"
}

func TestErrorPresenterHelpers(t *testing.T) {
	response := httptest.NewRecorder()
	if WritePresentedError(response, struct{}{}, errors.New("failed")) {
		t.Fatal("adapter without presenter handled the response")
	}
	if WritePresentedError(response, testErrorPresenter{}, errors.New("failed")) {
		t.Fatal("declining presenter handled the response")
	}
	if !WritePresentedError(response, testErrorPresenter{handled: true}, errors.New("failed")) || response.Code != http.StatusConflict {
		t.Fatalf("handled response status = %d", response.Code)
	}
	if got := PresentedErrorMessage(testErrorPresenter{}, "operation failed", errors.New("raw")); got != "operation failed: presented" {
		t.Fatalf("presented message = %q", got)
	}
	if got := PresentedErrorMessage(struct{}{}, "operation failed", errors.New(" raw ")); got != "operation failed: raw" {
		t.Fatalf("fallback message = %q", got)
	}
	if got := PresentedErrorMessage(struct{}{}, "operation failed", nil); got != "operation failed" {
		t.Fatalf("nil error message = %q", got)
	}
}
