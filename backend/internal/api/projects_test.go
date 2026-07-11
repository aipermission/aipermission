package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

func TestProjectRoutesAssignAndProtectConnectorTargets(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	createProject := performJSON(handler, http.MethodPost, "/api/projects", "", projectRequest{Name: "My Project"})
	if createProject.Code != http.StatusCreated {
		t.Fatalf("create project failed: %d %s", createProject.Code, createProject.Body.String())
	}
	project := decodeRouteResponse[projectstore.Project](t, createProject.Body.Bytes())
	if project.ID < 1 || project.Slug != "my-project" {
		t.Fatalf("unexpected project: %#v", project)
	}

	createTarget := performJSON(handler, http.MethodPost, "/api/connector-targets", "", createConnectorTargetRequest{
		ProjectID:     project.ID,
		ConnectorKind: "postgres",
		Name:          "project-db",
		Config: map[string]any{
			"connection_mode": "direct",
			"host":            "127.0.0.1",
			"port":            5432,
			"database":        "app",
			"ssl_mode":        "prefer",
		},
	})
	if createTarget.Code != http.StatusCreated {
		t.Fatalf("create project target failed: %d %s", createTarget.Code, createTarget.Body.String())
	}
	target := decodeRouteResponse[connectorTargetResponse](t, createTarget.Body.Bytes())
	if target.ProjectID != project.ID || target.ProjectName != "My Project" {
		t.Fatalf("target project response = %#v", target)
	}
	projectAudit := performJSON(handler, http.MethodGet, "/api/audit-logs?project_id="+strconv.FormatInt(project.ID, 10), "", nil)
	if projectAudit.Code != http.StatusOK || !strings.Contains(projectAudit.Body.String(), "connector.target.created") || !strings.Contains(projectAudit.Body.String(), `"project_name":"My Project"`) {
		t.Fatalf("project audit filter failed: %d %s", projectAudit.Code, projectAudit.Body.String())
	}

	archivePopulated := performJSON(handler, http.MethodDelete, "/api/projects/"+strconv.FormatInt(project.ID, 10), "", nil)
	if archivePopulated.Code != http.StatusConflict {
		t.Fatalf("archive populated project = %d %s", archivePopulated.Code, archivePopulated.Body.String())
	}

	projectsResponse := performJSON(handler, http.MethodGet, "/api/projects", "", nil)
	if projectsResponse.Code != http.StatusOK {
		t.Fatalf("list projects failed: %d %s", projectsResponse.Code, projectsResponse.Body.String())
	}
	list := decodeRouteResponse[struct {
		Items []projectstore.Project `json:"items"`
	}](t, projectsResponse.Body.Bytes())
	if len(list.Items) != 2 {
		t.Fatalf("unexpected projects: %#v", list.Items)
	}

	updateProject := performJSON(handler, http.MethodPut, "/api/projects/"+strconv.FormatInt(project.ID, 10), "", projectRequest{Name: "Renamed Project"})
	if updateProject.Code != http.StatusOK {
		t.Fatalf("rename project failed: %d %s", updateProject.Code, updateProject.Body.String())
	}
	renamed := decodeRouteResponse[projectstore.Project](t, updateProject.Body.Bytes())
	if renamed.Name != "Renamed Project" || renamed.Slug != project.Slug {
		t.Fatalf("unexpected renamed project: %#v", renamed)
	}
}
