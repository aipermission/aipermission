package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/projectcapabilities"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestTokenProjectCapabilityRoutes(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "vault-agent"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.NewStore(fixture.db).Create(ctx, "My Project")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/tokens/" + strconv.FormatInt(token.ID, 10) + "/project-capabilities"
	response := performJSON(fixture.server.Handler(), http.MethodPut, path, "", updateProjectCapabilitiesRequest{
		Capabilities: []projectCapabilityInput{{
			ProjectID: project.ID, CapabilityName: projectcapabilities.VaultMetadataRead,
			ExecutionRule: projectcapabilities.RuleAlwaysRun,
		}},
	})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), projectcapabilities.VaultMetadataRead) {
		t.Fatalf("update project capabilities: %d %s", response.Code, response.Body.String())
	}
	response = performJSON(fixture.server.Handler(), http.MethodGet, path, "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"allowed_rules"`) {
		t.Fatalf("list project capabilities: %d %s", response.Code, response.Body.String())
	}
	var updatedAudits int
	if err := fixture.db.QueryRow(`
		SELECT COUNT(*) FROM audit_logs
		WHERE action = 'token.project_capabilities.updated'`).Scan(&updatedAudits); err != nil {
		t.Fatal(err)
	}
	if updatedAudits != 1 {
		t.Fatalf("updated audit count = %d", updatedAudits)
	}
}

func TestTokenProjectCapabilityRouteAcceptsAlwaysApply(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "vault-agent"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.NewStore(fixture.db).Create(ctx, "My Project")
	if err != nil {
		t.Fatal(err)
	}
	response := performJSON(fixture.server.Handler(), http.MethodPut,
		"/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/project-capabilities", "",
		updateProjectCapabilitiesRequest{Capabilities: []projectCapabilityInput{{
			ProjectID: project.ID, CapabilityName: projectcapabilities.VaultSessionApply,
			ExecutionRule: projectcapabilities.RuleAlwaysRun,
		}}},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected always apply to succeed, got %d %s", response.Code, response.Body.String())
	}
}
