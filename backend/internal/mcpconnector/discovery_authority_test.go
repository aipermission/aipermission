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

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestNativeDiscoveryGroupsOnlySupportedAuthorizedActions(t *testing.T) {
	fixture := newDiscoveryFixture(t)
	fixture.grant(t, "mutate", connectortargets.ActionPermissionApprovalRequired, nil)
	fixture.grant(t, "unsupported", connectortargets.ActionPermissionAlwaysRun, nil)
	response := fixture.request("targets", "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), discoveryPrivateCanary) {
		t.Fatalf("discovery = %d %s", response.Code, response.Body.String())
	}
	var items []mcpconnector.TargetItem
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	want := []mcpconnector.TargetItem{{
		TargetRef: fixture.ref(), ProjectID: fixture.target.ProjectID, ProjectName: fixture.target.ProjectName,
		ProjectSlug: fixture.target.ProjectSlug, TargetID: fixture.target.ID, TargetName: fixture.target.Name,
		ConnectorKind: "fixture", ProfileID: fixture.profile.ID, ProfileLabel: fixture.profile.Label, ProfileKind: "test",
		Metadata: map[string]any{"endpoint": "public-endpoint", "account": "public-account"},
		Actions:  []mcpconnector.ActionGrant{{Name: "inspect", ExecutionRule: "always_run"}, {Name: "mutate", ExecutionRule: "approval_required"}},
	}}
	if len(items) != 1 || len(items[0].Hints) != 2 {
		t.Fatalf("grouped target/hints = %#v", items)
	}
	items[0].Hints = nil
	if !reflect.DeepEqual(items, want) || fixture.connector.metadataCalls != 1 {
		t.Fatalf("targets = %#v, metadata calls = %d", items, fixture.connector.metadataCalls)
	}
	if !reflect.DeepEqual(fixture.connector.metadataTarget, fixture.targetView()) ||
		!reflect.DeepEqual(fixture.connector.metadataProfile, connectortargets.CredentialProfileView(fixture.profile)) {
		t.Fatal("metadata adapter did not receive the exact public storage projections")
	}
	assertDiscoveryPublicProfile(t, fixture.connector.metadataProfile)
	fixture.scope.MetadataEnabled = func(context.Context) (bool, error) { return false, nil }
	response = fixture.request("targets", "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "metadata") || fixture.connector.metadataCalls != 1 {
		t.Fatalf("disabled metadata = %d %s, calls = %d", response.Code, response.Body.String(), fixture.connector.metadataCalls)
	}
}

