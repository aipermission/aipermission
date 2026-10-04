package connectortransport

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestNativeTransportProjectionsRemainKindScoped(t *testing.T) {
	fixture := newNativeTransportFixture(t)
	projections := map[string]connectorapi.ConnectorDataRuntime{
		"data":       DataRuntime(fixture.runtime, "carrier"),
		"live":       LiveRuntime(fixture.runtime, "carrier"),
		"action":     ActionRuntime(fixture.runtime, "carrier"),
		"credential": CredentialResourceRuntime(fixture.runtime, "carrier"),
		"lifecycle":  TargetLifecycleRuntime(fixture.runtime, "carrier", nil),
	}
	for name, projection := range projections {
		t.Run(name, func(t *testing.T) {
			target, profile, err := projection.ResolveConnectorActionTarget(t.Context(), fixture.carrier)
			if err != nil || target.Ref != fixture.carrier || target.ConnectorKind != "carrier" || profile.TargetID != target.ID {
				t.Fatalf("positive kind resolution: target=%#v profile=%#v error=%v", target, profile, err)
			}
			if _, _, err := projection.ResolveConnectorActionTarget(t.Context(), fixture.source); !errors.Is(err, connectortargets.ErrInvalidTargetRef) {
				t.Fatalf("foreign connector resolution = %v", err)
			}
		})
	}
	if _, _, err := DataRuntime(Runtime{}, "carrier").ResolveConnectorActionTarget(t.Context(), fixture.carrier); err == nil {
		t.Fatal("missing scope runtime admitted persisted target")
	}
	if ScopedResourceRuntime(fixture.runtime, "carrier").CredentialResources("journal") != nil {
		t.Fatal("resource authority appeared without a configured resource provider")
	}
	if (peerGateway{}).ConnectorTrustStorePath() != "" {
		t.Fatal("empty peer projection supplied a trust store")
	}
}

func TestNativeTransportRuntimeSurfaceAuthority(t *testing.T) {
	fixture := newNativeTransportFixture(t)
	_, targetID, profileID, _ := connectors.ParseTargetRef(fixture.carrier)
	surface, err := fixture.store.EnsureRuntimeSurface(t.Context(), connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: "carrier", TargetID: targetID, ProfileID: profileID, CapabilityKind: "live_console",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireRuntimeID(t.Context(), fixture.runtime, "carrier", surface.ID); err != nil {
		t.Fatalf("positive runtime resolution: %v", err)
	}
	if err := RequireTargetRuntimeID(t.Context(), fixture.runtime, "carrier", targetID, surface.ID); err != nil {
		t.Fatalf("positive target/runtime resolution: %v", err)
	}
	if err := RequireRuntimeID(t.Context(), fixture.runtime, "service", surface.ID); err == nil {
		t.Fatal("foreign connector resolved carrier runtime")
	}
	if err := RequireTargetRuntimeID(t.Context(), fixture.runtime, "carrier", targetID+999, surface.ID); !errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		t.Fatalf("foreign target resolved carrier runtime: %v", err)
	}
	if err := RequireRuntimeID(t.Context(), Runtime{}, "carrier", surface.ID); err == nil {
		t.Fatal("missing scope admitted runtime surface")
	}
}

func TestTransportPrincipalProjectionPreservesExactIdentityAndFailure(t *testing.T) {
	principal, err := executionprincipal.MCPToken(9007199254740993, "workspace-\u03b1", "runtime-\u03b2")
	if err != nil {
		t.Fatal(err)
	}
	port := TargetLifecycleRuntime(Runtime{}, "carrier", func() (executionprincipal.Principal, error) { return principal, nil })
	got, err := port.ConnectorLocalExecutionPrincipal()
	if err != nil || string(got.Kind) != "mcp_token" || got.TokenID != 9007199254740993 || got.WorkspaceID != "workspace-\u03b1" || got.RuntimeInstanceID != "runtime-\u03b2" {
		t.Fatalf("principal projection = %#v error=%v", got, err)
	}
	wantErr := errors.New("principal unavailable")
	port = TargetLifecycleRuntime(Runtime{}, "carrier", func() (executionprincipal.Principal, error) { return principal, wantErr })
	if got, err := port.ConnectorLocalExecutionPrincipal(); got != (connectorapi.Principal{}) || err != wantErr {
		t.Fatalf("principal failure leaked identity: %#v %v", got, err)
	}
	if got, err := TargetLifecycleRuntime(Runtime{}, "carrier", nil).ConnectorLocalExecutionPrincipal(); got != (connectorapi.Principal{}) || err == nil {
		t.Fatalf("missing principal admitted identity: %#v %v", got, err)
	}
}

func TestNativeTransportCapabilityCatalog(t *testing.T) {
	fixture := newNativeTransportFixture(t)
	target, profile, err := fixture.store.ResolveConnectorActionTarget(t.Context(), fixture.carrier)
	if err != nil {
		t.Fatal(err)
	}
	lookups := 0
	capabilities := NewCapabilities(Dependencies{Runtime: fixture.runtime, AdapterFor: func(string) connectorapi.Adapter {
		lookups++
		return nil
	}}, []actions.ResolvedDependency{
		{Purpose: "command_transport", Target: target, Profile: profile},
		{Purpose: "network_transport", Target: target, Profile: profile},
	})
	command, commandOK := capabilities.RuntimeCapability("command_transport").(connectors.CommandTransport)
	network, networkOK := capabilities.RuntimeCapability("network_transport").(connectors.NetworkTransport)
	if len(capabilities) != 2 || !commandOK || !networkOK || command.ConnectorRuntimeCapability() != "command_transport" || network.ConnectorRuntimeCapability() != "network_transport" || capabilities.RuntimeCapability("unknown") != nil {
		t.Fatalf("unexpected capability catalog: %#v", capabilities)
	}
	for _, name := range []string{"command_transport", "network_transport"} {
		var approved Approved
		switch value := capabilities.RuntimeCapability(name).(type) {
		case Command:
			approved = value.Approved
		case Network:
			approved = value.Approved
		}
		if len(approved) != 2 {
			t.Fatalf("catalog lost dependency snapshots for %s: %#v", name, approved)
		}
		release, err := approved.Acquire(t.Context(), fixture.runtime, name, fixture.carrier)
		if err != nil || release == nil {
			t.Fatalf("catalog lost %s snapshot: release=%t error=%v", name, release != nil, err)
		}
		release()
	}
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE connector_targets SET config_json = '{"endpoint":"changed"}' WHERE id = ?`, target.ID); err != nil {
		t.Fatal(err)
	}
	if result, err := command.RunConnectorCommand(t.Context(), connectors.CommandRunRequest{
		SourceTargetRef: fixture.source, TransportTargetRef: fixture.carrier, Command: "fixture-command",
	}); result != (connectors.CommandRunResult{}) || !errors.Is(err, ErrApprovalChanged) {
		t.Fatalf("command catalog failed to retain snapshot: %#v %v", result, err)
	}
	if conn, err := network.DialConnectorTCP(t.Context(), connectors.NetworkDialRequest{
		Mode: "connector", SourceTargetRef: fixture.source, TransportTargetRef: fixture.carrier, Host: "127.0.0.1", Port: 4321,
	}); conn != nil || !errors.Is(err, ErrApprovalChanged) {
		t.Fatalf("network catalog failed to retain snapshot: conn=%t %v", conn != nil, err)
	}
	if lookups != 0 {
		t.Fatalf("catalog snapshot drift reached provider: lookups=%d", lookups)
	}
	fixture.requireQuiescent(t)
}
