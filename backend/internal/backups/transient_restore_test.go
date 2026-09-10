package backups

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransientListRejectsInvalidRequestWithoutReflectingToken(t *testing.T) {
	const token = "transient-secret-token-with-thirty-two-characters"
	request := httptest.NewRequest(http.MethodPost, "/api/backup/remote/list", strings.NewReader(`{
		"base_url":"https://backup.example.com","token":"`+token+`","unknown":true
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	NewTransientHTTPHandlers().List(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if strings.Contains(response.Body.String(), token) {
		t.Fatal("transient credential was reflected in validation response")
	}
}

func TestTransientErrorsPreserveRestoreSemantics(t *testing.T) {
	tests := []struct {
		err    error
		status int
		text   string
	}{
		{ErrTransientBackupTooLarge, http.StatusRequestEntityTooLarge, ErrTransientBackupTooLarge.Error()},
		{ErrTransientBackupChanged, http.StatusBadGateway, ErrTransientBackupChanged.Error()},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		WriteServiceHTTPError(response, test.err)
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.text) {
			t.Fatalf("response = %d %s, want %d containing %q", response.Code, response.Body.String(), test.status, test.text)
		}
	}
}

func TestPrepareTransientRestoreRejectsIncompleteCompositionScope(t *testing.T) {
	_, err := PrepareTransientRestore(t.Context(), "", TransientRestoreSelection{
		BaseURL: "https://backup.example.com", Token: "transient-secret-token-with-thirty-two-characters",
		StreamID: "stream", BackupID: "backup",
	})
	if !errors.Is(err, ErrIncompleteScope) {
		t.Fatalf("error = %v, want ErrIncompleteScope", err)
	}
}
