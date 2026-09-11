package api

import (
	"testing"

	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

func requireTransferJobs(t testing.TB, runtime databaseRuntime) transferapp.Jobs {
	t.Helper()
	if runtime == nil {
		t.Fatal("file transfer test workspace is unavailable")
	}
	jobs, err := transferapp.WorkspaceJobs(runtime)
	if err != nil {
		t.Fatalf("file transfer test jobs: %v", err)
	}
	return jobs
}
