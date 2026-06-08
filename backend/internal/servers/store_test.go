package servers

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func openServerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func insertServerTestSSHKey(t *testing.T, database *sql.DB, name string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO ssh_keys (name, key_type, public_key, encrypted_private_key, fingerprint, created_at, updated_at)
		VALUES (?, 'ed25519', 'ssh-ed25519 AAAA test', 'encrypted', 'SHA256:test', ?, ?)`,
		name,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert ssh key: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("ssh key id: %v", err)
	}
	return id
}

func TestServerStoreCreateListUpdateDelete(t *testing.T) {
	ctx := context.Background()
	database := openServerTestDB(t)
	keyID := insertServerTestSSHKey(t, database, "main")
	store := NewStore(database)

	created, err := store.Create(ctx, CreateRequest{
		Name:                     " worker-1 ",
		Host:                     " 10.0.0.10 ",
		Username:                 " root ",
		SSHKeyID:                 keyID,
		Description:              " first server ",
		StartupInputAfterConnect: "q\r\n",
		ForceShellCommand:        " /bin/sh -l ",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if created.Name != "worker-1" || created.Host != "10.0.0.10" || created.Username != "root" || created.Port != 22 {
		t.Fatalf("server was not normalized: %#v", created)
	}
	if created.SSHKeyName != "main" {
		t.Fatalf("expected ssh key join name, got %q", created.SSHKeyName)
	}
	if created.StartupInputAfterConnect != "q\n" || created.ForceShellCommand != "/bin/sh -l" {
		t.Fatalf("server advanced startup settings were not normalized: %#v", created)
	}

	updated, err := store.Update(ctx, created.ID, UpdateRequest{
		Name:                     "worker-1b",
		Host:                     "example.test",
		Port:                     2200,
		Username:                 "ubuntu",
		SSHKeyID:                 keyID,
		Description:              "updated",
		StartupInputAfterConnect: "menu\nexit\n",
		ForceShellCommand:        "/usr/bin/env sh",
	})
	if err != nil {
		t.Fatalf("update server: %v", err)
	}
	if updated.Name != "worker-1b" || updated.Port != 2200 || updated.Description != "updated" {
		t.Fatalf("server was not updated: %#v", updated)
	}
	if updated.StartupInputAfterConnect != "menu\nexit\n" || updated.ForceShellCommand != "/usr/bin/env sh" {
		t.Fatalf("server advanced startup settings were not updated: %#v", updated)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("unexpected server list: %#v", list)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete server: %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := store.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting twice, got %v", err)
	}
}

func TestServerStoreValidatesRequests(t *testing.T) {
	ctx := context.Background()
	database := openServerTestDB(t)
	keyID := insertServerTestSSHKey(t, database, "main")
	store := NewStore(database)

	if ValidationError("bad server").Error() != "bad server" {
		t.Fatalf("validation error should return message")
	}
	requests := []CreateRequest{
		{Name: "", Host: "host", Port: 22, Username: "root", SSHKeyID: keyID},
		{Name: "server", Host: "", Port: 22, Username: "root", SSHKeyID: keyID},
		{Name: "bad\nserver", Host: "host", Port: 22, Username: "root", SSHKeyID: keyID},
		{Name: strings.Repeat("a", 81), Host: "host", Port: 22, Username: "root", SSHKeyID: keyID},
		{Name: "server", Host: "http://example.test", Port: 22, Username: "root", SSHKeyID: keyID},
		{Name: "server", Host: "bad host", Port: 22, Username: "root", SSHKeyID: keyID},
		{Name: "server", Host: strings.Repeat("a", 256), Port: 22, Username: "root", SSHKeyID: keyID},
		{Name: "server", Host: "host", Port: -1, Username: "root", SSHKeyID: keyID},
		{Name: "server", Host: "host", Port: 65536, Username: "root", SSHKeyID: keyID},
		{Name: "server", Host: "host", Port: 22, Username: "", SSHKeyID: keyID},
		{Name: "server", Host: "host", Port: 22, Username: "bad user", SSHKeyID: keyID},
		{Name: "server", Host: "host", Port: 22, Username: strings.Repeat("a", 65), SSHKeyID: keyID},
		{Name: "server", Host: "host", Port: 22, Username: "root", SSHKeyID: keyID, Description: "bad\ndescription"},
		{Name: "server", Host: "host", Port: 22, Username: "root", SSHKeyID: keyID, Description: strings.Repeat("a", 1001)},
		{Name: "server", Host: "host", Port: 22, Username: "root", SSHKeyID: keyID, StartupInputAfterConnect: strings.Repeat("a", 2001)},
		{Name: "server", Host: "host", Port: 22, Username: "root", SSHKeyID: keyID, StartupInputAfterConnect: "bad\x00input"},
		{Name: "server", Host: "host", Port: 22, Username: "root", SSHKeyID: keyID, ForceShellCommand: "bad\ncommand"},
		{Name: "server", Host: "host", Port: 22, Username: "root", SSHKeyID: keyID, ForceShellCommand: strings.Repeat("a", 201)},
		{Name: "server", Host: "host", Port: 22, Username: "root", SSHKeyID: 0},
		{Name: "server", Host: "host", Port: 22, Username: "root", SSHKeyID: 999},
	}
	for _, request := range requests {
		if _, err := store.Create(ctx, request); err == nil {
			t.Fatalf("expected validation error for %#v", request)
		}
	}
	if _, err := store.Update(ctx, 999, CreateRequest{Name: "server", Host: "host", Port: 22, Username: "root", SSHKeyID: keyID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating missing server, got %v", err)
	}
}

func TestServerStoreDuplicateNameReturnsValidationError(t *testing.T) {
	ctx := context.Background()
	database := openServerTestDB(t)
	keyID := insertServerTestSSHKey(t, database, "main")
	store := NewStore(database)

	first, err := store.Create(ctx, CreateRequest{Name: "worker-1", Host: "10.0.0.1", Username: "root", SSHKeyID: keyID})
	if err != nil {
		t.Fatalf("create first server: %v", err)
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "worker-1", Host: "10.0.0.2", Username: "root", SSHKeyID: keyID}); !errors.As(err, new(ValidationError)) {
		t.Fatalf("expected duplicate create validation error, got %v", err)
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "worker-2", Host: "10.0.0.2", Username: "root", SSHKeyID: keyID}); err != nil {
		t.Fatalf("create second server: %v", err)
	}
	if _, err := store.Update(ctx, first.ID, CreateRequest{Name: "worker-2", Host: "10.0.0.1", Username: "root", SSHKeyID: keyID}); !errors.As(err, new(ValidationError)) {
		t.Fatalf("expected duplicate update validation error, got %v", err)
	}
}
