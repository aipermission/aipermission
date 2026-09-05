package connectorapi

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type testRouteAdapter []RouteDefinition

func (a testRouteAdapter) Routes() []RouteDefinition {
	return a
}

func testRouteHandler(RouteGateway, http.ResponseWriter, *http.Request) {}

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
		{name: "transfer batch gateway", value: (*TransferBatchGateway)(nil), methods: []string{"ConnectorCreateDownloadBatch", "ConnectorRunTransferBatch"}},
		{name: "runtime capability gateway", value: (*RuntimeCapabilityGateway)(nil), methods: []string{"ConnectorRuntimeCapabilities"}},
		{name: "route gateway", value: (*RouteGateway)(nil), methods: []string{"ConnectorActiveRuntimeAvailable", "ConnectorChangeVaultPeerTrust", "ConnectorTrustStorePath"}},
		{name: "runtime action gateway", value: (*RuntimeActionGateway)(nil), methods: []string{"ConnectorCreateDownloadBatch", "ConnectorRestartConsoleSession", "ConnectorRunTransferBatch", "ConnectorTrustStorePath"}},
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
