package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
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
	case accesscontrol.UpdateConnectorPermissionsRequest:
		value.ExpectedRevision = body.Revision
		return value
	case accesscontrol.UpdateProjectScopesRequest:
		value.ExpectedRevision = body.Revision
		return value
	case accesscontrol.UpdateProjectCapabilitiesRequest:
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
			body: accesscontrol.UpdateProjectScopesRequest{EnabledProjectIDs: []int64{}},
		},
		{
			name: "project capabilities",
			path: base + "/project-capabilities",
			body: accesscontrol.UpdateProjectCapabilitiesRequest{Capabilities: []accesscontrol.ProjectCapabilityInput{{
				ProjectID: project.ID, CapabilityName: accesscontrol.VaultMetadataRead,
				ExecutionRule: accesscontrol.RuleAlwaysRun,
			}}},
		},
		{
			name: "connector permissions",
			path: base + "/connector-permissions",
			body: accesscontrol.UpdateConnectorPermissionsRequest{Permissions: []accesscontrol.ConnectorPermissionInput{{
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
