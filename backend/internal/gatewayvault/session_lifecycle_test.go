package gatewayvault

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
)

type sessionLifecycleLeases struct {
	steps *[]string
}

func (leases *sessionLifecycleLeases) RevokeSession(console.SessionHandle) {
	*leases.steps = append(*leases.steps, "revoke")
}
func (*sessionLifecycleLeases) RevokeToken(int64) {}
func (*sessionLifecycleLeases) Clear()            {}

type sessionLifecycleSessions struct{}

func (*sessionLifecycleSessions) Close(context.Context, executionprincipal.Principal, int64) error {
	return nil
}

type sessionLifecycleRequests struct{}

func (sessionLifecycleRequests) StalePendingForContext(context.Context, int64, int64, string) error {
	return nil
}
func (sessionLifecycleRequests) StalePendingForProject(context.Context, int64, string) error {
	return nil
}
func (sessionLifecycleRequests) StalePendingForRuntimes(context.Context, []int64, string) error {
	return nil
}

func TestSessionLifecycleConfiguresDeliveryGuardedAuthorization(t *testing.T) {
	steps := []string{}
	leases := &sessionLifecycleLeases{steps: &steps}
	sessions := &sessionLifecycleSessions{}
	var guard SessionAuthorizationGuard
	var closed func(context.Context, VaultSessionReference) error
	lifecycle, err := New(Dependencies{}).SessionLifecycle(SessionLifecycleRuntime{
		Database: &sql.DB{}, Leases: leases, Sessions: sessions,
		Principal: func() (executionprincipal.Principal, error) { return executionprincipal.Principal{}, nil },
		Requests:  func(context.Context) (RequestInvalidator, error) { return sessionLifecycleRequests{}, nil },
		AcquireDelivery: func(context.Context) (func(), error) {
			steps = append(steps, "acquire")
			return func() { steps = append(steps, "release") }, nil
		},
		InstallAuthorizer:    func(value SessionAuthorizationGuard) { guard = value },
		InstallSessionClosed: func(value func(context.Context, VaultSessionReference) error) { closed = value },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Configure(); err != nil {
		t.Fatal(err)
	}
	if guard == nil || closed == nil {
		t.Fatal("session lifecycle hooks were not configured")
	}
	err = guard(
		t.Context(), func() error {
			steps = append(steps, "authorize")
			return nil
		},
		func() error {
			steps = append(steps, "run")
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(steps, []string{"acquire", "authorize", "run", "release"}) {
		t.Fatalf("authorization order = %#v", steps)
	}
	closedCtx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := closed(closedCtx, VaultSessionReference{SessionID: 7, RuntimeID: 8, Generation: 9}); !errors.Is(err, context.Canceled) {
		t.Fatalf("session-closed persistence error = %v, want canceled context", err)
	}
}

func TestSessionLifecycleAuthorizationFailureDoesNotRunOperation(t *testing.T) {
	steps := []string{}
	want := errors.New("lease rejected")
	leases := &sessionLifecycleLeases{steps: &steps}
	sessions := &sessionLifecycleSessions{}
	var guard SessionAuthorizationGuard
	lifecycle, err := New(Dependencies{}).SessionLifecycle(SessionLifecycleRuntime{
		Database: &sql.DB{}, Leases: leases, Sessions: sessions,
		Principal: func() (executionprincipal.Principal, error) { return executionprincipal.Principal{}, nil },
		Requests:  func(context.Context) (RequestInvalidator, error) { return sessionLifecycleRequests{}, nil },
		AcquireDelivery: func(context.Context) (func(), error) {
			steps = append(steps, "acquire")
			return func() { steps = append(steps, "release") }, nil
		},
		InstallAuthorizer:    func(value SessionAuthorizationGuard) { guard = value },
		InstallSessionClosed: func(func(context.Context, VaultSessionReference) error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Configure(); err != nil {
		t.Fatal(err)
	}
	err = guard(t.Context(), func() error {
		steps = append(steps, "authorize")
		return want
	}, func() error {
		t.Fatal("operation ran after lease rejection")
		return nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if !reflect.DeepEqual(steps, []string{"acquire", "authorize", "release"}) {
		t.Fatalf("authorization order = %#v", steps)
	}
}

func TestSessionLifecycleRejectsIncompleteRuntime(t *testing.T) {
	_, err := New(Dependencies{}).SessionLifecycle(SessionLifecycleRuntime{})
	if !errors.Is(err, InvalidatorUnavailableError()) {
		t.Fatalf("error = %v, want invalidator unavailable", err)
	}
}

type recoveryRequests struct{ contexts, projects int }

type recoverySessionCloser struct {
	closed   []int64
	failOnce bool
}

func (c *recoverySessionCloser) Close(_ context.Context, _ executionprincipal.Principal, id int64) error {
	c.closed = append(c.closed, id)
	if c.failOnce {
		c.failOnce = false
		return errors.New("injected close failure")
	}
	return nil
}

func (r *recoveryRequests) StalePendingForContext(context.Context, int64, int64, string) error {
	r.contexts++
	return nil
}
func (r *recoveryRequests) StalePendingForProject(context.Context, int64, string) error {
	r.projects++
	return nil
}
func (*recoveryRequests) StalePendingForRuntimes(context.Context, []int64, string) error { return nil }

func TestSessionLifecycleRecoversDurableFinalizations(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "recover.aipdb"), "VaultRecoveryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := vaultfinalization.NewStore(database)
	for _, intent := range []vaultfinalization.Intent{
		{Kind: "mutation", ItemID: 4},
		{Kind: "project", ProjectID: 7, Reason: "archived", References: []vaultfinalization.Reference{{SessionID: 88, RuntimeID: 2, Generation: 1}}},
	} {
		if _, err := store.Queue(t.Context(), intent); err != nil {
			t.Fatal(err)
		}
	}
	steps := []string{}
	requests := &recoveryRequests{}
	closer := &recoverySessionCloser{failOnce: true}
	lifecycle, err := New(Dependencies{}).SessionLifecycle(SessionLifecycleRuntime{
		Database: database, Leases: &sessionLifecycleLeases{steps: &steps}, Sessions: closer,
		Principal:            func() (executionprincipal.Principal, error) { return executionprincipal.Principal{}, nil },
		Requests:             func(context.Context) (RequestInvalidator, error) { return requests, nil },
		AcquireDelivery:      func(context.Context) (func(), error) { return func() {}, nil },
		InstallAuthorizer:    func(SessionAuthorizationGuard) {},
		InstallSessionClosed: func(func(context.Context, VaultSessionReference) error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.RecoverPendingFinalizations(t.Context()); !errors.Is(err, vaultfinalization.ErrPending) {
		t.Fatalf("first recovery = %v", err)
	}
	if err := store.RequireReady(t.Context()); !errors.Is(err, vaultfinalization.ErrBlocked) {
		t.Fatalf("readiness after failed recovery = %v", err)
	}
	if err := lifecycle.RecoverPendingFinalizations(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.RecoverPendingFinalizations(t.Context()); err != nil {
		t.Fatal(err)
	}
	if requests.contexts != 1 || requests.projects != 2 {
		t.Fatalf("recovery calls = contexts:%d projects:%d", requests.contexts, requests.projects)
	}
	if !reflect.DeepEqual(closer.closed, []int64{88, 88}) {
		t.Fatalf("recovered session closes = %v", closer.closed)
	}
	if err := store.RequireReady(t.Context()); err != nil {
		t.Fatal(err)
	}
}
