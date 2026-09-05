package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type liveConsoleLookupTestAdapter struct {
	ref string
	err error
}

func (liveConsoleLookupTestAdapter) LiveConsoleCapabilityKind() string {
	return connectortargets.RuntimeCapabilityLiveConsole
}

func (adapter liveConsoleLookupTestAdapter) LiveConsoleTargetRef(context.Context, connectorapi.LiveConsoleRuntime, int64) (string, error) {
	return adapter.ref, adapter.err
}

func (liveConsoleLookupTestAdapter) LiveConsoleTargetMetadata(connectors.TargetView, connectors.CredentialProfileView) map[string]any {
	return nil
}

func TestLiveConsoleTargetRefPreservesUnexpectedAdapterErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "database", err: errors.New("query runtime surface: database closed")},
		{name: "cancellation", err: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAPITestFixture(t)
			runtime := fixture.server.activeRuntime()
			if err := runtime.registry.Register(localActionTestConnector{}); err != nil {
				t.Fatal(err)
			}
			if err := runtime.adapterRegistry.Register(localActionTestConnectorKind, liveConsoleLookupTestAdapter{err: test.err}); err != nil {
				t.Fatal(err)
			}

			_, err := liveConsoleTargetRefForRuntimeID(context.Background(), runtime, 999_999)
			if !errors.Is(err, test.err) {
				t.Fatalf("error = %v, want wrapped %v", err, test.err)
			}
		})
	}
}

func TestLiveConsoleTargetRefContinuesOnlyForRuntimeNotFound(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	if err := runtime.registry.Register(localActionTestConnector{}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.adapterRegistry.Register(localActionTestConnectorKind, liveConsoleLookupTestAdapter{err: connectortargets.ErrRuntimeSurfaceNotFound}); err != nil {
		t.Fatal(err)
	}

	_, err := liveConsoleTargetRefForRuntimeID(context.Background(), runtime, 999_999)
	if !errors.Is(err, connectortargets.ErrInvalidTargetRef) {
		t.Fatalf("error = %v, want invalid target ref after all adapters miss", err)
	}
}

func TestLiveConsoleTargetRefRejectsEmptySuccessfulReference(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	if err := runtime.registry.Register(localActionTestConnector{}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.adapterRegistry.Register(localActionTestConnectorKind, liveConsoleLookupTestAdapter{}); err != nil {
		t.Fatal(err)
	}

	_, err := liveConsoleTargetRefForRuntimeID(context.Background(), runtime, 999_999)
	if err == nil || !strings.Contains(err.Error(), "empty target reference") {
		t.Fatalf("error = %v", err)
	}
}
