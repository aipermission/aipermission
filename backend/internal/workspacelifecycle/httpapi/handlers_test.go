package httpapi

import (
	"context"
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

type fakeLifecycle struct {
	unlocked      bool
	status        workspacelifecycle.Status
	renameError   error
	unlockError   error
	passwordError error
	lockError     error
	lockCalls     int
}

func (f *fakeLifecycle) IsUnlocked() bool                           { return f.unlocked }
func (f *fakeLifecycle) Status() (workspacelifecycle.Status, error) { return f.status, nil }
func (f *fakeLifecycle) Setup(context.Context, string, string, string) (workspacelifecycle.Transition, error) {
	return workspacelifecycle.Transition{}, nil
}
func (f *fakeLifecycle) Unlock(context.Context, string, string) (workspacelifecycle.Transition, error) {
	return workspacelifecycle.Transition{}, f.unlockError
}
func (f *fakeLifecycle) Lock(string) (workspacelifecycle.Status, error) {
	f.lockCalls++
	return f.status, f.lockError
}
func (f *fakeLifecycle) WillLockAll(string) bool { return true }
func (f *fakeLifecycle) Rename(context.Context, string, string) (workspacelifecycle.Transition, error) {
	return workspacelifecycle.Transition{}, f.renameError
}
func (f *fakeLifecycle) DeleteCurrent(context.Context, string, string) (workspacelifecycle.Transition, error) {
	return workspacelifecycle.Transition{}, nil
}
func (f *fakeLifecycle) DeleteLocked(string, string) (workspacelifecycle.Transition, error) {
	return workspacelifecycle.Transition{}, nil
}
func (f *fakeLifecycle) Switch(context.Context, string, string) (workspacelifecycle.Transition, error) {
	return workspacelifecycle.Transition{}, nil
}
func (f *fakeLifecycle) ChangePassword(context.Context, string, string) error { return f.passwordError }

type fakeAttempt struct{ successes, failures int }

func (a *fakeAttempt) Success() { a.successes++ }
func (a *fakeAttempt) Failure() { a.failures++ }

type detachedRuntimeError struct{ databaseID string }

func (e detachedRuntimeError) Error() string                       { return "runtime reactivation failed" }
func (e detachedRuntimeError) SessionInvalidationDatabase() string { return e.databaseID }

func TestChangePasswordDetachedRuntimeInvalidatesWorkspaceSessionsAndExpiresCookies(t *testing.T) {
	attempt := &fakeAttempt{}
	invalidated := ""
	expired := false
	handlers := New(Dependencies{
		Lifecycle: &fakeLifecycle{passwordError: detachedRuntimeError{databaseID: "workspace-a"}},
		BeginAttempt: func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool) {
			return attempt, true
		},
		InvalidateSessions: func(databaseID string) { invalidated = databaseID },
		ExpireSession:      func(http.ResponseWriter) { expired = true },
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/settings/database-password", strings.NewReader(
		`{"current_password":"CurrentPassword123","new_password":"ReplacementPassword456","confirm_password":"ReplacementPassword456"}`,
	))
	request.Header.Set("Content-Type", "application/json")

	handlers.ChangePassword(response, request)

	if response.Code != http.StatusInternalServerError || invalidated != "workspace-a" || !expired {
		t.Fatalf("code=%d invalidated=%q expired=%t", response.Code, invalidated, expired)
	}
}

