package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/projectcapabilities"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func withCurrentAuthorizationRevision(t *testing.T, handler http.Handler, path string, request any) any {
	t.Helper()
	response := performJSON(handler, http.MethodGet, path, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("load authorization revision for %s: %d %s", path, response.Code, response.Body.String())
	}
	var body struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Revision == "" {
		t.Fatalf("decode authorization revision for %s: revision=%q error=%v", path, body.Revision, err)
	}
	switch value := request.(type) {
	case updateConnectorPermissionsRequest:
		value.ExpectedRevision = body.Revision
		return value
	case updateTokenProjectScopesRequest:
		value.ExpectedRevision = body.Revision
		return value
	case updateProjectCapabilitiesRequest:
		value.ExpectedRevision = body.Revision
		return value
	default:
		t.Fatalf("unsupported authorization request type %T", request)
		return nil
	}
}

func TestTokenAuthorizationUpdatesRejectStaleAndMissingRevisions(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	project, err := projects.NewStore(fixture.db).Create(ctx, "Revision Project")
	if err != nil {
		t.Fatal(err)
	}
	target := fixture.createKeyAndServer(t, "revision-host")
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "revision-agent"})
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/tokens/" + strconv.FormatInt(token.ID, 10)
	tests := []struct {
		name string
		path string
		body any
	}{
		{
			name: "project scopes",
			path: base + "/project-scopes",
			body: updateTokenProjectScopesRequest{EnabledProjectIDs: []int64{}},
		},
		{
			name: "project capabilities",
			path: base + "/project-capabilities",
			body: updateProjectCapabilitiesRequest{Capabilities: []projectCapabilityInput{{
				ProjectID: project.ID, CapabilityName: projectcapabilities.VaultMetadataRead,
				ExecutionRule: projectcapabilities.RuleAlwaysRun,
			}}},
		},
		{
			name: "connector permissions",
			path: base + "/connector-permissions",
			body: updateConnectorPermissionsRequest{Permissions: []connectorPermissionInput{{
				TargetID: target.TargetID, ProfileID: target.ProfileID, ActionName: sshconnector.ActionExec,
				ExecutionRule: string(connectortargets.ActionPermissionAlwaysRun),
			}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stale := withCurrentAuthorizationRevision(t, fixture.server.Handler(), test.path, test.body)
			missing := performJSON(fixture.server.Handler(), http.MethodPut, test.path, "", test.body)
			if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "revision is required") {
				t.Fatalf("missing revision response: %d %s", missing.Code, missing.Body.String())
			}
			current := performJSON(
				fixture.server.Handler(), http.MethodPut, test.path, "",
				withCurrentAuthorizationRevision(t, fixture.server.Handler(), test.path, test.body),
			)
			if current.Code != http.StatusOK || !strings.Contains(current.Body.String(), `"revision"`) {
				t.Fatalf("current revision update: %d %s", current.Code, current.Body.String())
			}
			conflict := performJSON(fixture.server.Handler(), http.MethodPut, test.path, "", stale)
			if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "changed in another client") {
				t.Fatalf("stale revision response: %d %s", conflict.Code, conflict.Body.String())
			}
		})
	}
}

func TestAuthorizationRevisionsIgnorePresentationOrder(t *testing.T) {
	t.Parallel()

	connectorItems := []connectortargets.ActionPermission{
		{TargetID: 2, ProfileID: 3, ActionName: "write"},
		{TargetID: 1, ProfileID: 4, ActionName: "read"},
	}
	connectorReverse := []connectortargets.ActionPermission{connectorItems[1], connectorItems[0]}
	connectorFirst, connectorFirstErr := connectorPermissionsRevision(connectorItems)
	connectorSecond, connectorSecondErr := connectorPermissionsRevision(connectorReverse)
	assertSameAuthorizationRevision(t, connectorFirst, connectorFirstErr, connectorSecond, connectorSecondErr)

	scopeItems := []projects.TokenScope{{ProjectID: 2, Enabled: true}, {ProjectID: 1, Enabled: false}}
	scopeReverse := []projects.TokenScope{scopeItems[1], scopeItems[0]}
	scopeFirst, scopeFirstErr := projectScopesRevision(scopeItems)
	scopeSecond, scopeSecondErr := projectScopesRevision(scopeReverse)
	assertSameAuthorizationRevision(t, scopeFirst, scopeFirstErr, scopeSecond, scopeSecondErr)

	capabilityItems := []projectcapabilities.Capability{
		{ProjectID: 2, Name: "write"},
		{ProjectID: 1, Name: "read"},
	}
	capabilityReverse := []projectcapabilities.Capability{capabilityItems[1], capabilityItems[0]}
	capabilityFirst, capabilityFirstErr := projectCapabilitiesRevision(capabilityItems)
	capabilitySecond, capabilitySecondErr := projectCapabilitiesRevision(capabilityReverse)
	assertSameAuthorizationRevision(t, capabilityFirst, capabilityFirstErr, capabilitySecond, capabilitySecondErr)
}

func assertSameAuthorizationRevision(t *testing.T, first string, firstErr error, second string, secondErr error) {
	t.Helper()
	if firstErr != nil || secondErr != nil {
		t.Fatalf("revision errors: first=%v second=%v", firstErr, secondErr)
	}
	if first != second {
		t.Fatalf("revision changed with presentation order: first=%q second=%q", first, second)
	}
}
