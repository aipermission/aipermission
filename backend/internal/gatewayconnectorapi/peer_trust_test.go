package gatewayconnectorapi

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestPeerTrustChangeLocksAndInvalidatesWorkspacesInIdentifierOrder(t *testing.T) {
	steps := []string{}
	workspace := func(identifier string) PeerTrustWorkspace {
		return PeerTrustWorkspace{
			Identifier: identifier,
			AcquireExclusive: func(context.Context) (func(), error) {
				steps = append(steps, "lock:"+identifier)
				return func() { steps = append(steps, "release:"+identifier) }, nil
			},
			InvalidateAll: func(_ context.Context, reason string) error {
				if reason != "connector peer trust changed; send a fresh Vault request" {
					t.Fatalf("reason = %q", reason)
				}
				steps = append(steps, "invalidate:"+identifier)
				return nil
			},
		}
	}
	coordinator := NewPeerTrustCoordinator(func() []PeerTrustWorkspace {
		return []PeerTrustWorkspace{workspace("zeta"), workspace("alpha")}
	})

	err := coordinator.Change(t.Context(), func() error {
		steps = append(steps, "change")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"lock:alpha", "lock:zeta", "invalidate:alpha", "invalidate:zeta", "change",
		"release:zeta", "release:alpha",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %#v, want %#v", steps, want)
	}
}

func TestPeerTrustChangeReleasesAcquiredLocksWhenLaterLockFails(t *testing.T) {
	want := errors.New("lock failed")
	steps := []string{}
	coordinator := NewPeerTrustCoordinator(func() []PeerTrustWorkspace {
		return []PeerTrustWorkspace{
			{
				Identifier: "alpha",
				AcquireExclusive: func(context.Context) (func(), error) {
					steps = append(steps, "lock:alpha")
					return func() { steps = append(steps, "release:alpha") }, nil
				},
				InvalidateAll: func(context.Context, string) error { return nil },
			},
			{
				Identifier: "zeta",
				AcquireExclusive: func(context.Context) (func(), error) {
					steps = append(steps, "lock:zeta")
					return nil, want
				},
				InvalidateAll: func(context.Context, string) error { return nil },
			},
		}
	})

	err := coordinator.Change(t.Context(), func() error {
		t.Fatal("change ran after lock failure")
		return nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if !reflect.DeepEqual(steps, []string{"lock:alpha", "lock:zeta", "release:alpha"}) {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestPeerTrustChangeRejectsMissingState(t *testing.T) {
	tests := []struct {
		name        string
		coordinator *PeerTrustCoordinator
		change      func() error
		want        error
	}{
		{name: "change", coordinator: NewPeerTrustCoordinator(func() []PeerTrustWorkspace { return nil }), want: ErrPeerTrustChangeRequired},
		{name: "coordinator", change: func() error { return nil }, want: ErrPeerTrustUnavailable},
		{name: "workspace", coordinator: NewPeerTrustCoordinator(func() []PeerTrustWorkspace { return nil }), change: func() error { return nil }, want: ErrWorkspaceLocked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.coordinator.Change(t.Context(), test.change)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
