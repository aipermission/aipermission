package workspacelifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

func TestImportRollsBackPublishedDatabaseWhenRuntimeOpenFails(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	sourcePath := filepath.Join(t.TempDir(), "source.aipdb")
	const password = "ImportedPassword123"
	source, err := db.OpenEncrypted(sourcePath, password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectvault.ResolveGatewaySecret(t.Context(), source, "gateway-secret"); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(defaultPath, "default", func(runtime *serviceRuntime) Identity { return runtime.identity })
	previous := &serviceRuntime{identity: Identity{ID: "default", Path: defaultPath}}
	registry.Activate(previous)
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: defaultPath, Registry: registry,
		Open: func(context.Context, string, string, string) (*serviceRuntime, error) {
			return nil, errors.New("injected open failure")
		},
		Close:         func(*serviceRuntime) error { return nil },
		GatewaySecret: func() string { return "gateway-secret" },
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared := false
	_, err = service.Import(t.Context(), ImportInput{
		DatabaseName: "Restored Copy", Password: password,
		Write: func(path string) error {
			content, err := os.ReadFile(sourcePath)
			if err != nil {
				return err
			}
			return os.WriteFile(path, content, 0o600)
		},
		BeforePublish: func() error { prepared = true; return nil },
	})
	if err == nil || !CredentialWasVerified(err) {
		t.Fatalf("import error=%v verified=%t", err, CredentialWasVerified(err))
	}
	if !prepared {
		t.Fatal("activation was not prepared before publish")
	}
	if active, ok := service.Active(); !ok || active != previous {
		t.Fatalf("previous runtime was not preserved: %#v", active)
	}
	targetPath, pathErr := databasecatalog.DatabasePath(defaultPath, "restored-copy")
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if db.Exists(targetPath) {
		t.Fatalf("failed import left published database at %s", targetPath)
	}
}

func TestImportRejectsWrongPasswordBeforePublish(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	sourcePath := filepath.Join(t.TempDir(), "source.aipdb")
	source, err := db.OpenEncrypted(sourcePath, "CorrectPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(defaultPath, "default", func(runtime *serviceRuntime) Identity { return runtime.identity })
	published := false
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: defaultPath, Registry: registry,
		Open: func(context.Context, string, string, string) (*serviceRuntime, error) {
			return nil, errors.New("unused")
		},
		Close:   func(*serviceRuntime) error { return nil },
		Publish: func(string, string) error { published = true; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Import(t.Context(), ImportInput{
		DatabaseName: "Rejected", Password: "WrongPassword123",
		Write: func(path string) error {
			content, readErr := os.ReadFile(sourcePath)
			if readErr != nil {
				return readErr
			}
			return os.WriteFile(path, content, 0o600)
		},
	})
	if !errors.Is(err, ErrCredential) || published {
		t.Fatalf("error=%v published=%t", err, published)
	}
}
