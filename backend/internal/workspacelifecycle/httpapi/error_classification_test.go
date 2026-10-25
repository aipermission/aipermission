package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/databaseownership"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

func TestSetupAndUnlockClassifyErrorsWithoutDisclosingUnknownInternals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"unexpected IO", errors.New("open /private/operator/database.db: permission denied"), 500},
		{"owned", fmt.Errorf("%w: /private/operator/database.db", databaseownership.ErrDatabaseInUse), 409},
		{"existing", databasecatalog.ErrDatabaseExists, 409},
		{"invalid selection", databasecatalog.ErrInvalidSelection, 400},
		{"invalid request", workspacelifecycle.ErrInvalidRequest, 400},
		{"credential", workspacelifecycle.ErrCredential, 401},
	} {
		for _, setup := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/setup=%t", tc.name, setup), func(t *testing.T) {
				h := New(Dependencies{Lifecycle: &fakeLifecycle{setupError: tc.err, unlockError: tc.err},
					BeginAttempt: func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool) { return &fakeAttempt{}, true },
				})
				w := httptest.NewRecorder()
				body := `{"password":"PasswordFixture123"}`
				if setup {
					body = `{"password":"PasswordFixture123","confirm_password":"PasswordFixture123"}`
				}
				r := httptest.NewRequest("POST", "/api/unlock", strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				if setup {
					h.Setup(w, r)
				} else {
					h.Unlock(w, r)
				}
				if w.Code != tc.status || strings.Contains(w.Body.String(), "/private/") {
					t.Fatalf("response=%d %s; want=%d without private path", w.Code, w.Body.String(), tc.status)
				}
			})
		}
	}
}

func TestInitializationRecoveryGuidanceRemainsActionable(t *testing.T) {
	w := httptest.NewRecorder()
	writeUnlockError(w, fmt.Errorf("%w: encrypted pre-migration snapshot retained at /recovery/snapshot.aipdb", workspacelifecycle.ErrInitialization))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "/recovery/snapshot.aipdb") {
		t.Fatalf("lost deliberate migration recovery guidance: %s", w.Body.String())
	}
}
