package connectorports

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type restartScopeFixture struct{ database *sql.DB }

func (fixture restartScopeFixture) ConnectorScope(kind string, _ connectorruntime.SecretAccessorFactory) *connectorruntime.Scope {
	return connectorruntime.NewScope(kind, connectorruntime.Dependencies{Database: fixture.database})
}

func TestRestartPortsPreserveScopedAdmissionAndDelegation(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "restart.db"), "restart-admission-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: "fixture", Name: "Restart fixture"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{TargetID: target.ID, ConnectorKind: "fixture", Kind: "fixture", Label: "Default"})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := store.EnsureRuntimeSurface(t.Context(), connectortargets.EnsureRuntimeSurfaceInput{ConnectorKind: "fixture", TargetID: target.ID, ProfileID: profile.ID, CapabilityKind: "live_console"})
	if err != nil {
		t.Fatal(err)
	}
	principal := connectorapi.Principal{Kind: connectorapi.PrincipalMCPToken, TokenID: 7, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"}
	delegated := connectorapi.ConsoleRestartResult{ClosedSessionIDs: []int64{11, 12}, CanceledRunningRequests: 2}
	delegateError := errors.New("restart delegate failed")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, portKind := range []string{"action", "deletion"} {
		for _, scenario := range []struct {
			name     string
			kind     string
			id       int64
			actor    connectorapi.Principal
			ctx      context.Context
			missing  bool
			noScope  bool
			delegate error
			want     error
			calls    int
		}{
			{name: "valid", kind: "fixture", id: surface.ID, actor: principal, ctx: t.Context(), calls: 1},
			{name: "unavailable first", kind: "foreign", id: -1, ctx: t.Context(), missing: true, want: ErrRuntimeUnavailable},
			{name: "foreign connector", kind: "foreign", id: surface.ID, actor: principal, ctx: t.Context(), want: connectortargets.ErrRuntimeSurfaceNotFound},
			{name: "missing runtime", kind: "fixture", id: surface.ID + 1000, actor: principal, ctx: t.Context(), want: connectortargets.ErrRuntimeSurfaceNotFound},
			{name: "invalid principal", kind: "fixture", id: surface.ID, ctx: t.Context(), want: executionprincipal.ErrInvalid},
			{name: "missing scope", kind: "fixture", id: surface.ID, actor: principal, ctx: t.Context(), noScope: true, want: connectorruntime.ErrInvalidRuntime},
			{name: "canceled", kind: "fixture", id: surface.ID, actor: principal, ctx: canceled, want: context.Canceled},
			{name: "delegate failure", kind: "fixture", id: surface.ID, actor: principal, ctx: t.Context(), delegate: delegateError, want: delegateError, calls: 1},
		} {
			t.Run(portKind+"/"+scenario.name, func(t *testing.T) {
				workspace := NewWorkspace(restartScopeFixture{database}, database, nil, nil)
				if scenario.noScope {
					workspace = NewWorkspace(nil, database, nil, nil)
				}
				calls := 0
				if !scenario.missing {
					workspace.Actions.Restart = func(ctx context.Context, core executionprincipal.Principal, id int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
						calls++
						if ctx != scenario.ctx || id != scenario.id || runningError != "canceled by fixture" || core.Kind != executionprincipal.KindMCPToken || core.TokenID != principal.TokenID || core.WorkspaceID != principal.WorkspaceID || core.RuntimeInstanceID != principal.RuntimeInstanceID {
							t.Fatal("restart delegation changed authority or arguments")
						}
						return delegated, scenario.delegate
					}
				}
				var restart func(context.Context, connectorapi.Principal, int64, string) (connectorapi.ConsoleRestartResult, error)
				component := NewPorts(PortsDependencies{})
				if portKind == "action" {
					gateway, _ := component.RuntimeActionPorts(workspace, scenario.kind)
					restart = gateway.ConnectorRestartConsoleSession
				} else {
					restart = component.TargetDeletionGateway(workspace, scenario.kind, target.ID).ConnectorRestartConsoleSession
				}
				got, err := restart(scenario.ctx, scenario.actor, scenario.id, "canceled by fixture")
				if !errors.Is(err, scenario.want) || calls != scenario.calls {
					t.Fatalf("restart error=%v calls=%d; want %v/%d", err, calls, scenario.want, scenario.calls)
				}
				want := connectorapi.ConsoleRestartResult{}
				if scenario.calls != 0 {
					want = delegated
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("restart result=%#v, want %#v", got, want)
				}
			})
		}
	}
}