func TestNativeDiscoveryHelpAndActionsUseExactPublicContext(t *testing.T) {
	fixture := newDiscoveryFixture(t)
	fixture.grant(t, "mutate", connectortargets.ActionPermissionBlocked, nil)
	response := fixture.request("help", fixture.ref())
	var help connectors.ConnectorHelp
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &help) != nil || help.Title != "Fixture help" ||
		!reflect.DeepEqual(fixture.connector.helpTarget, fixture.targetView()) {
		t.Fatalf("help = %d %s", response.Code, response.Body.String())
	}
	response = fixture.request("actions", fixture.ref())
	var result struct {
		Items []connectors.ActionDefinition `json:"items"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Items) != 1 {
		t.Fatalf("actions = %d %s", response.Code, response.Body.String())
	}
	action := result.Items[0]
	if action.Name != "inspect" || action.RetryPolicy.Class != connectors.RetryReadOnly || action.RetryPolicy.Guidance == "" ||
		action.InputSchema.Fields == nil || action.MaxInputBytes != connectors.DefaultMaxActionInputBytes {
		t.Fatalf("action contract = %#v", action)
	}
	if !reflect.DeepEqual(fixture.connector.actionTarget, fixture.targetView()) ||
		!reflect.DeepEqual(fixture.connector.actionProfile, connectortargets.CredentialProfileView(fixture.profile)) ||
		strings.Contains(response.Body.String(), discoveryPrivateCanary) {
		t.Fatal("action discovery changed or disclosed private context")
	}
	assertDiscoveryPublicProfile(t, fixture.connector.actionProfile)
}

func TestNativeDiscoveryHidesInactiveAuthority(t *testing.T) {
	for _, name := range []string{"blocked", "expired", "project disabled", "target archived", "profile archived", "other profile", "other token"} {
		t.Run(name, func(t *testing.T) {
			fixture := newDiscoveryFixture(t)
			permissions, err := fixture.scope.Permissions(t.Context())
			if err != nil || len(permissions) != 1 || permissions[0].TargetID != fixture.target.ID || permissions[0].ProfileID != fixture.profile.ID {
				t.Fatalf("positive authority control = %#v, %v", permissions, err)
			}
			switch name {
			case "blocked":
				fixture.grant(t, "inspect", connectortargets.ActionPermissionBlocked, nil)
			case "expired":
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
			case "other profile":
				profile, err := fixture.store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
					TargetID: fixture.target.ID, ConnectorKind: "fixture", Kind: "test", Label: "Other profile", Public: map[string]any{},
				})
				if err != nil {
					t.Fatal(err)
				}
				fixture.profile = profile
			case "other token":
				token, err := tokens.NewStore(fixture.database).Create(t.Context(), tokens.CreateRequest{Name: "Other discovery token"})
				if err != nil {
					t.Fatal(err)
				}
				fixture.scope.TokenID = token.ID
			}
			for _, method := range []string{"help", "actions"} {
				response := fixture.request(method, fixture.ref())
				if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), discoveryPrivateCanary) {
					t.Fatalf("hidden %s = %d %s", method, response.Code, response.Body.String())
				}
			}
			if fixture.connector.helpTarget.ID != 0 {
				t.Fatal("hidden target reached help callback")
			}
			if name != "other profile" {
				response := fixture.request("targets", "")
				if response.Code != http.StatusOK || response.Body.String() != "[]\n" || fixture.connector.metadataCalls != 0 {
					t.Fatalf("hidden list = %d %s", response.Code, response.Body.String())
				}
			}
		})
	}
}

func TestNativeActionDiscoveryRechecksPermissionAfterCatalogRead(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoked", true: "storage failure"}[failure], func(t *testing.T) {
			fixture := newDiscoveryFixture(t)
			original := fixture.scope.Permissions
			calls := 0
			fixture.scope.Permissions = func(ctx context.Context) ([]mcpconnector.Permission, error) {
				calls++
				if calls == 2 {
					if failure {
						return nil, errors.New(discoveryPrivateCanary)
					}
					fixture.grant(t, "inspect", connectortargets.ActionPermissionBlocked, nil)
				}
				return original(ctx)
			}
			response := fixture.request("actions", fixture.ref())
			wantStatus, wantBody := http.StatusOK, "{\"items\":[]}\n"
			if failure {
				wantStatus, wantBody = http.StatusInternalServerError, "{\"error\":\"internal server error\"}\n"
			}
			if calls != 2 || response.Code != wantStatus || response.Body.String() != wantBody || fixture.connector.actionTarget.ID != fixture.target.ID {
				t.Fatalf("recheck calls = %d, discovery = %d %s", calls, response.Code, response.Body.String())
			}
		})
	}
}

func TestDiscoveryFailuresDoNotDisclosePrivateDiagnostics(t *testing.T) {
	fixture := newDiscoveryFixture(t)
	for _, method := range []string{"help", "actions"} {
		if response := fixture.request(method, ""); response.Code != http.StatusBadRequest {
			t.Fatalf("missing ref %s = %d", method, response.Code)
		}
		if response := fixture.request(method, "fixture:999:999"); response.Code != http.StatusNotFound {
			t.Fatalf("unknown ref %s = %d", method, response.Code)
		}
	}
	fixture.connector.helpError = errors.New(discoveryPrivateCanary)
	assertDiscoveryInternalError(t, fixture.request("help", fixture.ref()))
	fixture.connector.helpError = nil
	fixture.scope.MetadataEnabled = func(context.Context) (bool, error) { return false, errors.New(discoveryPrivateCanary) }
	assertDiscoveryInternalError(t, fixture.request("targets", ""))
	metadataSettingsReads := 0
	fixture.scope.MetadataEnabled = func(context.Context) (bool, error) {
		metadataSettingsReads++
		return true, nil
	}
	fixture.scope.Permissions = func(context.Context) ([]mcpconnector.Permission, error) {
		return nil, errors.New(discoveryPrivateCanary)
	}
	for _, method := range []string{"targets", "help", "actions"} {
		assertDiscoveryInternalError(t, fixture.request(method, fixture.ref()))
	}
	if metadataSettingsReads != 0 {
		t.Fatalf("failed permissions reached metadata settings: %d", metadataSettingsReads)
	}
}

func TestDiscoveryResolvesStorageAndCatalogAfterVisibilityCheck(t *testing.T) {
	for _, name := range []string{"archived storage", "missing connector", "catalog failure"} {
		t.Run(name, func(t *testing.T) {
			fixture := newDiscoveryFixture(t)
			permissions, err := fixture.scope.Permissions(t.Context())
			if err != nil || len(permissions) != 1 {
				t.Fatalf("positive visibility control = %#v, %v", permissions, err)
			}
			fixture.scope.Permissions = func(context.Context) ([]mcpconnector.Permission, error) { return permissions, nil }
			fixture.connector.catalogCalls = 0
			switch name {
			case "archived storage":
				if err := fixture.store.DeleteTarget(t.Context(), fixture.target.ID); err != nil {
					t.Fatal(err)
				}
			case "missing connector":
				fixture.scope.Registry = connectors.NewRegistry().Snapshot()
			case "catalog failure":
				fixture.connector.catalogError = errors.New(discoveryPrivateCanary)
			}
			response := fixture.request("actions", fixture.ref())
			if name == "catalog failure" {
				assertDiscoveryInternalError(t, response)
				if fixture.connector.catalogCalls != 1 {
					t.Fatalf("failed catalog calls = %d", fixture.connector.catalogCalls)
				}
			} else if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), discoveryPrivateCanary) || fixture.connector.catalogCalls != 0 {
				t.Fatalf("storage/catalog discovery = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func assertDiscoveryPublicProfile(t *testing.T, profile connectors.CredentialProfileView) {
	t.Helper()
	encoded, err := json.Marshal(profile)
	if err != nil || strings.Contains(string(encoded), discoveryPrivateCanary) {
		t.Fatalf("private profile projection = %s, %v", encoded, err)
	}
}

func assertDiscoveryInternalError(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	result := response.Result()
	defer result.Body.Close()
	var body map[string]string
	if result.StatusCode != http.StatusInternalServerError || json.NewDecoder(result.Body).Decode(&body) != nil ||
		!reflect.DeepEqual(body, map[string]string{"error": "internal server error"}) {
		t.Fatalf("discovery error = %d %#v", result.StatusCode, body)
	}
}
