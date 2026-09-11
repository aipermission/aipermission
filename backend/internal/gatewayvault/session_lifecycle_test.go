package gatewayvault

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
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
	var closed func(VaultSessionReference)
	lifecycle, err := New(Dependencies{}).SessionLifecycle(SessionLifecycleRuntime{
		Database: &sql.DB{}, Leases: leases, Sessions: sessions,
		Principal: func() (executionprincipal.Principal, error) { return executionprincipal.Principal{}, nil },
		Requests:  func(context.Context) (RequestInvalidator, error) { return sessionLifecycleRequests{}, nil },
		AcquireDelivery: func(context.Context) (func(), error) {
			steps = append(steps, "acquire")
			return func() { steps = append(steps, "release") }, nil
		},
		InstallAuthorizer:    func(value SessionAuthorizationGuard) { guard = value },
		InstallSessionClosed: func(value func(VaultSessionReference)) { closed = value },
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
		InstallSessionClosed: func(func(VaultSessionReference)) {},
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
