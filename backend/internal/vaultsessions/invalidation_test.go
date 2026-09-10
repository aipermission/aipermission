package vaultsessions

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type invalidationLeaseStore struct {
	revoked []console.SessionHandle
	tokens  []int64
	clears  int
}

func (s *invalidationLeaseStore) RevokeSession(handle console.SessionHandle) {
	s.revoked = append(s.revoked, handle)
}
func (s *invalidationLeaseStore) RevokeToken(tokenID int64) { s.tokens = append(s.tokens, tokenID) }
func (s *invalidationLeaseStore) Clear()                    { s.clears++ }

type invalidationSessionCloser struct {
	closed []int64
	err    error
}

func (s *invalidationSessionCloser) Close(
	_ context.Context,
	_ executionprincipal.Principal,
	sessionID int64,
) error {
	s.closed = append(s.closed, sessionID)
	return s.err
}

type invalidationRequests struct {
	contextItemID    int64
	contextBindingID int64
	projectID        int64
	runtimeIDs       []int64
	reason           string
}

func (r *invalidationRequests) StalePendingForContext(_ context.Context, itemID, bindingID int64, reason string) error {
	r.contextItemID, r.contextBindingID, r.reason = itemID, bindingID, reason
	return nil
}
func (r *invalidationRequests) StalePendingForProject(_ context.Context, projectID int64, reason string) error {
	r.projectID, r.reason = projectID, reason
	return nil
}
func (r *invalidationRequests) StalePendingForRuntimes(_ context.Context, runtimeIDs []int64, reason string) error {
	r.runtimeIDs, r.reason = append([]int64(nil), runtimeIDs...), reason
	return nil
}

func TestInvalidatorClosesMutationSessionsBeforeStalingRequests(t *testing.T) {
	database, _, _, runtimeID, sessionID := invalidationFixture(t)
	leases := &invalidationLeaseStore{}
	sessions := &invalidationSessionCloser{}
	requests := &invalidationRequests{}
	owner := newTestInvalidator(t, database, leases, sessions, requests)
	reference := Reference{SessionID: sessionID, RuntimeID: runtimeID, Generation: 1}
	if err := owner.InvalidateMutation(t.Context(), []Reference{reference}, 7, 8, "changed"); err != nil {
		t.Fatal(err)
	}
	if len(leases.revoked) != 1 || leases.revoked[0] != (console.SessionHandle{ID: sessionID, RuntimeID: runtimeID, Generation: 1}) {
		t.Fatalf("revoked leases = %#v", leases.revoked)
	}
	if len(sessions.closed) != 1 || sessions.closed[0] != sessionID {
		t.Fatalf("closed sessions = %v", sessions.closed)
	}
	if requests.contextItemID != 7 || requests.contextBindingID != 8 || requests.reason != "changed" {
		t.Fatalf("request invalidation = %#v", requests)
	}
	var leaseStatus string
	if err := database.QueryRowContext(t.Context(), `SELECT status FROM vault_session_leases WHERE session_id = ?`, sessionID).Scan(&leaseStatus); err != nil {
		t.Fatal(err)
	}
	if leaseStatus != "revoked" {
		t.Fatalf("persisted lease status = %q", leaseStatus)
	}
}

func TestInvalidatorSelectsProjectTargetAndAllScopes(t *testing.T) {
	database, projectID, targetID, runtimeID, sessionID := invalidationFixture(t)
	leases := &invalidationLeaseStore{}
	sessions := &invalidationSessionCloser{}
	requests := &invalidationRequests{}
	owner := newTestInvalidator(t, database, leases, sessions, requests)

	if err := owner.InvalidateProject(t.Context(), projectID, "project changed"); err != nil {
		t.Fatal(err)
	}
	if requests.projectID != projectID || len(sessions.closed) != 1 || sessions.closed[0] != sessionID {
		t.Fatalf("project invalidation: requests=%#v sessions=%v", requests, sessions.closed)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE vault_session_leases SET status = 'active' WHERE session_id = ?`, sessionID); err != nil {
		t.Fatal(err)
	}
	sessions.closed = nil
	if err := owner.InvalidateTargetProfile(t.Context(), targetID, 0, "target changed"); err != nil {
		t.Fatal(err)
	}
	if len(requests.runtimeIDs) != 1 || requests.runtimeIDs[0] != runtimeID || len(sessions.closed) != 1 {
		t.Fatalf("target invalidation: requests=%#v sessions=%v", requests, sessions.closed)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE vault_session_leases SET status = 'active' WHERE session_id = ?`, sessionID); err != nil {
		t.Fatal(err)
	}
	sessions.closed = nil
	if err := owner.InvalidateAll(t.Context(), "all changed"); err != nil {
		t.Fatal(err)
	}
	if leases.clears != 1 || len(requests.runtimeIDs) != 1 || requests.runtimeIDs[0] != runtimeID || len(sessions.closed) != 1 {
		t.Fatalf("all invalidation: leases=%#v requests=%#v sessions=%v", leases, requests, sessions.closed)
	}
}

