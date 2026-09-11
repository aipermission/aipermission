package api

import (
	"testing"

	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
)

func requireTransferJobs(t testing.TB, server *Server, runtime databaseRuntime) transferapp.Jobs {
	t.Helper()
	if runtime == nil {
		t.Fatal("file transfer test workspace is unavailable")
	}
	if server == nil || server.transfers == nil {
		t.Fatal("file transfer test component is unavailable")
	}
	jobs, err := server.transfers.WorkspaceJobs(runtime)
	if err != nil {
		t.Fatalf("file transfer test jobs: %v", err)
	}
	return jobs
}
