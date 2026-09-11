package api

import (
	"testing"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func requireCommandRuntime(t *testing.T, server *Server, runtime databaseRuntime) gatewayaccess.CommandRuntime {
	t.Helper()
	owner, err := server.commandRuntime(runtime)
	if err != nil {
		t.Fatalf("resolve command runtime: %v", err)
	}
	return owner
}
