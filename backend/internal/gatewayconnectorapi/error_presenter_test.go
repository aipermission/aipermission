package gatewayconnectorapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testErrorPresenter struct {
	handled bool
}

func (presenter testErrorPresenter) PresentConnectorError(_ error) (ErrorPresentation, bool) {
	if !presenter.handled {
		return ErrorPresentation{}, false
	}
	return ErrorPresentation{
		StatusCode: http.StatusConflict,
		Header:     http.Header{"X-Connector": []string{"test"}},
		Payload:    map[string]string{"error": "presented"},
	}, true
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
	if response.Header().Get("X-Connector") != "test" || !strings.Contains(response.Body.String(), `"error":"presented"`) {
		t.Fatalf("handled response = headers=%v body=%q", response.Header(), response.Body.String())
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

func TestWriteErrorPresentationRejectsNonErrorStatuses(t *testing.T) {
	for _, status := range []int{0, http.StatusOK, http.StatusTemporaryRedirect, 600} {
		response := httptest.NewRecorder()
		WriteErrorPresentation(response, ErrorPresentation{StatusCode: status, Payload: map[string]string{"error": "failed"}})
		if response.Code != http.StatusInternalServerError {
			t.Errorf("presented status %d produced %d, want %d", status, response.Code, http.StatusInternalServerError)
		}
	}
}
