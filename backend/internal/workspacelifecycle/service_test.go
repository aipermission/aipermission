package workspacelifecycle

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db"
)

type serviceRuntime struct {
	identity Identity
	database *sql.DB
	secret   string
}

func (r *serviceRuntime) WorkspaceIdentity() Identity    { return r.identity }
func (r *serviceRuntime) WorkspaceDatabase() *sql.DB     { return r.database }
func (r *serviceRuntime) WorkspaceGatewaySecret() string { return r.secret }

func TestServiceUnlockSwitchAndLockLifecycle(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	secondPath := filepath.Join(root, "databases", "second.db")
	for _, candidate := range []struct{ path, password string }{{defaultPath, "DefaultPassword123"}, {secondPath, "SecondPassword123"}} {
		database, err := db.OpenEncrypted(candidate.path, candidate.password)
		if err != nil {
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
	}
	registry := NewRegistry(defaultPath, "default", func(runtime *serviceRuntime) Identity { return runtime.identity })
	opened, closed := []string{}, []string{}
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: defaultPath, Registry: registry,
		Open: func(path, id, password string) (*serviceRuntime, error) {
			database, err := db.OpenEncrypted(path, password)
			if err != nil {
				return nil, err
			}
			opened = append(opened, id)
			return &serviceRuntime{identity: Identity{ID: id, Path: path}, database: database}, nil
		},
		Close: func(runtime *serviceRuntime) error {
			closed = append(closed, runtime.identity.ID)
			return runtime.database.Close()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Unlock("default", "DefaultPassword123"); err != nil {
		t.Fatal(err)
	}
	if transition, err := service.Switch("second", "SecondPassword123"); err != nil || transition.Status != "switched" {
		t.Fatalf("switch failed: transition=%#v err=%v", transition, err)
	}
	if _, err := service.Switch("default", ""); err != nil {
		t.Fatalf("switch to unlocked runtime: %v", err)
	}
	if status, err := service.Lock("current"); err != nil || status.State != "unlocked" || status.Identity.ID != "second" {
		t.Fatalf("lock current failed: status=%#v err=%v", status, err)
	}
	if status, err := service.Lock("all"); err != nil || status.State == "unlocked" {
		t.Fatalf("lock all failed: status=%#v err=%v", status, err)
	}
	if len(opened) != 2 || len(closed) != 2 {
		t.Fatalf("opened=%v closed=%v", opened, closed)
	}
}

func TestServiceFailedSwitchPreservesActiveRuntime(t *testing.T) {
	registry := NewRegistry("/data/default.db", "default", func(runtime *serviceRuntime) Identity { return runtime.identity })
	active := &serviceRuntime{identity: Identity{ID: "default", Path: "/data/default.db"}}
	registry.Activate(active)
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: "/data/default.db", Registry: registry,
		Open:  func(string, string, string) (*serviceRuntime, error) { return nil, errors.New("open failed") },
		Close: func(*serviceRuntime) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Switch("missing", "Password123456"); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("err=%v", err)
	}
	if current, ok := service.Active(); !ok || current != active {
		t.Fatalf("active runtime changed: %#v", current)
	}
}
