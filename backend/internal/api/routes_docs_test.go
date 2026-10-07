package api

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"

	sharedtransport "github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/restcontract"
)

func TestRESTDocsMentionRegisteredRoutes(t *testing.T) {
	routesSource, err := os.ReadFile("httptransport/routes.go")
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
	if err := restcontract.ValidateDocumentedRoutes(routes, docText); err != nil {
		t.Fatal(err)
	}
}

func TestTypedRESTListResponsesConformToPublishedSchemas(t *testing.T) {
	fixture := newAPITestFixture(t)
	paths := []string{
		"/api/targets",
		"/api/connector-targets",
		"/api/connector-action-approvals",
		"/api/history",
		"/api/audit-logs",
		"/api/settings/diagnostics",
		"/api/settings/connector-capacity",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			response := performJSON(fixture.server.Handler(), http.MethodGet, path, "", nil)
			if err := restcontract.ValidateTypedResponse(http.MethodGet, path, response.Code, response.Body.Bytes()); err != nil {
				t.Fatalf("response does not conform: %v\n%s", err, response.Body.String())
			}
		})
	}
}

func TestGeneratedOpenAPIMatchesRegisteredRoutes(t *testing.T) {
	routesSource, err := os.ReadFile("httptransport/routes.go")
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

func TestCoreConnectorRoutesRemainReservedFromAdapters(t *testing.T) {
	routesSource, err := os.ReadFile("httptransport/routes.go")
	if err != nil {
		t.Fatalf("read routes: %v", err)
	}
	routes, err := restcontract.ParseRoutes(routesSource)
	if err != nil {
		t.Fatalf("parse core routes: %v", err)
	}
	for _, route := range routes {
		if !strings.HasPrefix(route.Path, "/api/connectors/{kind}/") {
			continue
		}
		path := strings.Replace(route.Path, "{kind}", "fixture", 1)
		if err := sharedtransport.ValidateConnectorOwnedRoutePath("fixture", path); err == nil {
			t.Errorf("core route %s %s is not reserved from connector adapters", route.Method, route.Path)
		}
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
