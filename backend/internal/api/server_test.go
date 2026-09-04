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

	server, err := NewServer(config.Config{DataPath: t.TempDir() + "/test.db"}, database, secretVault, tokens.NewStore(database))
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
	failingGenerator := withRuntimeInstanceIDGenerator(func() (string, error) { return "", wantErr })

	server, err := NewServer(
		config.Config{DataPath: t.TempDir() + "/test.db", GatewaySecret: "test-password"},
		database,
		secretVault,
		tokens.NewStore(database),
		failingGenerator,
	)
	if !errors.Is(err, wantErr) || server != nil {
		t.Fatalf("runtime identity failure should prevent construction: server=%v err=%v", server, err)
	}
}
