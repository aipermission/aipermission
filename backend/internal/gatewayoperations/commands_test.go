package gatewayoperations

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type commandSessionsStub struct{}

func (commandSessionsStub) WaitActive(context.Context, executionprincipal.Principal, console.SessionHandle) (console.ExecResult, error) {
	return console.ExecResult{}, nil
}

func (commandSessionsStub) InterruptActive(context.Context, executionprincipal.Principal, console.SessionHandle) error {
	return nil
}

func TestCommandRuntimeFailsClosedUntilInitialized(t *testing.T) {
	component := &CommandComponent{}
	if _, err := component.Runtime("runtime"); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("uninitialized command runtime error = %v", err)
	}
	if err := component.Initialize("runtime", CommandRuntimeDependencies{}); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("invalid command runtime dependencies error = %v", err)
	}
	var unavailable *CommandComponent
	if err := unavailable.Initialize("runtime", CommandRuntimeDependencies{}); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("nil component initialization error = %v", err)
	}
	if err := component.Initialize(" ", CommandRuntimeDependencies{}); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("blank runtime initialization error = %v", err)
	}
}

func TestCommandHTTPHandlersFailClosedWithNilComponent(t *testing.T) {
	var component *CommandComponent
	handlers := component.HTTPHandlers(CommandScopeProviders{})
	if handlers.Bulk != nil || handlers.Requests != nil {
		t.Fatal("nil command component exposed HTTP handlers")
	}
}

func TestCommandHTTPHandlersOwnFailClosedAdapters(t *testing.T) {
	handlers := (&CommandComponent{}).HTTPHandlers(CommandScopeProviders{})
	if handlers.Bulk == nil || handlers.Requests == nil {
		t.Fatal("command component did not expose owned HTTP handlers")
	}
	tests := []struct {
		name string
		run  func(http.ResponseWriter, *http.Request)
	}{
		{name: "bulk", run: handlers.Bulk.Run},
		{name: "request", run: handlers.Requests.Get},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.SetPathValue("id", "1")
			test.run(response, request)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d", response.Code)
			}
		})
	}
}

func TestCommandRuntimeWrapperFailsClosedWithoutOwner(t *testing.T) {
	runtime := &CommandRuntime{}
	if err := runtime.CancelRunning(t.Context(), "closing"); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("CancelRunning() error = %v", err)
	}
	if _, err := runtime.CancelRunningForRuntime(t.Context(), 1, "closing"); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("CancelRunningForRuntime() error = %v", err)
	}
}

func TestCommandRuntimeRoundTripsBoundaryDTOs(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "commands.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secretVault, err := vault.New("test-password")
	if err != nil {
		t.Fatal(err)
	}
	component := &CommandComponent{}
	if err := component.Initialize("runtime-one", CommandRuntimeDependencies{
		Database: database, Vault: secretVault, WorkspaceID: "workspace-one",
		Redact:   func(_ context.Context, value string) string { return value },
		Sessions: commandSessionsStub{}, BackgroundTimeout: time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	runtime, err := component.Runtime("runtime-one")
	if err != nil {
		t.Fatal(err)
	}
	runtimeID := insertCommandBoundaryRuntime(t, database)
	id, err := runtime.Insert(t.Context(), CommandInsert{
		RuntimeID: runtimeID, Source: "mcp", Command: "printf boundary", Reason: "contract", Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := runtime.Get(t.Context(), id, 0, "mcp")
	if err != nil {
		t.Fatal(err)
	}
	if created.Command != "printf boundary" || created.Reason != "contract" || created.Status != "running" {
		t.Fatalf("created command = %#v", created)
	}
	if err := runtime.Finish(t.Context(), CommandCompletion{
		ID: id, Status: "completed", SessionID: 7, Stdout: "boundary", ExitCode: 0,
	}); err != nil {
		t.Fatal(err)
	}
	completed, err := runtime.Get(t.Context(), id, 0, "mcp")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.Stdout != "boundary" || completed.SessionID == nil || *completed.SessionID != 7 {
		t.Fatalf("completed command = %#v", completed)
	}
}

func insertCommandBoundaryRuntime(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	result, err := database.ExecContext(t.Context(), `
		INSERT INTO connector_targets (project_id, connector_kind, name, config_json, status, created_at, updated_at)
		VALUES ((SELECT id FROM projects WHERE slug = 'ungrouped'), 'test', 'worker', '{}', 'active', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = database.ExecContext(t.Context(), `
		INSERT INTO connector_credential_profiles
			(target_id, connector_kind, kind, label, public_json, encrypted_secret_json, status, created_at, updated_at)
		VALUES (?, 'test', 'test', 'default', '{}', '', 'active', datetime('now'), datetime('now'))`, targetID)
	if err != nil {
		t.Fatal(err)
	}
	profileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = database.ExecContext(t.Context(), `
		INSERT INTO connector_runtime_surfaces
			(connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at)
		VALUES ('test', ?, ?, 'live_console', 'console', 'active', datetime('now'), datetime('now'))`, targetID, profileID)
	if err != nil {
		t.Fatal(err)
	}
	runtimeID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return runtimeID
}

func TestCommandBulkHTTPHandlerRejectsIncompleteRuntime(t *testing.T) {
	handlers := (&CommandComponent{}).HTTPHandlers(CommandScopeProviders{
		Bulk: func(http.ResponseWriter) (*CommandBulkHTTPRuntime, bool) {
			return &CommandBulkHTTPRuntime{}, true
		},
	})
	response := httptest.NewRecorder()
	handlers.Bulk.Run(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
}
