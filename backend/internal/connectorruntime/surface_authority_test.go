package connectorruntime

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectorcredentials"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestRuntimePortsPreserveTargetProfileCapabilityAuthority(t *testing.T) {
	database, _ := openScopedResourceFixture(t, filepath.Join(t.TempDir(), "surfaces.aipdb"))
	store := connectortargets.NewStore(database)
	create := func(kind string) (connectortargets.Target, connectortargets.CredentialProfile) {
		t.Helper()
		target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: kind, Name: kind + "-target"})
		if err != nil {
			t.Fatal(err)
		}
		profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{TargetID: target.ID, ConnectorKind: kind, Kind: "password", Label: "main"})
		if err != nil {
			t.Fatal(err)
		}
		return target, profile
	}
	target, profile := create("fixture")
	foreignTarget, foreignProfile := create("alternate")
	accessorCalls := 0
	scope := NewScope("fixture", Dependencies{Database: database, SecretAccessor: func(secrets map[string]any) connectors.SecretAccessor {
		accessorCalls++
		return connectorcredentials.Secrets(secrets, actionresult.NewCredentialBoundary(secrets))
	}})
	data := scope.DataRuntime()
	ensure := func(kind string, targetID, profileID int64, capability string) connectorapi.RuntimeSurface {
		t.Helper()
		owner := NewScope(kind, Dependencies{Database: database}).DataRuntime()
		surface, err := owner.EnsureRuntimeSurface(t.Context(), connectorapi.EnsureRuntimeSurfaceInput{TargetID: targetID, ProfileID: profileID, CapabilityKind: capability})
		if err != nil {
			t.Fatal(err)
		}
		if surface.ConnectorKind != kind || surface.TargetID != targetID || surface.ProfileID != profileID || surface.CapabilityKind != capability || surface.Status != "active" {
			t.Fatal("surface projection lost declared kind/profile/capability authority")
		}
		return surface
	}
	consoleSurface := ensure("fixture", target.ID, profile.ID, connectortargets.RuntimeCapabilityLiveConsole)
	transferSurface := ensure("fixture", target.ID, profile.ID, connectortargets.RuntimeCapabilityFileTransfer)
	foreignSurface := ensure("alternate", foreignTarget.ID, foreignProfile.ID, connectortargets.RuntimeCapabilityFileTransfer)
	if transferSurface.ID == profile.ID || transferSurface.ID == consoleSurface.ID {
		t.Fatal("fixture did not create independent runtime identities")
	}
	resolvedTarget, resolvedProfile, resolvedSurface, err := data.TargetProfileByRuntimeID(t.Context(), transferSurface.ID)
	if err != nil || resolvedTarget.ID != target.ID || resolvedProfile.ID != profile.ID || resolvedSurface != transferSurface {
		t.Fatalf("runtime binding lost: %v", err)
	}
	ref := connectors.FormatTargetRef("fixture", target.ID, profile.ID)
	resolvedTarget, resolvedProfile, err = data.ResolveConnectorActionTarget(t.Context(), ref)
	if err != nil || resolvedTarget.Ref != ref || resolvedProfile.ID != profile.ID {
		t.Fatalf("action target binding lost: %v", err)
	}
	profiles, err := data.ListCredentialProfiles(t.Context(), target.ID)
	if err != nil || len(profiles) != 1 || profiles[0].ID != profile.ID {
		t.Fatalf("profile enumeration lost binding: %v", err)
	}
	surfaces, err := data.ListRuntimeSurfacesForProfile(t.Context(), target.ID, profile.ID, "")
	if err != nil || len(surfaces) != 2 {
		t.Fatalf("capability enumeration = %d, %v", len(surfaces), err)
	}
	expectedCapabilities := map[int64]string{consoleSurface.ID: connectortargets.RuntimeCapabilityLiveConsole, transferSurface.ID: connectortargets.RuntimeCapabilityFileTransfer}
	for _, surface := range surfaces {
		capability, expected := expectedCapabilities[surface.ID]
		if !expected || surface.CapabilityKind != capability || surface.ConnectorKind != "fixture" || surface.TargetID != target.ID || surface.ProfileID != profile.ID {
			t.Fatal("runtime enumeration lost exact ID/capability binding")
		}
		delete(expectedCapabilities, surface.ID)
	}
	if len(expectedCapabilities) != 0 {
		t.Fatal("runtime enumeration omitted declared capability")
	}
	runtime, surface, err := scope.TransferRuntime().ResolveRuntimeContext(t.Context(), transferSurface.ID, connectortargets.RuntimeCapabilityFileTransfer)
	if err != nil || runtime.Target.ID != target.ID || runtime.Profile.ID != profile.ID || surface != transferSurface || accessorCalls != 1 {
		t.Fatalf("transfer identity resolution = %v, accessor calls=%d", err, accessorCalls)
	}
	before := accessorCalls
	foreignRuntime, foreignResult, err := scope.TransferRuntime().ResolveRuntimeContext(t.Context(), foreignSurface.ID, connectortargets.RuntimeCapabilityFileTransfer)
	if !errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) || foreignRuntime.Secrets != nil || foreignRuntime.Target.ID != 0 || foreignResult.ID != 0 || accessorCalls != before {
		t.Fatalf("foreign secret resolution retained authority: %v", err)
	}
	for _, runtimeID := range []int64{consoleSurface.ID, transferSurface.ID} {
		if err := scope.RequireRuntimeID(t.Context(), runtimeID); err != nil {
			t.Fatal(err)
		}
		if err := scope.RequireTargetRuntimeID(t.Context(), target.ID, runtimeID); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		call func() error
		want error
	}{
		{"missing_runtime", func() error { return scope.RequireRuntimeID(t.Context(), 0) }, connectortargets.ErrRuntimeSurfaceNotFound},
		{"foreign_runtime", func() error { return scope.RequireRuntimeID(t.Context(), foreignSurface.ID) }, connectortargets.ErrRuntimeSurfaceNotFound},
		{"foreign_target_binding", func() error { return scope.RequireTargetRuntimeID(t.Context(), foreignTarget.ID, transferSurface.ID) }, connectortargets.ErrRuntimeSurfaceNotFound},
		{"foreign_action_target", func() error {
			_, _, err := data.ResolveConnectorActionTarget(t.Context(), connectors.FormatTargetRef("alternate", foreignTarget.ID, foreignProfile.ID))
			return err
		}, connectortargets.ErrInvalidTargetRef},
		{"foreign_profile_list", func() error { _, err := data.ListCredentialProfiles(t.Context(), foreignTarget.ID); return err }, connectortargets.ErrTargetNotFound},
		{"foreign_surface_list", func() error {
			_, err := data.ListRuntimeSurfacesForProfile(t.Context(), foreignTarget.ID, foreignProfile.ID, "")
			return err
		}, connectortargets.ErrTargetNotFound},
		{"foreign_surface_create", func() error {
			_, err := data.EnsureRuntimeSurface(t.Context(), connectorapi.EnsureRuntimeSurfaceInput{TargetID: foreignTarget.ID, ProfileID: foreignProfile.ID, CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole})
			return err
		}, connectortargets.ErrTargetNotFound},
		{"capability_mismatch", func() error {
			_, _, err := scope.TransferRuntime().ResolveRuntimeContext(t.Context(), consoleSurface.ID, connectortargets.RuntimeCapabilityFileTransfer)
			return err
		}, connectortargets.ErrRuntimeSurfaceNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := accessorCalls
			if err := tc.call(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if accessorCalls != before {
				t.Fatal("rejected authority reached secret accessor")
			}
		})
	}
	remaining, err := store.ListRuntimeSurfacesForProfile(t.Context(), foreignTarget.ID, foreignProfile.ID, "")
	if err != nil || len(remaining) != 1 || remaining[0].ID != foreignSurface.ID {
		t.Fatalf("rejected creation changed foreign runtimes: %v", err)
	}
}
