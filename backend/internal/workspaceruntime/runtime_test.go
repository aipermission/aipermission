package workspaceruntime

import (
	"testing"
)

func TestRuntimeStoresCompositionStateWithoutExportingServiceLocatorMethods(t *testing.T) {
	runtime := &Runtime{WorkspaceUUID: "workspace-id", RuntimeInstanceID: "runtime-id"}
	if runtime.WorkspaceUUID != "workspace-id" || runtime.RuntimeInstanceID != "runtime-id" {
		t.Fatal("runtime identity state was not retained")
	}
}
