package connectorports

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

type unsupportedSessionEnvironment struct{}

func (unsupportedSessionEnvironment) Len() int { return 1 }
func (unsupportedSessionEnvironment) ForEach(func(string, []byte, bool, int64, int64, int64) error) error {
	return nil
}

func TestEnvironmentAdapterPreservesLifecycle(t *testing.T) {
	environment, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{Name: "SAFE_TOKEN", Value: []byte("top-secret-value")}})
	if err != nil {
		t.Fatal(err)
	}
	released := 0
	postValidated := 0
	var finalized connectorapi.ConsoleSessionHandle
	preparer := coreEnvironmentPreparer(func(_ context.Context, peerIdentity string) (connectorapi.LiveConsoleEnvironmentPreparation, error) {
		if peerIdentity != "peer" {
			t.Fatalf("peer identity = %q", peerIdentity)
		}
		return connectorapi.LiveConsoleEnvironmentPreparation{
			Environment:  environment,
			Release:      func() { released++ },
			PostValidate: func(context.Context) error { postValidated++; return nil },
			Finalize: func(_ context.Context, handle connectorapi.ConsoleSessionHandle) error {
				finalized = handle
				return nil
			},
		}, nil
	})
	prepared, err := preparer(t.Context(), "peer")
	if err != nil || prepared.Environment != environment || prepared.Release == nil || prepared.PostValidate == nil || prepared.Finalize == nil {
		t.Fatalf("prepared = %#v, err=%v", prepared, err)
	}
	prepared.Release()
	if err := prepared.PostValidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(t.Context(), console.SessionHandle{ID: 3, RuntimeID: 5, Generation: 7}); err != nil {
		t.Fatal(err)
	}
	if released != 1 || postValidated != 1 || finalized != (connectorapi.ConsoleSessionHandle{ID: 3, RuntimeID: 5, Generation: 7}) {
		t.Fatalf("released=%d post=%d finalized=%#v", released, postValidated, finalized)
	}
}

func TestEnvironmentAdapterRejectsForeignImplementationAndReleases(t *testing.T) {
	released := 0
	preparer := coreEnvironmentPreparer(func(context.Context, string) (connectorapi.LiveConsoleEnvironmentPreparation, error) {
		return connectorapi.LiveConsoleEnvironmentPreparation{
			Environment: unsupportedSessionEnvironment{}, Release: func() { released++ },
		}, nil
	})
	if _, err := preparer(t.Context(), "peer"); err == nil || released != 1 {
		t.Fatalf("error=%v released=%d", err, released)
	}
}

func TestErrorMappingPreservesBoundarySemantics(t *testing.T) {
	tests := []struct {
		input error
		want  error
	}{
		{input: console.ErrNotFound, want: connectorapi.ErrLiveConsoleNotFound},
		{input: console.ErrSessionLimit, want: connectorapi.ErrLiveConsoleSessionLimit},
		{input: console.ErrClientLimit, want: connectorapi.ErrLiveConsoleClientLimit},
		{input: console.ErrInputTooLarge, want: connectorapi.ErrLiveConsoleInputTooLarge},
	}
	for _, test := range tests {
		if got := mapError(test.input); !errors.Is(got, test.want) {
			t.Fatalf("map %v = %v, want %v", test.input, got, test.want)
		}
	}
	inactive := mapError(console.InactiveError{Status: "closed", Detail: "done"})
	var boundary connectorapi.LiveConsoleInactiveError
	if !errors.As(inactive, &boundary) || boundary.Status != "closed" || boundary.Detail != "done" {
		t.Fatalf("inactive mapping = %#v", inactive)
	}
}
