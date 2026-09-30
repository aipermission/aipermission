package commandrequests

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestCommandCompletionPersistsMaskedJSONText(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	secretVault, err := vault.New("json-command-test-key")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewWorkspaceRuntime(WorkspaceRuntimeDependencies{
		Database: database, Vault: secretVault, WorkspaceID: "json-command-test",
		Redact:   securitypolicy.NewService(database).Redact,
		Sessions: &testActiveSessions{}, BackgroundTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Source: SourceManual, Command: "inspect", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	input := `{"password":"unregistered-json-secret","keep":"visible"}`
	if err := owner.Finish(t.Context(), Completion{ID: id, Status: "error", Stdout: input, Stderr: input, Error: input}); err != nil {
		t.Fatal(err)
	}
	stored, err := NewStore(database).Get(t.Context(), id, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{stored.Stdout, stored.Stderr, stored.Error} {
		if strings.Contains(value, "unregistered-json-secret") || !json.Valid([]byte(value)) || !strings.Contains(value, "visible") {
			t.Fatalf("stored command output not safely redacted: %s", value)
		}
	}
	var historyOutput string
	if err := database.QueryRowContext(t.Context(), `SELECT output_text FROM history_entries WHERE source_ref_type = 'command_request' AND source_ref_id = ?`, id).Scan(&historyOutput); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(historyOutput, "unregistered-json-secret") || !strings.Contains(historyOutput, "visible") {
		t.Fatalf("history bypassed command redaction: %s", historyOutput)
	}
}
