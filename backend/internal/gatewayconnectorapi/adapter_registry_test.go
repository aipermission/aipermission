package gatewayconnectorapi

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type testRouteAdapter []RouteDefinition

type testNilAdapter struct{}

type testCatalog struct {
	kinds    []string
	adapters map[string]Adapter
}

func (catalog testCatalog) For(kind string) Adapter { return catalog.adapters[kind] }
func (catalog testCatalog) Kinds() []string         { return catalog.kinds }
func (catalog testCatalog) RouteDefinitions([]string) ([]RouteDefinition, error) {
	return nil, nil
}

func (a testRouteAdapter) Routes() []RouteDefinition {
	return a
}

func testReadRouteHandler(ReadRouteGateway, http.ResponseWriter, *http.Request)         {}
func testMutationRouteHandler(MutationRouteGateway, http.ResponseWriter, *http.Request) {}

func TestManagementAdaptersCannotOwnHTTPSerialization(t *testing.T) {
	responseWriter := reflect.TypeOf((*http.ResponseWriter)(nil)).Elem()
	httpRequest := reflect.TypeOf((*http.Request)(nil))
	managementResponse := reflect.TypeOf(connectors.ManagementResponse{})
	for name, contract := range map[string]reflect.Type{
		"draft":   reflect.TypeOf((*DraftTester)(nil)).Elem(),
		"profile": reflect.TypeOf((*CredentialProfileTester)(nil)).Elem(),
		"target":  reflect.TypeOf((*TargetOperationRunner)(nil)).Elem(),
	} {
		method := contract.Method(0)
		for index := 0; index < method.Type.NumIn(); index++ {
			input := method.Type.In(index)
			if input == responseWriter || input == httpRequest {
				t.Fatalf("%s management adapter accepts HTTP serialization input %s", name, input)
			}
		}
		if method.Type.NumOut() != 2 || method.Type.Out(0) != managementResponse {
			t.Fatalf("%s management adapter result contract is %s", name, method.Type)
		}
	}
}

func TestRegisterRejectsDuplicateAdapter(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("duplicate_test", struct{}{}); err != nil {
		t.Fatalf("register adapter: %v", err)
	}
	if err := registry.Register("duplicate_test", struct{}{}); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate registration error, got %v", err)
	}
}

func TestAdapterCatalogRejectsTypedNilAdapters(t *testing.T) {
	var adapter *testNilAdapter
	registry := NewRegistry()
	if err := registry.Register("typed_nil", adapter); err == nil || !strings.Contains(err.Error(), "is nil") {
		t.Fatalf("typed-nil registration error = %v", err)
	}

	_, err := SnapshotCatalog(testCatalog{
		kinds:    []string{"typed_nil"},
		adapters: map[string]Adapter{"typed_nil": adapter},
	})
	if err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("typed-nil snapshot error = %v", err)
	}
}

