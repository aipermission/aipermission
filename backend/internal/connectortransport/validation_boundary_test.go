package connectortransport

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/projects"
)

func TestNativeCommandSourceBoundaryPreventsAdapterLookup(t *testing.T) {
	fixture := newNativeTransportFixture(t)
	other, err := projects.NewStore(fixture.database).Create(t.Context(), "Foreign command fixture")
	if err != nil {
		t.Fatal(err)
	}
	foreign := fixture.addTarget(t, other.ID, "service", "Foreign source")
	lookups := 0
	transport := Command{Dependencies: Dependencies{Runtime: fixture.runtime, AdapterFor: func(string) connectorapi.Adapter {
		lookups++
		return commandAdapterFunc(func(context.Context, connectorapi.PeerIdentityGateway, connectorapi.LiveConsoleRuntime, string, string) (connectors.CommandRunResult, error) {
			t.Error("invalid source dispatched a command")
			return connectors.CommandRunResult{DispatchStarted: true}, nil
		})
	}}}
	for _, source := range []string{foreign, "service:999999:999999", ""} {
		result, err := transport.RunConnectorCommand(t.Context(), connectors.CommandRunRequest{
			SourceTargetRef: source, TransportTargetRef: fixture.carrier, Command: "fixture-command",
		})
		if err == nil || result != (connectors.CommandRunResult{}) || lookups != 0 {
			t.Fatalf("invalid source %q: result=%#v err=%v lookups=%d", source, result, err, lookups)
		}
		fixture.requireQuiescent(t)
	}
}

func TestNativeTransportEarlyFailuresNeverDispatch(t *testing.T) {
	fixture := newNativeTransportFixture(t)
	for _, capability := range []string{"command", "network"} {
		for _, name := range []string{"missing-ref", "invalid-ref", "missing-database", "missing-admission", "nil-release", "nil-provider", "wrong-adapter", "canceled"} {
			t.Run(capability+"/"+name, func(t *testing.T) {
				ctx := t.Context()
				lookups := 0
				dependencies := Dependencies{Runtime: fixture.runtime, AdapterFor: func(string) connectorapi.Adapter {
					lookups++
					return struct{}{}
				}}
				ref := fixture.carrier
				switch name {
				case "missing-ref":
					ref = ""
				case "invalid-ref":
					ref = "not-a-target-ref"
				case "missing-database":
					dependencies.Runtime.Database = nil
				case "missing-admission":
					dependencies.Runtime.AcquireDelivery = nil
				case "nil-release":
					dependencies.Runtime.AcquireDelivery = func(context.Context) (func(), error) { return nil, nil }
				case "nil-provider":
					dependencies.AdapterFor = nil
				case "canceled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				var err error
				if capability == "command" {
					result, commandErr := (Command{Dependencies: dependencies}).RunConnectorCommand(ctx, connectors.CommandRunRequest{
						SourceTargetRef: fixture.source, TransportTargetRef: ref, Command: "fixture-command",
					})
					err = commandErr
					if result != (connectors.CommandRunResult{}) {
						t.Fatalf("early failure produced command output: %#v", result)
					}
				} else {
					conn, dialErr := (Network{Dependencies: dependencies}).DialConnectorTCP(ctx, connectors.NetworkDialRequest{
						Mode: "connector", SourceTargetRef: fixture.source, TransportTargetRef: ref, Host: "127.0.0.1", Port: 4321,
					})
					err = dialErr
					if conn != nil {
						conn.Close()
						t.Fatal("early failure returned a network connection")
					}
				}
				wantLookups := 0
				if name == "wrong-adapter" {
					wantLookups = 1
				}
				if err == nil || lookups != wantLookups || (name == "canceled" && !errors.Is(err, context.Canceled)) {
					t.Fatalf("early failure: error=%v lookups=%d want=%d", err, lookups, wantLookups)
				}
				fixture.requireQuiescent(t)
			})
		}
	}
}
