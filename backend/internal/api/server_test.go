package api

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/config"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestNewServerBootstrapsTheOpenedWorkspaceRuntime(t *testing.T) {
	path := t.TempDir() + "/test.db"
	database, err := dbpkg.OpenEncrypted(path, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at)
		VALUES ('mcp_start_enabled', 'true', datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(
		config.Config{DataPath: path, GatewaySecret: "test-gateway-secret"},
		testOpenWorkspaceInput(database),
	)
	if err != nil {
		t.Fatal(err)
	}
	runtime := server.activeRuntime()
	if runtime == nil || !testRuntimeControlState(t, server, runtime).MCPStarted() {
		t.Fatal("opened workspace did not apply its MCP startup setting")
	}
	if testRuntimeConsoleSessions(t, server, runtime) == nil {
		t.Fatal("opened workspace did not initialize its console manager")
	}
	if _, err := server.commandRuntime(runtime); err != nil {
		t.Fatalf("opened workspace command runtime: %v", err)
	}
	if err := server.closeRuntime(runtime); err != nil {
		t.Fatalf("close opened workspace: %v", err)
	}
}
