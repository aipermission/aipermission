package connectorruntime

import (
	"reflect"
	"slices"
	"testing"
)

func TestAdapterPortsExposeOnlyDeclaredAuthority(t *testing.T) {
	scope := NewScope("test", Dependencies{})
	tests := []struct {
		name    string
		value   any
		methods []string
	}{
		{name: "data", value: scope.DataRuntime(), methods: []string{"CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "TargetProfileByRuntimeID"}},
		{name: "live console", value: scope.LiveConsoleRuntime(), methods: []string{"CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "TargetProfileByRuntimeID"}},
		{name: "action", value: scope.ActionRuntime(), methods: []string{"ConnectorConsoleSessions", "CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "TargetProfileByRuntimeID"}},
		{name: "transfer", value: scope.TransferRuntime(), methods: []string{"CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "ResolveRuntimeContext", "TargetProfileByRuntimeID"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			typeValue := reflect.TypeOf(test.value)
			methods := make([]string, 0, typeValue.NumMethod())
			for index := 0; index < typeValue.NumMethod(); index++ {
				methods = append(methods, typeValue.Method(index).Name)
			}
			slices.Sort(methods)
			slices.Sort(test.methods)
			if !slices.Equal(methods, test.methods) {
				t.Fatalf("concrete methods = %v, want %v", methods, test.methods)
			}
		})
	}
}
