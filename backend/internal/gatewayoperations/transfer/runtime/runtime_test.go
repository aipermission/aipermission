package transferruntime

import (
	"context"
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

func TestNewRuntimeRejectsMissingRequiredDependencies(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	valid := RuntimeDependencies{
		StorageID: "workspace",
		Database:  database, Jobs: &transferjobs.Registry{},
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
		{name: "storage identity", mutate: func(value *RuntimeDependencies) { value.StorageID = "" }},
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

func TestLifecycleOwnsRegistryAndFinalizationLifetime(t *testing.T) {
	lifecycle := NewLifecycle()
	if lifecycle.Registry() == nil || !lifecycle.finalization.Valid() {
		t.Fatal("new lifecycle is incomplete")
	}
	if !lifecycle.Wait(t.Context()) {
		t.Fatal("empty lifecycle did not drain")
	}
	lifecycle.Stop()
	select {
	case <-lifecycle.finalization.Context().Done():
	default:
		t.Fatal("stopped lifecycle retained its finalization context")
	}
}

func TestNilLifecycleFailsClosed(t *testing.T) {
	var lifecycle *Lifecycle
	if lifecycle.Registry() != nil || !lifecycle.Wait(context.Background()) {
		t.Fatal("nil lifecycle did not remain inert")
	}
	lifecycle.Stop()
}
