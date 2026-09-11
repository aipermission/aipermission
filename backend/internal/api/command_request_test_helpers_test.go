package api

import (
	"testing"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func requireCommandRuntime(t *testing.T, server *Server, runtime databaseRuntime) *gatewayoperations.CommandRuntime {
	t.Helper()
	owner, err := server.commandRuntime(runtime)
	if err != nil {
		t.Fatalf("resolve command runtime: %v", err)
	}
	return owner
}
