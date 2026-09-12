package api

import (
	"testing"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

func requireTransferJobs(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle) gatewaytransfer.Jobs {
	t.Helper()
	if runtime == nil {
		t.Fatal("file transfer test workspace is unavailable")
	}
	if server == nil || server.operationsOwner == nil {
		t.Fatal("file transfer test component is unavailable")
	}
	jobs, err := server.operationsOwner.FileTransferJobs(runtime)
	if err != nil {
		t.Fatalf("file transfer test jobs: %v", err)
	}
	return jobs
}
