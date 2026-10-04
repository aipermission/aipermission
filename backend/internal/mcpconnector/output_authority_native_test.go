package mcpconnector_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type nativeDeliveryGate struct {
	coordinator *vaultsessions.DeliveryCoordinator
}

func (gate nativeDeliveryGate) Acquire(ctx context.Context) (func(), error) {
	return gate.coordinator.AcquireDelivery(ctx)
}

func TestNativeOutputDeliveryWithholdsForeignOrMissingRequestOwner(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign token", true: "missing token"}[missing], func(t *testing.T) {
			fixture := newDiscoveryFixture(t)
			authorization, request, response := nativeOutputFixture(t, fixture)
			if !authorization.Authorized(t.Context(), fixture.scope.TokenID, request) {
				t.Fatal("positive output authority control failed")
			}
			if missing {
				request.TokenID = nil
			} else {
				other, err := tokens.NewStore(fixture.database).Create(t.Context(), tokens.CreateRequest{Name: "Foreign output owner"})
				if err != nil {
					t.Fatal(err)
				}
				request.TokenID = &other.ID
			}
			if authorization.Authorized(t.Context(), fixture.scope.TokenID, request) {
				t.Fatal("foreign/missing request owner authorized")
			}
			assertNativeOutputWithheld(t, authorization, fixture.scope.TokenID, request, response)
		})
	}
}

func nativeOutputFixture(t *testing.T, fixture *discoveryFixture) (*mcpconnector.OutputAuthorization, connectortargets.ActionRequest, actions.Response) {
	t.Helper()
	principal, err := executionprincipal.MCPToken(fixture.scope.TokenID, "output-workspace", "output-runtime")
	if err != nil {
		t.Fatal(err)
	}
	authorization := &mcpconnector.OutputAuthorization{
		Database: fixture.database, Tokens: tokens.NewStore(fixture.database), Leases: vaultsessions.NewStore(),
		Delivery:   nativeDeliveryGate{coordinator: &vaultsessions.DeliveryCoordinator{}},
		MCPStarted: func() bool { return true },
		Principal:  func(int64) (executionprincipal.Principal, error) { return principal, nil },
	}
	request := connectortargets.ActionRequest{
		ID: 17, TokenID: &fixture.scope.TokenID, TargetID: fixture.target.ID, ProfileID: fixture.profile.ID,
		ConnectorKind: "fixture", TargetName: fixture.target.Name, ProfileLabel: fixture.profile.Label,
		ActionName: "inspect", Status: connectors.ResultOutcomeUnknown,
		Input: map[string]any{"value": "private-delivery-canary"}, Output: map[string]any{"value": "private-delivery-canary"},
		DisplayText: "private-delivery-canary", Error: "private-delivery-canary",
		RetryPolicy: connectors.EffectiveRetryPolicy(connectors.ActionDefinition{Risk: connectors.RiskRead}),
	}
	response := actions.FromRequest(request, "")
	if response.OutputWithheld || response.Input["value"] != "private-delivery-canary" ||
		!reflect.DeepEqual(response.Output, map[string]any{"value": "private-delivery-canary"}) ||
		response.DisplayText != "private-delivery-canary" || response.Error != "private-delivery-canary" ||
		response.TargetRef != fixture.ref() || response.Status != "outcome_unknown" || response.AssistantHint == "" {
		t.Fatalf("positive response content control = %#v", response)
	}
	return authorization, request, response
}

func assertNativeOutputWithheld(t *testing.T, authorization *mcpconnector.OutputAuthorization, tokenID int64, request connectortargets.ActionRequest, content actions.Response) {
	t.Helper()
	response := httptest.NewRecorder()
	authorization.Deliver(response, httptest.NewRequest(http.MethodGet, "/", nil), tokenID, request, content)
	var result actions.Response
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || !result.OutputWithheld ||
		result.Input != nil || result.Output != nil || result.DisplayText != "" || result.Error != "" ||
		result.TargetRef != "" || result.TargetName != "" || result.ConnectorKind != "" || result.ProfileLabel != "" ||
		strings.Contains(response.Body.String(), "private-delivery-canary") {
		t.Fatalf("withheld output = %d %s", response.Code, response.Body.String())
	}
	if result.Status != content.Status || result.RequestID != content.RequestID || result.ActionName != content.ActionName ||
		!reflect.DeepEqual(result.RetryPolicy, content.RetryPolicy) || !strings.Contains(result.AssistantHint, content.AssistantHint) ||
		response.Header().Get("Cache-Control") != "no-store, private" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("withheld safety/cache contract = %#v, headers = %#v", result, response.Header())
	}
}

