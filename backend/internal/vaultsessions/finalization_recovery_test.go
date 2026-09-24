package vaultsessions

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
)

func TestRecoverPendingMutationFinalizationRetriesFailedCleanup(t *testing.T) {
	database, _, _, runtimeID, sessionID := invalidationFixture(t)
	store := vaultfinalization.NewStore(database)
	if _, err := store.Queue(t.Context(), vaultfinalization.Intent{
		Kind: "mutation", ItemID: 7, BindingID: 8,
		References: []vaultfinalization.Reference{{SessionID: sessionID, RuntimeID: runtimeID, Generation: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	closeFailure := errors.New("session close failed")
	sessions := &invalidationSessionCloser{err: closeFailure}
	requests := &invalidationRequests{}
	owner := newTestInvalidator(t, database, &invalidationLeaseStore{}, sessions, requests)

	if err := owner.RecoverPendingFinalizations(t.Context()); !errors.Is(err, vaultfinalization.ErrPending) {
		t.Fatalf("first recovery = %v", err)
	}
	if err := store.RequireReady(t.Context()); !errors.Is(err, vaultfinalization.ErrBlocked) {
		t.Fatalf("pending readiness = %v", err)
	}
	sessions.err = nil
	if err := owner.RecoverPendingFinalizations(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireReady(t.Context()); err != nil {
		t.Fatalf("completed readiness = %v", err)
	}
	if requests.contextItemID != 7 || requests.contextBindingID != 8 || len(sessions.closed) != 2 {
		t.Fatalf("replayed cleanup: requests=%#v sessions=%v", requests, sessions.closed)
	}
}

func TestRecoverPendingProjectFinalizationUsesStoredReferences(t *testing.T) {
	database, projectID, _, runtimeID, sessionID := invalidationFixture(t)
	store := vaultfinalization.NewStore(database)
	if _, err := store.Queue(t.Context(), vaultfinalization.Intent{
		Kind: "project", ProjectID: projectID, Reason: "project archived",
		References: []vaultfinalization.Reference{{SessionID: sessionID, RuntimeID: runtimeID, Generation: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	sessions := &invalidationSessionCloser{}
	requests := &invalidationRequests{}
	owner := newTestInvalidator(t, database, &invalidationLeaseStore{}, sessions, requests)
	if err := owner.RecoverPendingFinalizations(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(sessions.closed) != 1 || sessions.closed[0] != sessionID || requests.projectID != projectID || requests.reason != "project archived" {
		t.Fatalf("project cleanup: sessions=%v requests=%#v", sessions.closed, requests)
	}
	if err := store.RequireReady(t.Context()); err != nil {
		t.Fatal(err)
	}
}
