package connectorapi

import (
	"net/http"
	"strings"
	"testing"
)

type testRouteAdapter []RouteDefinition

func (a testRouteAdapter) Routes() []RouteDefinition {
	return a
}

func testRouteHandler(GatewayServer, http.ResponseWriter, *http.Request) {}

func TestRegisterRejectsDuplicateAdapter(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("duplicate_test", struct{}{}); err != nil {
		t.Fatalf("register adapter: %v", err)
	}
	if err := registry.Register("duplicate_test", struct{}{}); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate registration error, got %v", err)
	}
}

func TestRouteDefinitionsValidateSortAndRejectDuplicates(t *testing.T) {
	const firstKind = "route_catalog_first_test"
	const secondKind = "route_catalog_second_test"
	registry := NewRegistry()
	if err := registry.Register(firstKind, testRouteAdapter{
		{Method: "post", Path: "/api/z-last", Handler: testRouteHandler},
		{Method: "GET", Path: "/api/a-first", Handler: testRouteHandler},
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(secondKind, testRouteAdapter{
		{Method: "POST", Path: "/api/z-last", Handler: testRouteHandler},
	}); err != nil {
		t.Fatal(err)
	}

	routes, err := registry.RouteDefinitions([]string{firstKind})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Pattern() != "GET /api/a-first" || routes[1].Pattern() != "POST /api/z-last" {
		t.Fatalf("unexpected routes: %+v", routes)
	}
	if _, err := registry.RouteDefinitions([]string{firstKind, secondKind}); err == nil || !strings.Contains(err.Error(), "both register POST /api/z-last") {
		t.Fatalf("expected duplicate route error, got %v", err)
	}
}

func TestRouteDefinitionsRejectInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name  string
		route RouteDefinition
		want  string
	}{
		{name: "method", route: RouteDefinition{Path: "/api/test", Handler: testRouteHandler}, want: "method is required"},
		{name: "path", route: RouteDefinition{Method: "GET", Path: "api/test", Handler: testRouteHandler}, want: "must start with /"},
		{name: "handler", route: RouteDefinition{Method: "GET", Path: "/api/test"}, want: "has no handler"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind := "route_catalog_invalid_" + test.name
			registry := NewRegistry()
			if err := registry.Register(kind, testRouteAdapter{test.route}); err != nil {
				t.Fatal(err)
			}
			if _, err := registry.RouteDefinitions([]string{kind}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}
