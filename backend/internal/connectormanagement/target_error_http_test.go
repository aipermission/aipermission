package connectormanagement

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func TestManagementTargetErrorsRetainConflictPrecedence(t *testing.T) {
	for _, cause := range []error{connectortargets.ErrTargetUpdateConflict, connectortargets.ErrCredentialProfileUpdateConflict, connectortargets.ErrRemoteCleanupPending} {
		t.Run(cause.Error(), func(t *testing.T) {
			response := httptest.NewRecorder()
			err := errors.Join(cause, connectortargets.ErrTargetNotFound)
			writeTargetError(response, err)
			var payload httptransport.ErrorResponse
			if decodeErr := json.Unmarshal(response.Body.Bytes(), &payload); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if response.Code != http.StatusConflict || payload.Error != err.Error() {
				t.Fatalf("conflict presentation = %d %q", response.Code, payload.Error)
			}
		})
	}
	response := httptest.NewRecorder()
	writeTargetError(response, errors.Join(errors.New("private storage detail"), connectortargets.ErrTargetNotFound))
	if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"connector target not found\"}\n" {
		t.Fatalf("read fallback = %d %s", response.Code, response.Body.String())
	}
}
