package vaultsessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

func TestLeaseRequiresExactPrincipalSessionAndContext(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	lease := Lease{
		WorkspaceID: "workspace", RuntimeInstanceID: "instance", TokenID: 7,
		RuntimeID: 11, SessionID: 12, SessionGeneration: 13,
		EnvironmentContentHash: "environment", ApprovalContextHash: "context", ExpiresAt: now.Add(time.Hour),
	}
	if err := store.Grant(lease); err != nil {
		t.Fatalf("grant: %v", err)
	}
	principal, err := executionprincipal.MCPToken(7, "workspace", "instance")
	if err != nil {
		t.Fatalf("principal: %v", err)
	}
	session := console.SessionAuthorization{
		Handle:                 console.SessionHandle{ID: 12, RuntimeID: 11, Generation: 13},
		EnvironmentContentHash: "environment", ApprovalContextHash: "context",
	}
	if err := store.Authorize(context.Background(), principal, session, console.OperationExecute); err != nil {
		t.Fatalf("authorize exact lease: %v", err)
	}
	session.Handle.Generation++
	if err := store.Authorize(context.Background(), principal, session, console.OperationExecute); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("stale generation = %v", err)
	}
	session.Handle.Generation--
	session.EnvironmentContentHash = "changed-environment"
	if err := store.Authorize(context.Background(), principal, session, console.OperationExecute); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("changed environment = %v", err)
	}
	session.EnvironmentContentHash = "environment"
	store.RevokeToken(7)
	if err := store.Authorize(context.Background(), principal, session, console.OperationExecute); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked token = %v", err)
	}
}

func TestLeaseDoesNotOutliveHardTTL(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if err := store.Grant(Lease{
		WorkspaceID: "workspace", RuntimeInstanceID: "instance", TokenID: 1,
		RuntimeID: 2, SessionID: 3, SessionGeneration: 4,
		EnvironmentContentHash: "environment", ApprovalContextHash: "context", ExpiresAt: now.Add(48 * time.Hour),
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	now = now.Add(MaxLeaseTTL + time.Second)
	principal, err := executionprincipal.MCPToken(1, "workspace", "instance")
	if err != nil {
		t.Fatalf("principal: %v", err)
	}
	session := console.SessionAuthorization{
		Handle:                 console.SessionHandle{ID: 3, RuntimeID: 2, Generation: 4},
		EnvironmentContentHash: "environment", ApprovalContextHash: "context",
	}
	if err := store.Authorize(context.Background(), principal, session, console.OperationObserve); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired hard TTL = %v", err)
	}
}

func TestLeaseRevokesItselfWhenLiveContextValidationFails(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	validations := 0
	lease := Lease{
		WorkspaceID: "workspace", RuntimeInstanceID: "instance", TokenID: 7,
		RuntimeID: 11, SessionID: 12, SessionGeneration: 13,
		EnvironmentContentHash: "environment", ApprovalContextHash: "context", ExpiresAt: now.Add(time.Hour),
		Validate: func(context.Context) error {
			validations++
			return errors.New("context drifted")
		},
	}
	if err := store.Grant(lease); err != nil {
		t.Fatalf("grant: %v", err)
	}
	principal, err := executionprincipal.MCPToken(7, "workspace", "instance")
	if err != nil {
		t.Fatal(err)
	}
	session := console.SessionAuthorization{
		Handle:                 console.SessionHandle{ID: 12, RuntimeID: 11, Generation: 13},
		EnvironmentContentHash: "environment", ApprovalContextHash: "context",
	}
	if err := store.Authorize(context.Background(), principal, session, console.OperationExecute); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("drifted lease = %v", err)
	}
	if validations != 1 {
		t.Fatalf("validation count = %d", validations)
	}
	if err := store.Authorize(context.Background(), principal, session, console.OperationExecute); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked lease = %v", err)
	}
	if validations != 1 {
		t.Fatalf("revoked lease should not revalidate, count = %d", validations)
	}
}

func TestLeaseRejectsDifferentPrincipalAndRuntimeIdentity(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	lease := Lease{
		WorkspaceID: "workspace", RuntimeInstanceID: "instance", TokenID: 7,
		RuntimeID: 11, SessionID: 12, SessionGeneration: 13,
		EnvironmentContentHash: "environment", ApprovalContextHash: "context", ExpiresAt: now.Add(time.Hour),
	}
	if err := store.Grant(lease); err != nil {
		t.Fatal(err)
	}
	session := console.SessionAuthorization{
		Handle:                 console.SessionHandle{ID: 12, RuntimeID: 11, Generation: 13},
		EnvironmentContentHash: "environment", ApprovalContextHash: "context",
	}
	tests := []struct {
		name              string
		tokenID           int64
		workspace         string
		runtimeInstanceID string
		mutateSession     func(*console.SessionAuthorization)
	}{
		{name: "wrong token", tokenID: 8, workspace: "workspace", runtimeInstanceID: "instance"},
		{name: "wrong workspace", tokenID: 7, workspace: "other", runtimeInstanceID: "instance"},
		{name: "wrong runtime instance", tokenID: 7, workspace: "workspace", runtimeInstanceID: "other"},
		{
			name: "wrong runtime", tokenID: 7, workspace: "workspace", runtimeInstanceID: "instance",
			mutateSession: func(value *console.SessionAuthorization) { value.Handle.RuntimeID++ },
		},
		{
			name: "wrong session", tokenID: 7, workspace: "workspace", runtimeInstanceID: "instance",
			mutateSession: func(value *console.SessionAuthorization) { value.Handle.ID++ },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			principal, err := executionprincipal.MCPToken(test.tokenID, test.workspace, test.runtimeInstanceID)
			if err != nil {
				t.Fatal(err)
			}
			candidate := session
			if test.mutateSession != nil {
				test.mutateSession(&candidate)
			}
			if err := store.Authorize(context.Background(), principal, candidate, console.OperationObserve); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("authorize = %v", err)
			}
		})
	}
}

func TestLeaseRevocationScopesAndClear(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	grant := func(tokenID, runtimeID, sessionID int64) {
		t.Helper()
		if err := store.Grant(Lease{
			WorkspaceID: "workspace", RuntimeInstanceID: "instance", TokenID: tokenID,
			RuntimeID: runtimeID, SessionID: sessionID, SessionGeneration: 1,
			EnvironmentContentHash: "environment", ApprovalContextHash: "context",
			ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	authorize := func(tokenID, runtimeID, sessionID int64) error {
		t.Helper()
		principal, err := executionprincipal.MCPToken(tokenID, "workspace", "instance")
		if err != nil {
			t.Fatal(err)
		}
		return store.Authorize(context.Background(), principal, console.SessionAuthorization{
			Handle:                 console.SessionHandle{ID: sessionID, RuntimeID: runtimeID, Generation: 1},
			EnvironmentContentHash: "environment", ApprovalContextHash: "context",
		}, console.OperationObserve)
	}

	grant(1, 10, 100)
	grant(1, 10, 101)
	grant(2, 20, 200)
	store.RevokeSession(console.SessionHandle{ID: 100, RuntimeID: 10, Generation: 1})
	if err := authorize(1, 10, 100); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("session revoke = %v", err)
	}
	if err := authorize(1, 10, 101); err != nil {
		t.Fatalf("unrelated session = %v", err)
	}

	store.RevokeRuntime(10)
	if err := authorize(1, 10, 101); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("runtime revoke = %v", err)
	}
	if err := authorize(2, 20, 200); err != nil {
		t.Fatalf("unrelated runtime = %v", err)
	}

	store.Clear()
	if err := authorize(2, 20, 200); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("clear = %v", err)
	}
}
