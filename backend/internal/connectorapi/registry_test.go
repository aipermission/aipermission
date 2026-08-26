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
	const kind = "duplicate_test"
	Register(kind, struct{}{})

	defer func() {
		mu.Lock()
		delete(adapters, kind)
		mu.Unlock()
		if recovered := recover(); recovered == nil {
			t.Fatal("expected duplicate adapter registration panic")
		}
	}()

	Register(kind, struct{}{})
}

func TestRouteDefinitionsValidateSortAndRejectDuplicates(t *testing.T) {
	const firstKind = "route_catalog_first_test"
	const secondKind = "route_catalog_second_test"
	Register(firstKind, testRouteAdapter{
		{Method: "post", Path: "/api/z-last", Handler: testRouteHandler},
		{Method: "GET", Path: "/api/a-first", Handler: testRouteHandler},
	})
	Register(secondKind, testRouteAdapter{
		{Method: "POST", Path: "/api/z-last", Handler: testRouteHandler},
	})
	t.Cleanup(func() {
		mu.Lock()
		delete(adapters, firstKind)
		delete(adapters, secondKind)
		mu.Unlock()
	})

	routes, err := RouteDefinitions([]string{firstKind})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Pattern() != "GET /api/a-first" || routes[1].Pattern() != "POST /api/z-last" {
		t.Fatalf("unexpected routes: %+v", routes)
	}
	if _, err := RouteDefinitions([]string{firstKind, secondKind}); err == nil || !strings.Contains(err.Error(), "both register POST /api/z-last") {
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
			Register(kind, testRouteAdapter{test.route})
			t.Cleanup(func() {
				mu.Lock()
				delete(adapters, kind)
				mu.Unlock()
			})
			if _, err := RouteDefinitions([]string{kind}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}
