package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/config"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestNewServerReturnsWorkspaceIdentityError(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(t.TempDir()+"/test.db", "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	secretVault, err := vault.New("test-secret")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}

	server, err := NewServer(config.Config{DataPath: t.TempDir() + "/test.db"}, testAdoptInput(database, secretVault, tokens.NewStore(database)))
	if err == nil || server != nil {
		t.Fatalf("closed database should prevent server construction: server=%v err=%v", server, err)
	}
	if !strings.Contains(err.Error(), "initialize workspace identity") {
		t.Fatalf("unexpected constructor error: %v", err)
	}
}

func TestNewServerReturnsRuntimeIdentityError(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(t.TempDir()+"/test.db", "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secretVault, err := vault.New("test-secret")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	wantErr := errors.New("random source unavailable")
	adopted := testAdoptInput(database, secretVault, tokens.NewStore(database))
	adopted.RuntimeInstanceID = func() (string, error) { return "", wantErr }

	server, err := NewServer(
		config.Config{DataPath: t.TempDir() + "/test.db", GatewaySecret: "test-password"},
		adopted,
	)
	if !errors.Is(err, wantErr) || server != nil {
		t.Fatalf("runtime identity failure should prevent construction: server=%v err=%v", server, err)
	}
}

func TestNewServerBootstrapsTheAdoptedWorkspaceRuntime(t *testing.T) {
	path := t.TempDir() + "/test.db"
	database, err := dbpkg.OpenEncrypted(path, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at)
		VALUES ('mcp_start_enabled', 'true', datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`); err != nil {
		t.Fatal(err)
	}
	secretVault, err := vault.New("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(
		config.Config{DataPath: path, GatewaySecret: "test-gateway-secret"},
		testAdoptInput(database, secretVault, tokens.NewStore(database)),
	)
	if err != nil {
		t.Fatal(err)
	}
	runtime := server.activeRuntime()
	if runtime == nil || !testRuntimeControlState(t, server, runtime).MCPStarted() {
		t.Fatal("adopted workspace did not apply its MCP startup setting")
	}
	if testRuntimeConsoleSessions(t, server, runtime) == nil {
		t.Fatal("adopted workspace did not initialize its console manager")
	}
	if _, err := server.commandRuntime(runtime); err != nil {
		t.Fatalf("adopted workspace command runtime: %v", err)
	}
	if err := server.closeRuntime(runtime); err != nil {
		t.Fatalf("close adopted workspace: %v", err)
	}
}
