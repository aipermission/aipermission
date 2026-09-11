package gatewayworkspace

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func TestComposeRuntimePublishesPortsWithoutExposingConcreteOwner(t *testing.T) {
	owner := &workspaceruntime.Runtime{
		ID: "database-one", Path: "/data/database-one.aipdb",
		WorkspaceUUID: "workspace-one", RuntimeInstanceID: "runtime-one",
		UIRetryIdentity: "retry-one", GatewaySecret: "gateway-secret",
		ActionIdentityKey: []byte("action-identity"),
	}

	runtime, err := composeRuntime(owner)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Identity.DatabaseID != owner.ID || runtime.Identity.RuntimeID != owner.RuntimeInstanceID {
		t.Fatalf("runtime identity = %#v", runtime.Identity)
	}
	if runtime.Storage == nil || runtime.Connectors == nil || runtime.Security == nil || runtime.Observation == nil {
		t.Fatal("runtime composition omitted a feature port")
	}
}

func TestCloseClearsCompositionAndOwnerActionIdentity(t *testing.T) {
	owner := &workspaceruntime.Runtime{ID: "database-one", ActionIdentityKey: []byte("action-identity")}
	runtime := &Runtime{owner: owner}

	if err := (&Component{}).Close(runtime, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if owner.ActionIdentityKey != nil {
		t.Fatal("workspace close retained action identity material")
	}
}