func TestAdapterCatalogSnapshotIsImmutableAndDetachedFromBuilder(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("first", struct{}{}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := SnapshotCatalog(registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, mutable := snapshot.(interface{ Register(string, Adapter) error }); mutable {
		t.Fatal("runtime adapter catalog exposes registration")
	}
	if err := registry.Register("second", struct{}{}); err != nil {
		t.Fatal(err)
	}
	if adapter := snapshot.For("second"); adapter != nil {
		t.Fatalf("builder mutation leaked into immutable snapshot: %T", adapter)
	}
	if got := snapshot.Kinds(); !slices.Equal(got, []string{"first"}) {
		t.Fatalf("snapshot adapter kinds = %v, want [first]", got)
	}
}

func TestRouteDefinitionsValidateSortAndRejectDuplicates(t *testing.T) {
	const firstKind = "route_catalog_first_test"
	const secondKind = "route_catalog_second_test"
	registry := NewRegistry()
	if err := registry.Register(firstKind, testRouteAdapter{
		{Method: "post", Path: "/api/connectors/route_catalog_first_test/z-last", Policy: RoutePolicyUIMutation, MutationHandler: testMutationRouteHandler},
		{Method: "GET", Path: "/api/connectors/route_catalog_first_test/a-first", Policy: RoutePolicyUIRead, ReadHandler: testReadRouteHandler},
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(secondKind, testRouteAdapter{
		{Method: "POST", Path: "/api/connectors/route_catalog_second_test/z-last", Policy: RoutePolicyUIMutation, MutationHandler: testMutationRouteHandler},
	}); err != nil {
		t.Fatal(err)
	}

	routes, err := registry.RouteDefinitions([]string{firstKind})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Pattern() != "GET /api/connectors/route_catalog_first_test/a-first" || routes[1].Pattern() != "POST /api/connectors/route_catalog_first_test/z-last" {
		t.Fatalf("unexpected routes: %+v", routes)
	}
	if routes[0].Kind != firstKind || routes[1].Kind != firstKind {
		t.Fatalf("route owner kind was not preserved: %+v", routes)
	}
	if _, err := registry.RouteDefinitions([]string{firstKind, secondKind}); err != nil {
		t.Fatalf("connector namespaces should keep routes distinct: %v", err)
	}
	duplicate := NewRegistry()
	if err := duplicate.Register("duplicate", testRouteAdapter{
		{Method: "GET", Path: "/api/connectors/duplicate/item", Policy: RoutePolicyUIRead, ReadHandler: testReadRouteHandler},
		{Method: "GET", Path: "/api/connectors/duplicate/item", Policy: RoutePolicyUIRead, ReadHandler: testReadRouteHandler},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := duplicate.RouteDefinitions([]string{"duplicate"}); err == nil || !strings.Contains(err.Error(), "both register GET /api/connectors/duplicate/item") {
		t.Fatalf("expected duplicate route error, got %v", err)
	}
}

func TestRouteDefinitionsRejectInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name  string
		route RouteDefinition
		want  string
	}{
		{name: "method", route: RouteDefinition{Path: "/api/test", Policy: RoutePolicyUIRead, ReadHandler: testReadRouteHandler}, want: "method is required"},
		{name: "path", route: RouteDefinition{Method: "GET", Path: "api/test", Policy: RoutePolicyUIRead, ReadHandler: testReadRouteHandler}, want: "must start with /"},
		{name: "reserved namespace", route: RouteDefinition{Method: "POST", Path: "/api/mcp/connector-fixture", Policy: RoutePolicyUIMutation, MutationHandler: testMutationRouteHandler}, want: "must use namespace"},
		{name: "handler", route: RouteDefinition{Method: "GET", Path: "/api/test", Policy: RoutePolicyUIRead}, want: "has no read handler"},
		{name: "policy", route: RouteDefinition{Method: "GET", Path: "/api/test", ReadHandler: testReadRouteHandler}, want: "route policy is required"},
		{name: "read method", route: RouteDefinition{Method: "POST", Path: "/api/test", Policy: RoutePolicyUIRead, ReadHandler: testReadRouteHandler}, want: "ui_read policy requires GET or HEAD"},
		{name: "mutation method", route: RouteDefinition{Method: "GET", Path: "/api/test", Policy: RoutePolicyUIMutation, MutationHandler: testMutationRouteHandler}, want: "ui_mutation policy requires a state-changing method"},
		{name: "read authority", route: RouteDefinition{Method: "GET", Path: "/api/test", Policy: RoutePolicyUIRead, ReadHandler: testReadRouteHandler, MutationHandler: testMutationRouteHandler}, want: "must not expose a mutation handler"},
		{name: "mutation authority", route: RouteDefinition{Method: "POST", Path: "/api/test", Policy: RoutePolicyUIMutation, MutationHandler: testMutationRouteHandler, ReadHandler: testReadRouteHandler}, want: "must not expose a read handler"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind := "route_catalog_invalid_" + strings.ReplaceAll(test.name, " ", "_")
			route := test.route
			if route.Path == "/api/test" {
				route.Path = "/api/connectors/" + kind + "/test"
			}
			registry := NewRegistry()
			if err := registry.Register(kind, testRouteAdapter{route}); err != nil {
				t.Fatal(err)
			}
			if _, err := registry.RouteDefinitions([]string{kind}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func TestRouteDefinitionsRejectCoreRouteShadowing(t *testing.T) {
	const kind = "route_catalog_shadow_test"
	for _, suffix := range []string{"credentials", "credentials/import", "credentials/123"} {
		registry := NewRegistry()
		route := RouteDefinition{
			Method:      "GET",
			Path:        "/api/connectors/" + kind + "/" + suffix,
			Policy:      RoutePolicyUIRead,
			ReadHandler: testReadRouteHandler,
		}
		if err := registry.Register(kind, testRouteAdapter{route}); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.RouteDefinitions([]string{kind}); err == nil || !strings.Contains(err.Error(), "core-owned route segment") {
			t.Fatalf("shadow route %q error = %v", route.Path, err)
		}
	}
	for _, routePath := range []string{
		"/api/connectors/ssh/%63redentials",
		"/api/connectors/ssh/config/../credentials",
		"/api/connectors/ssh/{resource}",
	} {
		t.Run(routePath, func(t *testing.T) {
			registry := NewRegistry()
			if err := registry.Register("ssh", testRouteAdapter{{
				Method: http.MethodGet, Path: routePath, Policy: RoutePolicyUIRead,
				ReadHandler: testReadRouteHandler,
			}}); err != nil {
				t.Fatal(err)
			}
			if _, err := registry.RouteDefinitions([]string{"ssh"}); err == nil || !strings.Contains(err.Error(), "canonical and static") {
				t.Fatalf("expected non-canonical route rejection, got %v", err)
			}
		})
	}
}

func TestConnectorCapabilityPortsStayLeastPrivilege(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		methods []string
	}{
		{name: "credential resource store", value: (*CredentialResourceStore)(nil), methods: []string{"CountProfileReferences", "Create", "Delete", "Get", "GetSecret", "List", "Update"}},
		{name: "connector data runtime", value: (*ConnectorDataRuntime)(nil), methods: []string{"CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "TargetProfileByRuntimeID"}},
		{name: "live session runtime", value: (*LiveSessionRuntime)(nil), methods: []string{"ConnectorConsoleSessions", "CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "TargetProfileByRuntimeID"}},
		{name: "principal runtime", value: (*PrincipalRuntime)(nil), methods: []string{"ConnectorLocalExecutionPrincipal"}},
		{name: "live console runtime", value: (*LiveConsoleRuntime)(nil), methods: []string{"CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "TargetProfileByRuntimeID"}},
		{name: "action runtime", value: (*ActionRuntime)(nil), methods: []string{"ConnectorConsoleSessions", "CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "TargetProfileByRuntimeID"}},
		{name: "transfer runtime", value: (*TransferRuntime)(nil), methods: []string{"CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "ResolveRuntimeContext", "TargetProfileByRuntimeID"}},
		{name: "target lifecycle runtime", value: (*TargetLifecycleRuntime)(nil), methods: []string{"ConnectorConsoleSessions", "ConnectorLocalExecutionPrincipal", "CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "TargetProfileByRuntimeID"}},
		{name: "credential resource runtime", value: (*CredentialResourceRuntime)(nil), methods: []string{"CredentialResources", "EnsureRuntimeSurface", "ListCredentialProfiles", "ListRuntimeSurfacesForProfile", "ResolveConnectorActionTarget", "TargetProfileByRuntimeID"}},
		{name: "runtime availability gateway", value: (*RuntimeAvailabilityGateway)(nil), methods: []string{"ConnectorActiveRuntimeAvailable"}},
		{name: "peer identity gateway", value: (*PeerIdentityGateway)(nil), methods: []string{"ConnectorTrustStorePath"}},
		{name: "peer trust gateway", value: (*PeerTrustGateway)(nil), methods: []string{"ConnectorChangeVaultPeerTrust", "ConnectorTrustStorePath"}},
		{name: "live console gateway", value: (*LiveConsoleGateway)(nil), methods: []string{"ConnectorOpenLiveConsole", "ConnectorTrustStorePath"}},
		{name: "console restart gateway", value: (*ConsoleRestartGateway)(nil), methods: []string{"ConnectorRestartConsoleSession"}},
		{name: "action finish gateway", value: (*ActionFinishGateway)(nil), methods: []string{"ConnectorFinishActionRequest"}},
		{name: "transfer batch gateway", value: (*TransferBatchGateway)(nil), methods: []string{"ConnectorCreateAndRunDownloadBatch"}},
		{name: "runtime capability gateway", value: (*RuntimeCapabilityGateway)(nil), methods: []string{"ConnectorRuntimeCapabilities"}},
		{name: "read route gateway", value: (*ReadRouteGateway)(nil), methods: []string{"ConnectorActiveRuntimeAvailable", "ConnectorTrustStorePath"}},
		{name: "mutation route gateway", value: (*MutationRouteGateway)(nil), methods: []string{"ConnectorActiveRuntimeAvailable", "ConnectorChangeVaultPeerTrust", "ConnectorTrustStorePath"}},
		{name: "runtime action gateway", value: (*RuntimeActionGateway)(nil), methods: []string{"ConnectorCreateAndRunDownloadBatch", "ConnectorRestartConsoleSession", "ConnectorTrustStorePath"}},
		{name: "file transfer gateway", value: (*FileTransferGateway)(nil), methods: []string{"ConnectorRuntimeCapabilities", "ConnectorTrustStorePath"}},
		{name: "target deletion gateway", value: (*TargetDeletionGateway)(nil), methods: []string{"ConnectorDeleteTargetRecord", "ConnectorFinalizeDeletedTarget", "ConnectorRestartConsoleSession", "ConnectorTrustStorePath"}},
		{name: "target operation gateway", value: (*TargetOperationGateway)(nil), methods: []string{"ConnectorTrustStorePath", "ConnectorWriteAudit"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			interfaceType := reflect.TypeOf(test.value).Elem()
			got := make([]string, 0, interfaceType.NumMethod())
			for index := 0; index < interfaceType.NumMethod(); index++ {
				got = append(got, interfaceType.Method(index).Name)
			}
			slices.Sort(got)
			slices.Sort(test.methods)
			if !slices.Equal(got, test.methods) {
				t.Fatalf("capability methods = %v, want %v", got, test.methods)
			}
		})
	}
}

func TestConnectorRuntimePortsDoNotReturnRawGatewayState(t *testing.T) {
	ports := []any{
		(*ConnectorDataRuntime)(nil), (*LiveSessionRuntime)(nil), (*LiveConsoleRuntime)(nil),
		(*ActionRuntime)(nil), (*TransferRuntime)(nil), (*TargetLifecycleRuntime)(nil),
		(*CredentialResourceRuntime)(nil), (*CredentialResourceStore)(nil),
	}
	for _, port := range ports {
		portType := reflect.TypeOf(port).Elem()
		for methodIndex := 0; methodIndex < portType.NumMethod(); methodIndex++ {
			method := portType.Method(methodIndex)
			for outputIndex := 0; outputIndex < method.Type.NumOut(); outputIndex++ {
				output := method.Type.Out(outputIndex)
				if output.Kind() == reflect.Pointer && output.Elem().PkgPath() == "database/sql" {
					t.Fatalf("%s.%s returns raw database state %s", portType.Name(), method.Name, output)
				}
				if strings.Contains(output.PkgPath(), "/internal/vault") || (output.Kind() == reflect.Pointer && strings.Contains(output.Elem().PkgPath(), "/internal/vault")) {
					t.Fatalf("%s.%s returns raw Vault state %s", portType.Name(), method.Name, output)
				}
				if output.Kind() == reflect.Interface && output.NumMethod() == 0 {
					t.Fatalf("%s.%s returns an untyped escape hatch", portType.Name(), method.Name)
				}
			}
		}
	}
}