func TestStatusRedactsFilesystemPathsAndReportsSessionRequirement(t *testing.T) {
	lifecycle := &fakeLifecycle{unlocked: true, status: workspacelifecycle.Status{
		State: "unlocked", Identity: workspacelifecycle.Identity{ID: "private", Path: "/secret/private.db"},
		DatabaseName: "Private", Databases: []databasecatalog.DatabaseInfo{{ID: "private", Path: "/secret/private.db"}},
	}}
	handlers := New(Dependencies{Lifecycle: lifecycle, HasSession: func(*http.Request) bool { return false }})
	response := httptest.NewRecorder()
	handlers.Status(response, httptest.NewRequest(http.MethodGet, "/api/unlock/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"session_required"`) {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "/secret/") || strings.Contains(response.Body.String(), "data_path") {
		t.Fatalf("status leaked local path: %s", response.Body.String())
	}
}

func TestInvalidLockScopeHasNoLifecycleSideEffects(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	maintenanceClosed := false
	handlers := New(Dependencies{
		Lifecycle:        lifecycle,
		CloseMaintenance: func(string) { maintenanceClosed = true },
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/lock", strings.NewReader(`{"scope":"invalid"}`))
	request.Header.Set("Content-Type", "application/json")
	handlers.Lock(response, request)
	if response.Code != http.StatusBadRequest || maintenanceClosed || lifecycle.lockCalls != 0 {
		t.Fatalf("code=%d maintenance_closed=%t lock_calls=%d", response.Code, maintenanceClosed, lifecycle.lockCalls)
	}
}

func TestLockClearsSessionWhenRuntimeDetachedDespiteCleanupError(t *testing.T) {
	cleared := false
	handlers := New(Dependencies{
		Lifecycle: &fakeLifecycle{
			status:    workspacelifecycle.Status{State: "locked"},
			lockError: errors.New("cleanup failed"),
		},
		ClearSessions: func(http.ResponseWriter) { cleared = true },
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/lock", strings.NewReader(`{"scope":"all"}`))
	request.Header.Set("Content-Type", "application/json")
	handlers.Lock(response, request)
	if response.Code != http.StatusInternalServerError || !cleared {
		t.Fatalf("code=%d cleared=%t", response.Code, cleared)
	}
}

func TestLockCurrentInvalidatesOnlyTheDetachedWorkspaceSessions(t *testing.T) {
	invalidated := ""
	handlers := New(Dependencies{
		Lifecycle: &fakeLifecycle{status: workspacelifecycle.Status{
			State: "unlocked", Identity: workspacelifecycle.Identity{ID: "workspace-a"},
		}},
		InvalidateSessions: func(databaseID string) { invalidated = databaseID },
		IssueSession:       func(http.ResponseWriter) error { return nil },
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/lock", strings.NewReader(`{"scope":"current"}`))
	request.Header.Set("Content-Type", "application/json")
	handlers.Lock(response, request)
	if response.Code != http.StatusOK || invalidated != "workspace-a" {
		t.Fatalf("code=%d invalidated=%q", response.Code, invalidated)
	}
}

func TestUnlockErrorsAndAttemptsAreClassifiedAtTheWorkspaceBoundary(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantStatus   int
		wantBody     string
		wantFailures int
		wantSuccess  int
	}{
		{
			name: "database already owned", err: databaseownership.ErrDatabaseInUse,
			wantStatus: http.StatusConflict, wantBody: "database is in use by another AIPermission process",
		},
		{
			name: "authentication", err: fmt.Errorf("%w: encrypted database validation failed", workspacelifecycle.ErrAuthentication),
			wantStatus: http.StatusUnauthorized, wantBody: "invalid unlock password or database", wantFailures: 1,
		},
		{
			name: "initialization", err: fmt.Errorf("%w: migrate encrypted records: invalid envelope", workspacelifecycle.ErrInitialization),
			wantStatus: http.StatusConflict, wantBody: "database initialization failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempt := &fakeAttempt{}
			handlers := New(Dependencies{
				Lifecycle: &fakeLifecycle{unlockError: test.err},
				BeginAttempt: func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool) {
					return attempt, true
				},
			})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/unlock", strings.NewReader(`{"password":"LongPassword123"}`))
			request.Header.Set("Content-Type", "application/json")
			handlers.Unlock(response, request)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantBody) {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
			if attempt.failures != test.wantFailures || attempt.successes != test.wantSuccess {
				t.Fatalf("attempt successes=%d failures=%d", attempt.successes, attempt.failures)
			}
		})
	}
}

func TestRenameFailureClearsSessionOnlyWhenRecoveryFailed(t *testing.T) {
	lifecycle := &fakeLifecycle{unlocked: false, renameError: errors.New("move failed")}
	attempt := &fakeAttempt{}
	cleared := false
	handlers := New(Dependencies{
		Lifecycle:     lifecycle,
		BeginAttempt:  func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool) { return attempt, true },
		ClearSessions: func(http.ResponseWriter) { cleared = true },
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/databases/rename", strings.NewReader(`{"database_name":"Renamed","current_password":"Password123456"}`))
	request.Header.Set("Content-Type", "application/json")
	handlers.Rename(response, request)
	if response.Code != http.StatusInternalServerError || !cleared {
		t.Fatalf("code=%d cleared=%t", response.Code, cleared)
	}
}
