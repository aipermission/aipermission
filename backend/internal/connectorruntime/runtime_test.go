package connectorruntime

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
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

func TestScopeRejectsMissingAuthorityAndForeignConnectorKind(t *testing.T) {
	ctx := context.Background()
	for _, scope := range []*Scope{nil, NewScope("fixture", Dependencies{}), NewScope(" ", Dependencies{})} {
		if _, err := scope.store(); !errors.Is(err, ErrInvalidRuntime) {
			t.Fatalf("store with incomplete authority: %v", err)
		}
		if err := scope.requireTarget(ctx, 1); !errors.Is(err, ErrInvalidRuntime) {
			t.Fatalf("requireTarget with incomplete authority: %v", err)
		}
		if _, _, err := scope.resolveTarget(ctx, "fixture:1:1"); !errors.Is(err, ErrInvalidRuntime) {
			t.Fatalf("resolveTarget with incomplete authority: %v", err)
		}
		if _, err := scope.ensureSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{TargetID: 1}); !errors.Is(err, ErrInvalidRuntime) {
			t.Fatalf("ensureSurface with incomplete authority: %v", err)
		}
		if scope.resourcesFor("private_key") != nil {
			t.Fatal("resource access was exposed without backing authority")
		}
	}
	fixture := NewScope("fixture", Dependencies{})
	if _, err := fixture.ensureSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{ConnectorKind: "other", TargetID: 1}); !errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		t.Fatalf("foreign connector surface = %v", err)
	}
}
