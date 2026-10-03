package commandrequests

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestShutdownCannotAssertUndispatchedWithoutEvidence(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	owner := newTestRuntime(t, database, &testActiveSessions{}, &testCommandProjection{})
	id, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Command: "remote effect", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.CancelRunning(t.Context(), "workspace closed"); err != nil {
		t.Fatal(err)
	}
	var status, message string
	var exitCode *int
	if err := database.QueryRowContext(t.Context(), `SELECT status, error, exit_code FROM command_requests WHERE id = ?`, id).Scan(&status, &message, &exitCode); err != nil {
		t.Fatal(err)
	}
	if status != "outcome_unknown" || exitCode != nil || !strings.Contains(message, "before retrying") {
		t.Fatalf("shutdown asserted a terminal outcome without evidence: status=%q exit=%v message=%q", status, exitCode, message)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/console/command-requests/"+strconv.FormatInt(id, 10), nil)
	request.SetPathValue("id", strconv.FormatInt(id, 10))
	reply := httptest.NewRecorder()
	NewHTTPHandlers(func(http.ResponseWriter) (HTTPReader, bool) { return owner, true }).Get(reply, request)
	var result map[string]any
	if reply.Code != http.StatusOK || json.Unmarshal(reply.Body.Bytes(), &result) != nil || result["status"] != "outcome_unknown" || result["exit_code"] != nil {
		t.Fatalf("HTTP projection misrepresented unknown outcome: %d %s", reply.Code, reply.Body.String())
	}
}

func TestDispatchRecoveryMatchesHistoryAndAudit(t *testing.T) {
	for _, scope := range []string{"lock", "runtime", "restart"} {
		t.Run(scope, func(t *testing.T) {
			database, runtimeID := commandRequestFixture(t)
			store := NewStore(database)
			projection := commandHistoryProjection{}
			cases := []struct {
				name, want                          string
				queued, admitted, session, finished bool
			}{
				{name: "queued", queued: true, want: "canceled"},
				{name: "admitted", queued: true, admitted: true, want: "outcome_unknown"},
				{name: "older", want: "outcome_unknown"},
				{name: "bound", queued: true, session: true, want: "outcome_unknown"},
				{name: "terminal", queued: true, admitted: true, finished: true, want: "completed"},
			}
			ids := make([]int64, len(cases))
			for index, item := range cases {
				id, err := store.Insert(t.Context(), testCommandCodec{}, projection, PreparedInsert{
					insert: Insert{RuntimeID: runtimeID, Command: item.name, Status: "running", Queued: item.queued}, storedCommand: item.name,
				})
				if err != nil {
					t.Fatal(err)
				}
				ids[index] = id
				if item.admitted {
					if err := store.ClaimDispatch(t.Context(), projection, id); err != nil {
						t.Fatal(err)
					}
				}
				if item.session {
					if err := store.SetSession(t.Context(), projection, id, 44); err != nil {
						t.Fatal(err)
					}
				}
				if item.finished {
					if err := store.Finish(t.Context(), projection, Completion{ID: id, Status: "completed", Stdout: "known reply"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			switch scope {
			case "lock":
				if err := store.CancelRunning(t.Context(), projection, "workspace locked"); err != nil {
					t.Fatal(err)
				}
			case "runtime":
				count, err := store.CancelRunningForRuntime(t.Context(), projection, runtimeID, "runtime closed")
				if err != nil || count != 4 {
					t.Fatalf("recovery affected=%d err=%v", count, err)
				}
			case "restart":
				var sequence int
				var name, path string
				if err := database.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &path); err != nil {
					t.Fatal(err)
				}
				if err := database.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := dbpkg.OpenEncrypted(path, "test-password")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = reopened.Close() })
				database = reopened
			}
			for index, item := range cases {
				assertCommandFinality(t, database, ids[index], item.want)
			}
		})
	}
}

func assertCommandFinality(t *testing.T, database *sql.DB, id int64, expected string) {
	t.Helper()
	var status, historyStatus, message, historyMessage, output, historyOutput string
	var exitCode, historyExit *int
	if err := database.QueryRowContext(t.Context(), `SELECT cr.status, he.status, cr.error, he.error,
		cr.stdout, he.output_text, cr.exit_code, he.exit_code FROM command_requests cr
		JOIN history_entries he ON he.source_ref_id = cr.id AND he.source_ref_type = 'command_request'
		WHERE cr.id = ?`, id).Scan(&status, &historyStatus, &message, &historyMessage, &output, &historyOutput, &exitCode, &historyExit); err != nil {
		t.Fatal(err)
	}
	if status != expected || historyStatus != status || historyMessage != message || historyOutput != output {
		t.Fatalf("finality projection mismatch id=%d status=%q/%q reason=%q/%q output=%q/%q", id, status, historyStatus, message, historyMessage, output, historyOutput)
	}
	if expected == "completed" {
		if exitCode == nil || *exitCode != 0 || historyExit == nil || *historyExit != 0 || output != "known reply" {
			t.Fatal("known completion was overwritten")
		}
	} else if exitCode != nil || historyExit != nil {
		t.Fatal("recovery invented an exit code")
	}
	if expected == "outcome_unknown" && !strings.Contains(message, "before retrying") {
		t.Fatal("unknown outcome lost reconciliation guidance")
	}
	var count int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_outbox
		WHERE json_extract(payload_json, '$.request_id') = ? AND lifecycle_phase = ?`, id, expected).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit terminal events for request %d status %s: %d", id, expected, count)
	}
}

func TestDispatchSchemaUpgradeDoesNotInventQueuedEvidence(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	if _, err := database.ExecContext(t.Context(), "ALTER TABLE command_requests DROP COLUMN dispatch_state"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), "DELETE FROM schema_migrations WHERE version = 42"); err != nil {
		t.Fatal(err)
	}
	result, err := database.ExecContext(t.Context(), `INSERT INTO command_requests (runtime_id, command, status, created_at)
		VALUES (?, 'previous process command', 'running', datetime('now'))`, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	var seq int
	var name, path string
	if err := database.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := dbpkg.OpenEncrypted(path, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var state string
	if err := reopened.QueryRowContext(t.Context(), "SELECT dispatch_state FROM command_requests WHERE id = ?", id).Scan(&state); err != nil || state != "unknown" {
		t.Fatalf("upgrade invented dispatch evidence %q: %v", state, err)
	}
	assertCommandFinality(t, reopened, id, "outcome_unknown")
	if _, err := reopened.ExecContext(t.Context(), "UPDATE command_requests SET dispatch_state = 'invalid' WHERE id = ?", id); err == nil {
		t.Fatal("invalid dispatch state admitted")
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := dbpkg.OpenEncrypted(path, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	assertCommandFinality(t, again, id, "outcome_unknown")
}
