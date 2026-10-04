package httpowner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func TestMCPReadAdapterPreservesScopeAndPermissionIdentity(t *testing.T) {
	if adaptMCPReadScopeProvider(nil) != nil {
		t.Fatal("nil provider became available")
	}
	database := &sql.DB{}
	registry := connectors.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, r := httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)
	sentinel := errors.New("permission backend unavailable")
	want := mcpconnector.Permission{
		ProjectID: 9007199254740993, ProjectName: "Project 名", ProjectSlug: "project",
		TargetID: 9007199254740995, TargetName: "Target λ", ProfileID: 9007199254740997,
		ProfileLabel: "Profile é", ConnectorKind: "fixture", ProfileKind: "credential",
		ActionName: "read", ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
		ExpiresAt: "2026-10-04T00:00:00Z",
	}
	permissionErr := error(nil)
	metadataReads := 0
	provider := adaptMCPReadScopeProvider(func(gotW http.ResponseWriter, gotR *http.Request) (gatewayaccess.MCPScope, bool) {
		if gotW != w || gotR != r {
			t.Fatal("provider received a different HTTP request")
		}
		return gatewayaccess.MCPScope{
			Database: database, Registry: registry, TokenID: 9007199254740999,
			Permissions: func(got context.Context) ([]gatewayaccess.MCPPermission, error) {
				if got != ctx {
					t.Fatal("permission context changed")
				}
				return []gatewayaccess.MCPPermission{{
					ProjectID: want.ProjectID, ProjectName: want.ProjectName, ProjectSlug: want.ProjectSlug,
					TargetID: want.TargetID, TargetName: want.TargetName, ProfileID: want.ProfileID,
					ProfileLabel: want.ProfileLabel, ConnectorKind: want.ConnectorKind, ProfileKind: want.ProfileKind,
					ActionName: want.ActionName, ExecutionRule: gatewayaccess.ActionPermissionRule(want.ExecutionRule),
					ExpiresAt: want.ExpiresAt,
				}}, permissionErr
			},
			MetadataEnabled: func(got context.Context) (bool, error) {
				if got != ctx {
					t.Fatal("metadata context changed")
				}
				metadataReads++
				return false, sentinel
			},
		}, true
	})
	scope, ok := provider(w, r)
	if !ok || scope.Database != database || scope.Registry != registry || scope.TokenID != 9007199254740999 || scope.Metadata != nil || metadataReads != 0 {
		t.Fatal("scope identity or lazy capability availability changed")
	}
	items, err := scope.Permissions(ctx)
	if err != nil || !reflect.DeepEqual(items, []mcpconnector.Permission{want}) {
		t.Fatalf("permission fields changed: %#v %v", items, err)
	}
	permissionErr = sentinel
	if items, err := scope.Permissions(ctx); !errors.Is(err, sentinel) || items != nil {
		t.Fatalf("errored permissions were exposed: %#v %v", items, err)
	}
	if enabled, err := scope.MetadataEnabled(ctx); enabled || !errors.Is(err, sentinel) || metadataReads != 1 {
		t.Fatal("metadata policy result changed")
	}
	for _, admitted := range []bool{false, true} {
		missing, ok := adaptMCPReadScopeProvider(func(http.ResponseWriter, *http.Request) (gatewayaccess.MCPScope, bool) {
			return gatewayaccess.MCPScope{}, admitted
		})(w, r)
		if ok != admitted || missing.Permissions != nil || missing.Metadata != nil || missing.MetadataEnabled != nil {
			t.Fatal("empty scope manufactured capabilities or changed admission")
		}
	}
}

func TestMCPReadAdapterKeepsMetadataResolutionLazyAndConnectorOwned(t *testing.T) {
	target := connectors.TargetView{ID: 23, ConnectorKind: "fixture", Name: "Target"}
	profile := connectors.CredentialProfileView{ID: 29, Label: "Profile"}
	reads := 0
	resolver := gatewayaccess.NewMCPMetadataResolver(func(kind string) gatewayaccess.MCPMetadataAdapter {
		if kind != "fixture" {
			t.Fatalf("kind = %q", kind)
		}
		return metadataFixture{resolve: func(gotTarget connectors.TargetView, gotProfile connectors.CredentialProfileView) map[string]any {
			reads++
			if !reflect.DeepEqual(gotTarget, target) || !reflect.DeepEqual(gotProfile, profile) {
				t.Fatal("metadata identity changed")
			}
			return map[string]any{"public": "fixture"}
		}}
	})
	scope, _ := adaptMCPReadScopeProvider(func(http.ResponseWriter, *http.Request) (gatewayaccess.MCPScope, bool) {
		return gatewayaccess.MCPScope{Metadata: resolver}, true
	})(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if scope.Metadata == nil || reads != 0 {
		t.Fatal("metadata resolver was lost or called during adaptation")
	}
	if got := scope.Metadata(target, profile); !reflect.DeepEqual(got, map[string]any{"public": "fixture"}) || reads != 1 {
		t.Fatalf("metadata changed: %#v, reads=%d", got, reads)
	}
}

type metadataFixture struct {
	resolve func(connectors.TargetView, connectors.CredentialProfileView) map[string]any
}

func (fixture metadataFixture) LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	return fixture.resolve(target, profile)
}

