package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type fakeLifecycle struct {
	unlocked    bool
	status      workspacelifecycle.Status
	renameError error
	lockCalls   int
}

func (f *fakeLifecycle) IsUnlocked() bool                           { return f.unlocked }
func (f *fakeLifecycle) Status() (workspacelifecycle.Status, error) { return f.status, nil }
func (f *fakeLifecycle) Setup(string, string, string) (workspacelifecycle.Transition, error) {
	return workspacelifecycle.Transition{}, nil
}
func (f *fakeLifecycle) Unlock(string, string) (workspacelifecycle.Transition, error) {
	return workspacelifecycle.Transition{}, nil
}
func (f *fakeLifecycle) Lock(string) (workspacelifecycle.Status, error) {
	f.lockCalls++
	return f.status, nil
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
func (f *fakeLifecycle) Switch(string, string) (workspacelifecycle.Transition, error) {
	return workspacelifecycle.Transition{}, nil
}
func (f *fakeLifecycle) ChangePassword(context.Context, string, string) error { return nil }

type fakeAttempt struct{ successes, failures int }

func (a *fakeAttempt) Success() { a.successes++ }
func (a *fakeAttempt) Failure() { a.failures++ }

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
