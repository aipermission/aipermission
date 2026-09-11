package connectors

import (
	"context"
	"database/sql"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
)

func TestStateOwnsConsoleSessionManagerConstruction(t *testing.T) {
	state := New(nil, nil, &sql.DB{}, nil, "workspace")
	state.ConfigureConsoleSessions(func(context.Context, console.RuntimeOpenRequest) (*console.RuntimeSession, error) {
		return nil, nil
	}, func(value string) string { return value })
	if state.ConsoleSessionManager() == nil {
		t.Fatal("console session manager was not configured")
	}
}

func TestNilStateConsoleSessionConfigurationIsSafe(t *testing.T) {
	var state *State
	state.ConfigureConsoleSessions(nil, nil)
	if state.ConsoleSessionManager() != nil {
		t.Fatal("nil state exposed a console session manager")
	}
}

func TestMissingRegistriesFailClosed(t *testing.T) {
	state := New(nil, nil, &sql.DB{}, nil, "workspace")
	if state.ConnectorRegistry() != nil || state.ConnectorAdapterRegistry() != nil {
		t.Fatal("missing registry wiring was replaced with an empty registry")
	}
}