func TestMCPActionAdapterPreservesCallOutcomeAndCapabilityErrors(t *testing.T) {
	if adaptMCPActionScopeProvider(nil) != nil {
		t.Fatal("nil action provider became available")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sentinel := errors.New("policy unavailable")
	request := connectortargets.ActionRequest{ID: 9007199254740993, ActionName: "write"}
	result := connectors.ActionResult{Status: connectors.ResultOutcomeUnknown, Output: map[string]any{"state": "uncertain"}}
	callErr := error(nil)
	policyCalls, callCalls := 0, 0
	scope, ok := adaptMCPActionScopeProvider(func(http.ResponseWriter, *http.Request) (gatewayaccess.MCPActionScope, bool) {
		return gatewayaccess.MCPActionScope{
			RuntimeID: "runtime-λ", TokenID: 9007199254740995,
			ResourcePolicy: func(got context.Context, target, action string) (gatewayaccess.MCPActionResourcePolicy, error) {
				policyCalls++
				if got != ctx || target != "fixture:23:29" || action != "write" {
					t.Fatal("resource policy identity changed")
				}
				return gatewayaccess.MCPActionResourcePolicy{MaxInputBytes: 1234}, sentinel
			},
			Call: func(got context.Context, call gatewayaccess.MCPActionCall) (gatewayaccess.MCPActionCallResult, error) {
				callCalls++
				want := gatewayaccess.MCPActionCall{
					Source: "mcp", TokenID: 9007199254740995, TargetRef: "fixture:23:29",
					ActionName: "write", Input: map[string]any{"value": "λ", "exact": json.Number("9007199254740993")}, Reason: "fixture reason", IdempotencyKey: "fixture-key",
				}
				if got != ctx || !reflect.DeepEqual(call, want) {
					t.Fatalf("call changed: %#v", call)
				}
				return gatewayaccess.MCPActionCallResult{Request: request, Result: result, Replayed: true}, callErr
			},
			RunningHint: func(got connectortargets.ActionRequest) string {
				if !reflect.DeepEqual(got, request) {
					t.Fatal("running hint request changed")
				}
				return "reconcile the original request"
			},
		}, true
	})(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	if !ok || scope.RuntimeID != "runtime-λ" || scope.TokenID != 9007199254740995 || policyCalls != 0 || callCalls != 0 {
		t.Fatal("scope identity changed or adaptation invoked a capability")
	}
	policy, err := scope.ResourcePolicy(ctx, "fixture:23:29", "write")
	if policy.MaxInputBytes != 1234 || !errors.Is(err, sentinel) || policyCalls != 1 {
		t.Fatal("policy value/error changed")
	}
	call := mcpconnector.ActionCall{
		Source: "mcp", TokenID: scope.TokenID, TargetRef: "fixture:23:29", ActionName: "write",
		Input: map[string]any{"value": "λ", "exact": json.Number("9007199254740993")}, Reason: "fixture reason", IdempotencyKey: "fixture-key",
	}
	for _, failure := range []error{nil, sentinel} {
		callErr = failure
		got, err := scope.Call(ctx, call)
		if err != failure || !reflect.DeepEqual(got, mcpconnector.ActionCallResult{Request: request, Result: result, Replayed: true}) {
			t.Fatalf("action outcome changed: %#v %v", got, err)
		}
	}
	if callCalls != 2 || scope.RunningHint(request) != "reconcile the original request" {
		t.Fatal("action or running hint dispatch count changed")
	}
	missing, admitted := adaptMCPActionScopeProvider(func(http.ResponseWriter, *http.Request) (gatewayaccess.MCPActionScope, bool) {
		return gatewayaccess.MCPActionScope{}, false
	})(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if admitted || missing.Call != nil || missing.ResourcePolicy != nil || missing.Output != nil || missing.RunningHint(request) != "" {
		t.Fatal("empty action scope changed admission or manufactured required capabilities")
	}
}

func TestOutputAdapterPreservesAuthorizationDependencies(t *testing.T) {
	if outputAuthorization(nil) != nil {
		t.Fatal("nil authorization became available")
	}
	database := &sql.DB{}
	clock := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	value := &gatewayaccess.MCPOutputAuthorization{
		Database: database, Tokens: &tokens.Store{}, Leases: &vaultsessions.Store{}, Delivery: unavailableDeliveryGate{},
		MCPStarted: func() bool { return false }, Now: func() time.Time { return clock },
		Principal: func(id int64) (gatewayaccess.Principal, error) {
			if id != 9007199254740993 {
				t.Fatal("principal token changed")
			}
			return gatewayaccess.Principal{}, context.Canceled
		},
	}
	adapted := outputAuthorization(value)
	if adapted.Database != value.Database || adapted.Tokens != value.Tokens || adapted.Leases != value.Leases ||
		adapted.Delivery != value.Delivery || adapted.MCPStarted() || adapted.Now() != clock {
		t.Fatal("output authorization dependencies changed")
	}
	if _, err := adapted.Principal(9007199254740993); !errors.Is(err, context.Canceled) {
		t.Fatal("principal error changed")
	}
	request := connectortargets.ActionRequest{ID: 17, Status: connectors.ResultOutcomeUnknown}
	response := ResponseForToken(context.Background(), nil, nil, 1, request, connectors.ActionResult{
		Status: connectors.ResultOutcomeUnknown, Output: map[string]any{"private": "fixture"}, DisplayText: "private fixture",
	})
	if !response.OutputWithheld || response.Output != nil || response.DisplayText != "" || Authorized(context.Background(), nil, 1, request) {
		t.Fatal("absent authorization exposed output")
	}
	w := httptest.NewRecorder()
	Deliver(w, httptest.NewRequest(http.MethodGet, "/", nil), nil, 1, request, response)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("absent delivery authorization returned %d", w.Code)
	}
}
