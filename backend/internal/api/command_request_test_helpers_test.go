package api

import (
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"testing"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func requireCommandRuntime(t *testing.T, server *Server, runtime *gatewayinfra.WorkspaceHandle) *gatewayoperations.CommandRuntime {
	t.Helper()
	owner, err := server.commandRuntime(runtime)
	if err != nil {
		t.Fatalf("resolve command runtime: %v", err)
	}
	return owner
}