func TestInvalidatorFinishesTokenCleanupAfterCallerCancellation(t *testing.T) {
	database, _, _, _, sessionID := invalidationFixture(t)
	leases := &invalidationLeaseStore{}
	sessions := &invalidationSessionCloser{}
	owner := newTestInvalidator(t, database, leases, sessions, &invalidationRequests{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := owner.FinishTokenInvalidation(ctx, 17, []int64{sessionID}); err != nil {
		t.Fatal(err)
	}
	if len(leases.tokens) != 1 || leases.tokens[0] != 17 || len(sessions.closed) != 1 {
		t.Fatalf("token cleanup: leases=%#v sessions=%v", leases, sessions.closed)
	}
}

func TestInvalidatorFailsClosedAndJoinsCleanupErrors(t *testing.T) {
	if _, err := NewInvalidator(InvalidatorDependencies{}); !errors.Is(err, ErrInvalidatorUnavailable) {
		t.Fatalf("NewInvalidator() error = %v", err)
	}
	database, _, _, runtimeID, sessionID := invalidationFixture(t)
	leases := &invalidationLeaseStore{}
	closeFailure := errors.New("close failed")
	requestFailure := errors.New("request provider failed")
	owner, err := NewInvalidator(InvalidatorDependencies{
		Persistence: NewPersistence(database), Leases: leases,
		Sessions: &invalidationSessionCloser{err: closeFailure},
		Principal: func() (executionprincipal.Principal, error) {
			return executionprincipal.LocalOperator("workspace", "runtime")
		},
		Requests: func(context.Context) (RequestInvalidator, error) { return nil, requestFailure },
	})
	if err != nil {
		t.Fatal(err)
	}
	err = owner.InvalidateMutation(t.Context(), []Reference{{SessionID: sessionID, RuntimeID: runtimeID, Generation: 1}}, 1, 0, "changed")
	if !errors.Is(err, closeFailure) || !errors.Is(err, requestFailure) {
		t.Fatalf("InvalidateMutation() error = %v", err)
	}
}

func TestInvalidatorIgnoresEmptyRuntimeScope(t *testing.T) {
	database, _, _, _, _ := invalidationFixture(t)
	requests := &invalidationRequests{}
	owner := newTestInvalidator(
		t, database, &invalidationLeaseStore{}, &invalidationSessionCloser{}, requests,
	)
	if err := owner.InvalidateRuntimes(t.Context(), nil, "nothing changed"); err != nil {
		t.Fatal(err)
	}
	if requests.reason != "" || requests.runtimeIDs != nil {
		t.Fatalf("empty scope invalidated requests: %#v", requests)
	}
}

func newTestInvalidator(
	t *testing.T,
	database *sql.DB,
	leases *invalidationLeaseStore,
	sessions *invalidationSessionCloser,
	requests *invalidationRequests,
) *Invalidator {
	t.Helper()
	owner, err := NewInvalidator(InvalidatorDependencies{
		Persistence: NewPersistence(database), Leases: leases, Sessions: sessions,
		Principal: func() (executionprincipal.Principal, error) {
			return executionprincipal.LocalOperator("workspace", "runtime")
		},
		Requests: func(context.Context) (RequestInvalidator, error) { return requests, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func invalidationFixture(t *testing.T) (*sql.DB, int64, int64, int64, int64) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "invalidation-runtime.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	project, err := projectstore.NewStore(database).Create(t.Context(), "Invalidation Runtime")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := database.ExecContext(t.Context(), `
		INSERT INTO connector_targets (project_id, connector_kind, name, status, config_json, created_at, updated_at)
		VALUES (?, 'test', 'target', 'active', '{}', datetime('now'), datetime('now'))`, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = database.ExecContext(t.Context(), `
		INSERT INTO connector_credential_profiles (target_id, connector_kind, kind, label, encrypted_secret_json, status, created_at, updated_at)
		VALUES (?, 'test', 'test', 'profile', '{}', 'active', datetime('now'), datetime('now'))`, targetID)
	if err != nil {
		t.Fatal(err)
	}
	profileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = database.ExecContext(t.Context(), `
		INSERT INTO connector_runtime_surfaces (connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at)
		VALUES ('test', ?, ?, 'live_console', 'console', 'active', datetime('now'), datetime('now'))`, targetID, profileID)
	if err != nil {
		t.Fatal(err)
	}
	runtimeID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = database.ExecContext(t.Context(), `
		INSERT INTO console_sessions (
			runtime_id, name, status, generation, environment_content_hash, created_at, updated_at
		) VALUES (?, 'Vault session', 'connected', 1, 'environment', datetime('now'), datetime('now'))`, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := NewPersistence(database).Grant(t.Context(), project.ID, Lease{
		TokenID: token.ID, RuntimeID: runtimeID, SessionID: sessionID, SessionGeneration: 1,
		ApprovalContextHash: "approval", EnvironmentContentHash: "environment",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return database, project.ID, targetID, runtimeID, sessionID
}
