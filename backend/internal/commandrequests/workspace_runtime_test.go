package commandrequests

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestWorkspaceRuntimeOwnsEncryptedCommandAndHistoryProjection(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	secretVault, err := vault.New("workspace-command-request-secret")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewWorkspaceRuntime(WorkspaceRuntimeDependencies{
		Database: database, Vault: secretVault, WorkspaceID: "workspace-command-request-test",
		Redact: func(_ context.Context, value string) string {
			return strings.ReplaceAll(value, "secret", "[REDACTED]")
		},
		Sessions: &testActiveSessions{}, BackgroundTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := owner.Insert(t.Context(), Insert{
		RuntimeID: runtimeID, Source: SourceManual, Command: "echo secret", Reason: "secret reason", Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := owner.Get(t.Context(), id, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if item.Command != "echo [REDACTED]" || item.Reason != "[REDACTED] reason" {
		t.Fatalf("redacted command request = %#v", item)
	}
	command, err := owner.ExecutionCommand(t.Context(), id)
	if err != nil || command != "echo secret" {
		t.Fatalf("ExecutionCommand() = %q, %v", command, err)
	}
	var display, encrypted string
	if err := database.QueryRowContext(t.Context(), `
		SELECT command, encrypted_command FROM command_requests WHERE id = ?`, id,
	).Scan(&display, &encrypted); err != nil {
		t.Fatal(err)
	}
	if display != "echo [REDACTED]" || encrypted == "" || strings.Contains(encrypted, "echo secret") {
		t.Fatalf("persisted command display=%q encrypted=%q", display, encrypted)
	}
	var historyInput string
	if err := database.QueryRowContext(t.Context(), `
		SELECT input_text FROM history_entries
		WHERE source_ref_type = 'command_request' AND source_ref_id = ?`, id,
	).Scan(&historyInput); err != nil {
		t.Fatal(err)
	}
	if historyInput != "echo [REDACTED]" {
		t.Fatalf("history input = %q", historyInput)
	}
}

func TestWorkspaceRuntimeFailsClosedForIncompleteOwnership(t *testing.T) {
	if _, err := NewWorkspaceRuntime(WorkspaceRuntimeDependencies{}); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("NewWorkspaceRuntime() error = %v", err)
	}
}
