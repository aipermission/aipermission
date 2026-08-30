package api

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/restcontract"
)

func TestRESTDocsMentionRegisteredRoutes(t *testing.T) {
	routesSource, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes: %v", err)
	}
	docs, err := os.ReadFile("../../../docs/api/rest-api.md")
	if err != nil {
		t.Fatalf("read rest docs: %v", err)
	}
	docText := string(docs)
	routes := registeredRESTContractRoutes(t, routesSource)
	if err := restcontract.ValidateTypedRoutes(routes); err != nil {
		t.Fatalf("validate typed REST routes: %v", err)
	}
	for _, route := range routes {
		if !strings.Contains(docText, route.Path) {
			t.Fatalf("REST docs do not mention registered route %s %s", route.Method, route.Path)
		}
	}
}

func TestTypedRESTListResponsesConformToPublishedSchemas(t *testing.T) {
	fixture := newAPITestFixture(t)
	tests := []struct {
		path         string
		contractPath string
	}{
		{path: "/api/targets", contractPath: "/api/targets"},
		{path: "/api/connector-targets", contractPath: "/api/connector-targets"},
		{path: "/api/connector-action-approvals", contractPath: "/api/connector-action-approvals"},
		{path: "/api/history", contractPath: "/api/history"},
		{path: "/api/audit-logs", contractPath: "/api/audit-logs"},
		{path: "/api/settings/diagnostics", contractPath: "/api/settings/diagnostics"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := performJSON(fixture.server.Handler(), http.MethodGet, test.path, "", nil)
			if err := restcontract.ValidateTypedResponse(http.MethodGet, test.contractPath, response.Code, response.Body.Bytes()); err != nil {
				t.Fatalf("response does not conform: %v\n%s", err, response.Body.String())
			}
		})
	}
}

func TestGeneratedOpenAPIMatchesRegisteredRoutes(t *testing.T) {
	routesSource, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes: %v", err)
	}
	routes := registeredRESTContractRoutes(t, routesSource)
	expected, err := restcontract.GenerateRoutes(routes)
	if err != nil {
		t.Fatalf("generate OpenAPI route inventory: %v", err)
	}
	current, err := os.ReadFile("../../../docs/api/openapi.json")
	if err != nil {
		t.Fatalf("read generated OpenAPI route inventory: %v", err)
	}
	if !bytes.Equal(current, expected) {
		t.Fatal("generated OpenAPI route inventory is stale; run make rest-contract")
	}
}

func registeredRESTContractRoutes(t *testing.T, routesSource []byte) []restcontract.Route {
	t.Helper()
	routes, err := restcontract.ParseRoutes(routesSource)
	if err != nil {
		t.Fatalf("parse registered routes: %v", err)
	}
	catalog := newTestConnectorCatalog(t)
	connectorInfos := catalog.connectors.List()
	kinds := make([]string, 0, len(connectorInfos))
	for _, info := range connectorInfos {
		kinds = append(kinds, info.Kind)
	}
	adapterRoutes, err := catalog.adapters.RouteDefinitions(kinds)
	if err != nil {
		t.Fatalf("load connector adapter routes: %v", err)
	}
	for _, route := range adapterRoutes {
		routes = append(routes, restcontract.Route{Method: route.Method, Path: route.Path})
	}
	routes, err = restcontract.NormalizeRoutes(routes)
	if err != nil {
		t.Fatalf("normalize registered routes: %v", err)
	}
	return routes
}
