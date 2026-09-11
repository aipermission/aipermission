package workspaceruntime

import (
	"testing"
)

func TestIdentityReadyRequiresBothRuntimeIdentifiers(t *testing.T) {
	for name, runtime := range map[string]*Runtime{
		"nil":               nil,
		"empty":             {},
		"workspace missing": {RuntimeInstanceID: "runtime-id"},
		"runtime missing":   {WorkspaceUUID: "workspace-id"},
	} {
		t.Run(name, func(t *testing.T) {
			if runtime.IdentityReady() {
				t.Fatal("incomplete identity reported ready")
			}
		})
	}
	if !(&Runtime{WorkspaceUUID: "workspace-id", RuntimeInstanceID: "runtime-id"}).IdentityReady() {
		t.Fatal("complete identity reported unavailable")
	}
}
