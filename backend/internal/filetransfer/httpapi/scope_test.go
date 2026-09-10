package filetransferhttp

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

func TestNewRuntimeRejectsMissingRequiredDependencies(t *testing.T) {
	fixture := newTransferTestFixture(t)
	valid := RuntimeDependencies{
		Database:     fixture.database,
		Jobs:         &transferjobs.Registry{},
		Finalization: transferjobs.NewFinalizationLifetime(),
		Observe:      func(context.Context, string, *int64, int64, string, any) {},
		ConnectorPorts: func(context.Context, int64) (ConnectorPorts, error) {
			return ConnectorPorts{}, nil
		},
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*RuntimeDependencies)
	}{
		{name: "database", mutate: func(value *RuntimeDependencies) { value.Database = nil }},
		{name: "jobs", mutate: func(value *RuntimeDependencies) { value.Jobs = nil }},
		{name: "finalization", mutate: func(value *RuntimeDependencies) { value.Finalization = transferjobs.FinalizationLifetime{} }},
		{name: "observer", mutate: func(value *RuntimeDependencies) { value.Observe = nil }},
		{name: "connector resolver", mutate: func(value *RuntimeDependencies) { value.ConnectorPorts = nil }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dependencies := valid
			testCase.mutate(&dependencies)
			if runtime, err := NewRuntime(dependencies); err == nil || runtime != nil {
				t.Fatalf("missing %s accepted: runtime=%#v err=%v", testCase.name, runtime, err)
			}
		})
	}
}

func TestConnectorPortsRejectUninitializedCredentialBoundary(t *testing.T) {
	fixture := newTransferTestFixture(t)
	execution := fixture.execution(t)
	fixture.runtime.connectorPorts = func(context.Context, int64) (ConnectorPorts, error) {
		return ConnectorPorts{
			ConnectorKind: "fixture",
			Gateway:       testTransferGateway{},
			Runtime:       execution.runtime,
		}, nil
	}

	if _, err := connectorFileTransferPortsForID(t.Context(), fixture.runtime, fixture.runtimeID); err == nil {
		t.Fatal("uninitialized credential boundary was accepted")
	}
}