func TestNativeOutputDeliveryRechecksChangedAuthority(t *testing.T) {
	for _, cause := range []string{"token revoked", "token expired", "action blocked", "action expired", "project disabled", "target archived", "profile archived", "MCP stopped", "partial generation", "partial session id"} {
		t.Run(cause, func(t *testing.T) {
			fixture := newDiscoveryFixture(t)
			authorization, request, content := nativeOutputFixture(t, fixture)
			response := httptest.NewRecorder()
			authorization.Deliver(response, httptest.NewRequest(http.MethodGet, "/", nil), fixture.scope.TokenID, request, content)
			var positive actions.Response
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &positive) != nil || !reflect.DeepEqual(positive, content) {
				t.Fatalf("positive delivery control = %d %s", response.Code, response.Body.String())
			}
			switch cause {
			case "token revoked":
				if _, err := authorization.Tokens.Revoke(t.Context(), fixture.scope.TokenID); err != nil {
					t.Fatal(err)
				}
			case "token expired":
				if _, err := fixture.database.ExecContext(t.Context(), `UPDATE api_tokens SET expires_at = ? WHERE id = ?`,
					time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), fixture.scope.TokenID); err != nil {
					t.Fatal(err)
				}
			case "action blocked":
				fixture.grant(t, "inspect", connectortargets.ActionPermissionBlocked, nil)
			case "action expired":
				expired := time.Now().UTC().Add(-time.Hour)
				fixture.grant(t, "inspect", connectortargets.ActionPermissionAlwaysRun, &expired)
			case "project disabled":
				if _, err := projects.NewStore(fixture.database).ReplaceTokenScopes(t.Context(), fixture.scope.TokenID, nil); err != nil {
					t.Fatal(err)
				}
			case "target archived":
				if err := fixture.store.DeleteTarget(t.Context(), fixture.target.ID); err != nil {
					t.Fatal(err)
				}
			case "profile archived":
				if err := fixture.store.DeleteCredentialProfile(t.Context(), fixture.target.ID, fixture.profile.ID); err != nil {
					t.Fatal(err)
				}
			case "MCP stopped":
				authorization.MCPStarted = func() bool { return false }
			case "partial generation":
				generation := int64(1)
				request.SessionGeneration = &generation
			case "partial session id":
				sessionID := int64(1)
				request.SessionID = &sessionID
			}
			if authorization.Authorized(t.Context(), fixture.scope.TokenID, request) {
				t.Fatal("changed authority still authorizes output")
			}
			assertNativeOutputWithheld(t, authorization, fixture.scope.TokenID, request, content)
		})
	}
}

type fencedOutputRecorder struct {
	*httptest.ResponseRecorder
	t           *testing.T
	coordinator *vaultsessions.DeliveryCoordinator
	checks      int
}

func (response *fencedOutputRecorder) requireDeliveryFence() {
	response.t.Helper()
	ctx, cancel := context.WithTimeout(response.t.Context(), 50*time.Millisecond)
	defer cancel()
	release, err := response.coordinator.AcquireExclusive(ctx)
	if release != nil {
		release()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		response.t.Errorf("response write was not protected from exclusive mutation: %v", err)
	}
	response.checks++
}

func (response *fencedOutputRecorder) WriteHeader(status int) {
	response.requireDeliveryFence()
	response.ResponseRecorder.WriteHeader(status)
}

func (response *fencedOutputRecorder) Write(value []byte) (int, error) {
	response.requireDeliveryFence()
	return response.ResponseRecorder.Write(value)
}

func TestNativeOutputDeliveryHoldsFenceThroughHeaderAndBody(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "authorized", true: "withheld"}[revoked], func(t *testing.T) {
			fixture := newDiscoveryFixture(t)
			authorization, request, content := nativeOutputFixture(t, fixture)
			coordinator := &vaultsessions.DeliveryCoordinator{}
			authorization.Delivery = nativeDeliveryGate{coordinator: coordinator}
			if revoked {
				fixture.grant(t, "inspect", connectortargets.ActionPermissionBlocked, nil)
			}
			response := &fencedOutputRecorder{ResponseRecorder: httptest.NewRecorder(), t: t, coordinator: coordinator}
			authorization.Deliver(response, httptest.NewRequest(http.MethodGet, "/", nil), fixture.scope.TokenID, request, content)
			if response.Code != http.StatusOK || response.checks != 2 {
				t.Fatalf("delivery checks=%d, status=%d", response.checks, response.Code)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			release, err := coordinator.AcquireExclusive(ctx)
			if err != nil {
				t.Fatalf("delivery fence not released after response: %v", err)
			}
			release()
		})
	}
}
