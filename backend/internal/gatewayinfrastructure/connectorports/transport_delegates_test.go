package connectorports

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
)

type delegateNetworkProbe struct {
	connectors.NetworkTransport
	dial func(context.Context, connectors.NetworkDialRequest) (net.Conn, error)
}

func (probe delegateNetworkProbe) DialConnectorTCP(ctx context.Context, input connectors.NetworkDialRequest) (net.Conn, error) {
	return probe.dial(ctx, input)
}

type delegateCommandProbe struct {
	connectors.CommandTransport
	run func(context.Context, connectors.CommandRunRequest) (connectors.CommandRunResult, error)
}

func (probe delegateCommandProbe) RunConnectorCommand(ctx context.Context, input connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	return probe.run(ctx, input)
}

func TestTransportDelegatesPreserveContextRequestResultAndError(t *testing.T) {
	ctx, failure := t.Context(), errors.New("transport failure")
	networkRequest := connectors.NetworkDialRequest{Host: "fixture", Port: 22}
	commandRequest := connectors.CommandRunRequest{Command: "fixture-command", TimeoutSeconds: 12}
	result := connectors.CommandRunResult{Stdout: "fixture", ExitCode: 7, DispatchStarted: true}
	network := networkDelegate{transport: delegateNetworkProbe{dial: func(got context.Context, input connectors.NetworkDialRequest) (net.Conn, error) {
		if got != ctx || input != networkRequest {
			t.Fatal("network delegate changed context or request")
		}
		return nil, failure
	}}}
	command := commandDelegate{transport: delegateCommandProbe{run: func(got context.Context, input connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
		if got != ctx || input != commandRequest {
			t.Fatal("command delegate changed context or request")
		}
		return result, failure
	}}}
	if connection, err := network.DialConnectorTCP(ctx, networkRequest); connection != nil || err != failure || network.ConnectorRuntimeCapability() != connectors.NetworkTransportCapabilityName {
		t.Fatalf("network delegate=%v %v", connection, err)
	}
	if got, err := command.RunConnectorCommand(ctx, commandRequest); got != result || err != failure || command.ConnectorRuntimeCapability() != connectors.CommandTransportCapabilityName {
		t.Fatalf("command delegate=%#v %v", got, err)
	}
}

func TestTransportFactoriesExposeNoImplementationOrExportedAuthority(t *testing.T) {
	for _, value := range []connectors.RuntimeCapability{
		NetworkTransport(Workspace{}, nil, nil), CommandTransport(Workspace{}, nil, nil),
		ApprovedNetworkTransport(Workspace{}, nil, nil, nil), ApprovedCommandTransport(Workspace{}, nil, nil, nil),
	} {
		if _, exposed := value.(connectortransport.Network); exposed {
			t.Fatal("factory exposed concrete network dependencies")
		}
		if _, exposed := value.(connectortransport.Command); exposed {
			t.Fatal("factory exposed concrete command dependencies")
		}
		kind := reflect.TypeOf(value)
		for index := range kind.NumField() {
			field := kind.Field(index)
			if field.IsExported() || field.Anonymous {
				t.Fatalf("factory exposed authority through %s.%s", kind, field.Name)
			}
		}
		if kind.NumMethod() != 2 {
			t.Fatalf("factory exposed extra authority methods: %v", kind)
		}
	}
}
