package gatewayinfrastructure

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

type recordingAccessHTTPFactory struct {
	providers gatewayaccess.ScopeProviders
	calls     int
}

func (factory *recordingAccessHTTPFactory) Build(providers gatewayaccess.ScopeProviders) gatewayaccess.HTTPHandlers {
	factory.calls++
	factory.providers = providers
	return gatewayaccess.HTTPHandlers{}
}

func completeAccessHTTPDependencies() AccessHTTPDependencies {
	return AccessHTTPDependencies{
		Active:      func(http.ResponseWriter) (*WorkspaceHandle, bool) { return nil, false },
		TokenAccess: func(*WorkspaceHandle) AccessControlPorts { return AccessControlPorts{} },
		MCPRuntime:  func(*WorkspaceHandle) MCPRuntimePorts { return MCPRuntimePorts{} },
		MCPRead: func(http.ResponseWriter, *http.Request) (*WorkspaceHandle, MCPReadPorts, bool) {
			return nil, MCPReadPorts{}, false
		},
		MCPAction: func(http.ResponseWriter, *http.Request) (*WorkspaceHandle, MCPActionPorts, bool) {
			return nil, MCPActionPorts{}, false
		},
	}
}

func TestAccessHTTPHandlersOwnScopeProviderConstruction(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	factory := &recordingAccessHTTPFactory{}
	component.AccessOwner().HTTPHandlers(gatewayaccess.NewComponent("3212"), factory, completeAccessHTTPDependencies())

	if factory.calls != 1 {
		t.Fatalf("factory calls = %d, want 1", factory.calls)
	}
	providers := factory.providers
	if providers.Security == nil || providers.TokenAccess == nil || providers.MCPRuntime == nil ||
		providers.MCPConnectorReads == nil || providers.MCPConnectorActions == nil {
		t.Fatal("access owner did not construct the complete scope provider set")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := providers.Security(w); ok {
		t.Fatal("security provider did not fail closed without an active workspace")
	}
	if _, ok := providers.TokenAccess(w); ok {
		t.Fatal("token provider did not fail closed without an active workspace")
	}
	if _, ok := providers.MCPRuntime(w); ok {
		t.Fatal("runtime provider did not fail closed without an active workspace")
	}
	if _, ok := providers.MCPConnectorReads(w, r); ok {
		t.Fatal("MCP read provider did not fail closed without authentication")
	}
	if _, ok := providers.MCPConnectorActions(w, r); ok {
		t.Fatal("MCP action provider did not fail closed without authentication")
	}
}

func TestAccessHTTPHandlersRejectIncompleteComposition(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	application := gatewayaccess.NewComponent("3212")
	complete := completeAccessHTTPDependencies()

	tests := []struct {
		name   string
		owner  *AccessOwner
		app    *gatewayaccess.Component
		mutate func(*AccessHTTPDependencies)
	}{
		{name: "nil owner", owner: nil, app: application},
		{name: "nil application", owner: component.AccessOwner(), app: nil},
		{name: "missing active resolver", owner: component.AccessOwner(), app: application, mutate: func(deps *AccessHTTPDependencies) { deps.Active = nil }},
		{name: "missing token ports", owner: component.AccessOwner(), app: application, mutate: func(deps *AccessHTTPDependencies) { deps.TokenAccess = nil }},
		{name: "missing runtime ports", owner: component.AccessOwner(), app: application, mutate: func(deps *AccessHTTPDependencies) { deps.MCPRuntime = nil }},
		{name: "missing read authentication", owner: component.AccessOwner(), app: application, mutate: func(deps *AccessHTTPDependencies) { deps.MCPRead = nil }},
		{name: "missing action authentication", owner: component.AccessOwner(), app: application, mutate: func(deps *AccessHTTPDependencies) { deps.MCPAction = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := complete
			if test.mutate != nil {
				test.mutate(&dependencies)
			}
			factory := &recordingAccessHTTPFactory{}
			test.owner.HTTPHandlers(test.app, factory, dependencies)
			if factory.calls != 0 {
				t.Fatalf("factory calls = %d, want 0", factory.calls)
			}
		})
	}
}
