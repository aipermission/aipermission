package gatewayworkspace

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func available[T any](capability Capability[T]) bool {
	_, ok := capability.Current()
	return ok
}

func projectionAvailability(projection Projection) map[string]func() bool {
	return map[string]func() bool{
		"vault metadata":        func() bool { return available(projection.Access.VaultMetadata) },
		"access control":        func() bool { return available(projection.Access.AccessControl) },
		"MCP token source":      func() bool { return available(projection.Access.MCPTokenSource) },
		"MCP read":              func() bool { return available(projection.Access.MCPRead) },
		"MCP action":            func() bool { return available(projection.Access.MCPAction) },
		"MCP runtime":           func() bool { return available(projection.Access.MCPRuntime) },
		"runtime control":       func() bool { return available(projection.Access.RuntimeControl) },
		"console recovery":      func() bool { return available(projection.Access.ConsoleRecovery) },
		"security policy":       func() bool { return available(projection.Access.SecurityPolicy) },
		"runtime configuration": func() bool { return available(projection.Access.RuntimeConfiguration) },
		"console configuration": func() bool { return available(projection.Access.ConsoleConfiguration) },
		"action":                func() bool { return available(projection.ConnectorActions.Action) },
		"approval":              func() bool { return available(projection.ConnectorActions.Approval) },
		"catalog":               func() bool { return available(projection.ConnectorManagement.Catalog) },
		"credential":            func() bool { return available(projection.ConnectorManagement.Credential) },
		"management":            func() bool { return available(projection.ConnectorManagement.Management) },
		"transport":             func() bool { return available(projection.ConnectorPorts.Transport) },
		"observation":           func() bool { return available(projection.Observation.Runtime) },
		"command":               func() bool { return available(projection.Operations.Command) },
		"command bulk":          func() bool { return available(projection.Operations.CommandBulk) },
		"live console":          func() bool { return available(projection.Operations.LiveConsole) },
		"backup":                func() bool { return available(projection.Operations.Backup) },
		"password validation":   func() bool { return available(projection.Operations.PasswordValidation) },
		"transfer":              func() bool { return available(projection.Operations.Transfer) },
		"peer trust":            func() bool { return available(projection.Operations.PeerTrust) },
		"message":               func() bool { return available(projection.Operations.Message) },
		"Vault runtime":         func() bool { return available(projection.Vault.Runtime) },
		"Vault session":         func() bool { return available(projection.Vault.Session) },
		"Vault MCP":             func() bool { return available(projection.Vault.MCP) },
		"Vault approval":        func() bool { return available(projection.Vault.Approval) },
		"project":               func() bool { return available(projection.Vault.Project) },
	}
}

func TestAllWorkspaceProjectionsFailClosedWithoutAnOwner(t *testing.T) {
	for _, runtime := range []*Runtime{nil, {Identity: RuntimeIdentity{DatabaseID: "detached"}}} {
		projection := ProjectCapabilities(runtime)
		for name, check := range projectionAvailability(projection) {
			if check() {
				t.Errorf("%s exposed without a concrete owner", name)
			}
		}
		if projection.ConnectorActions.Tag != nil {
			if tag, err := projection.ConnectorActions.Tag([]byte("request")); tag != "" || err == nil {
				t.Fatal("detached projection signs action identity")
			}
		}
	}
}

func TestAllWorkspaceProjectionsResolveAfterNativeConfiguration(t *testing.T) {
	runtime := openNativeWorkspace(t, nativeWorkspaceInput(t, "configured"))
	projection := ProjectCapabilities(runtime)
	configuration, ok := projection.Access.ConsoleConfiguration.Current()
	if !ok || configuration.Configure == nil {
		t.Fatal("console configuration unavailable")
	}
	configuration.Configure(nil, nil)
	for name, check := range projectionAvailability(projection) {
		if !check() {
			t.Errorf("%s unavailable after configuration", name)
		}
	}
}

func TestUnavailableWorkspaceRuntimeIdentityFailsClosed(t *testing.T) {
	for _, runtime := range []*Runtime{nil, {}} {
		if runtime.WorkspaceIdentity() != (workspacelifecycle.Identity{}) || runtime.WorkspaceDatabase() != nil || runtime.ConfiguredGatewaySecret() != "" {
			t.Fatal("unavailable runtime exposed identity or storage")
		}
		if tag, err := runtime.TagActionIdentity(nil); tag != "" || err == nil {
			t.Fatal("unavailable runtime signs an action")
		}
		if err := runtime.WaitTeardown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, identity := range []RuntimeIdentity{{}, {WorkspaceID: "workspace"}, {RuntimeID: "runtime"}} {
		if identity.Ready() {
			t.Fatal("partial runtime identity is ready")
		}
	}
	for _, owner := range []*workspaceruntime.Runtime{nil, {}, {WorkspaceUUID: "workspace"}, {RuntimeInstanceID: "runtime"}} {
		if runtime, err := composeRuntime(owner); runtime != nil || err == nil {
			t.Fatal("partial concrete identity was composed")
		}
	}
}
