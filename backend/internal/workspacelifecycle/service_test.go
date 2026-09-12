package workspacelifecycle

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/db"
)

func TestServiceRequestGateSerializesMutationsAgainstReaders(t *testing.T) {
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: "/data/default.db",
		Registry: NewRegistry("/data/default.db", "default", func(runtime *serviceRuntime) Identity {
			return runtime.identity
		}),
		Open: func(context.Context, string, string, string) (*serviceRuntime, error) {
			return nil, errors.New("unused")
		},
		Close: func(*serviceRuntime) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	releaseRead, err := service.AcquireReadContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	acquiredMutation := make(chan struct{})
	done := make(chan struct{})
	go func() {
		releaseMutation, err := service.AcquireMutationContext(t.Context())
		if err != nil {
			t.Errorf("acquire mutation: %v", err)
			close(done)
			return
		}
		close(acquiredMutation)
		releaseMutation()
		close(done)
	}()

	select {
	case <-acquiredMutation:
		t.Fatal("mutation acquired while read lease was active")
	case <-time.After(25 * time.Millisecond):
	}
	releaseRead()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("mutation did not acquire after read lease was released")
	}
}

func TestServiceRequestGateAllowsConcurrentReaders(t *testing.T) {
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: "/data/default.db",
		Registry: NewRegistry("/data/default.db", "default", func(runtime *serviceRuntime) Identity {
			return runtime.identity
		}),
		Open: func(context.Context, string, string, string) (*serviceRuntime, error) {
			return nil, errors.New("unused")
		},
		Close: func(*serviceRuntime) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	releaseFirst, err := service.AcquireReadContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var acquired sync.WaitGroup
	acquired.Add(1)
	done := make(chan struct{})
	go func() {
		defer acquired.Done()
		releaseSecond, err := service.AcquireReadContext(t.Context())
		if err != nil {
			t.Errorf("acquire second reader: %v", err)
			close(done)
			return
		}
		releaseSecond()
		close(done)
	}()
	acquired.Wait()
	releaseFirst()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent reader did not complete")
	}
}

func TestServiceRequestGateMutationHonorsContextWhileReaderIsActive(t *testing.T) {
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: "/data/default.db",
		Registry: NewRegistry("/data/default.db", "default", func(runtime *serviceRuntime) Identity {
			return runtime.identity
		}),
		Open: func(context.Context, string, string, string) (*serviceRuntime, error) {
			return nil, errors.New("unused")
		},
		Close: func(*serviceRuntime) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	releaseRead, err := service.AcquireReadContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRead()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if release, err := service.AcquireMutationContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("AcquireMutationContext() error = %v, want deadline exceeded", err)
	}
}

func TestServiceCloseAllSignalsEveryRuntimeBeforeSharedDeadline(t *testing.T) {
	registry := NewRegistry("/data/default.db", "default", func(runtime *serviceRuntime) Identity { return runtime.identity })
	first := &serviceRuntime{identity: Identity{ID: "default", Path: "/data/default.db"}}
	second := &serviceRuntime{identity: Identity{ID: "second", Path: "/data/second.db"}}
	registry.Activate(first)
	registry.Activate(second)
	started := make(chan string, 2)
	release := make(chan struct{})
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: "/data/default.db", Registry: registry,
		Open: func(context.Context, string, string, string) (*serviceRuntime, error) {
			return nil, errors.New("unused")
		},
		Close: func(runtime *serviceRuntime) error {
			started <- runtime.identity.ID
			<-release
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if err := service.CloseAll(ctx); !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("CloseAll() error = %v, want deadline exceeded", err)
	}
	seen := map[string]bool{}
	for range 2 {
		select {
		case id := <-started:
			seen[id] = true
		default:
			close(release)
			t.Fatalf("not every runtime received a concurrent close signal: %v", seen)
		}
	}
	close(release)
	if !seen["default"] || !seen["second"] {
		t.Fatalf("closed runtimes = %v", seen)
	}
}

type serviceRuntime struct {
	identity Identity
	database *sql.DB
	secret   string
}

type deferredCloseTestError struct{}

func (deferredCloseTestError) Error() string           { return "close deferred" }
func (deferredCloseTestError) WorkspaceCloseDeferred() {}

func TestServiceLockTreatsDeferredCloseAsSuccessfulTransition(t *testing.T) {
	registry := NewRegistry("/data/default.db", "default", func(runtime *serviceRuntime) Identity { return runtime.identity })
	registry.Activate(&serviceRuntime{identity: Identity{ID: "default", Path: "/data/default.db"}})
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: "/data/default.db", Registry: registry,
		Open: func(context.Context, string, string, string) (*serviceRuntime, error) {
			return nil, errors.New("unused")
		},
		Close: func(*serviceRuntime) error {
			return errors.Join(errors.New("initial drain timeout"), deferredCloseTestError{})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.Lock("all")
	if err != nil || status.State == "unlocked" || service.IsUnlocked() {
		t.Fatalf("status=%#v unlocked=%t err=%v", status, service.IsUnlocked(), err)
	}
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
		Open: func(_ context.Context, path, id, password string) (*serviceRuntime, error) {
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
	if _, err := service.Unlock(t.Context(), "default", "DefaultPassword123"); err != nil {
		t.Fatal(err)
	}
	if transition, err := service.Switch(t.Context(), "second", "SecondPassword123"); err != nil || transition.Status != "switched" {
		t.Fatalf("switch failed: transition=%#v err=%v", transition, err)
	}
	if _, err := service.Switch(t.Context(), "default", ""); err != nil {
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
		Open: func(context.Context, string, string, string) (*serviceRuntime, error) {
			return nil, errors.New("open failed")
		},
		Close: func(*serviceRuntime) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Switch(t.Context(), "missing", "Password123456"); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("err=%v", err)
	}
	if current, ok := service.Active(); !ok || current != active {
		t.Fatalf("active runtime changed: %#v", current)
	}
}

func TestServiceUnlockCancelsRuntimeOpeningWithCaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aipermission.db")
	database, err := db.OpenEncrypted(path, "DefaultPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(path, "default", func(runtime *serviceRuntime) Identity { return runtime.identity })
	openStarted := make(chan struct{})
	closed := false
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: path, Registry: registry,
		Open: func(ctx context.Context, openedPath, id, _ string) (*serviceRuntime, error) {
			close(openStarted)
			<-ctx.Done()
			return &serviceRuntime{identity: Identity{ID: id, Path: openedPath}}, nil
		},
		Close: func(*serviceRuntime) error { closed = true; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := service.Unlock(ctx, "default", "DefaultPassword123"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unlock error = %v, want deadline exceeded", err)
	}
	select {
	case <-openStarted:
	default:
		t.Fatal("runtime opener did not receive the request context")
	}
	if service.IsUnlocked() {
		t.Fatal("canceled runtime opening activated the workspace")
	}
	if !closed {
		t.Fatal("runtime returned after cancellation was not closed")
	}
}

func TestServiceRenameFailureReopensOriginalRuntime(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "aipermission.db")
	const password = "DatabasePassword123"
	database, err := db.OpenEncrypted(path, password)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &serviceRuntime{identity: Identity{ID: "default", Path: path}, database: database}
	registry := NewRegistry(path, "default", func(runtime *serviceRuntime) Identity { return runtime.identity })
	registry.Activate(runtime)
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: path, Registry: registry,
		Open: func(_ context.Context, path, id, password string) (*serviceRuntime, error) {
			database, err := db.OpenEncrypted(path, password)
			return &serviceRuntime{identity: Identity{ID: id, Path: path}, database: database}, err
		},
		Close: func(runtime *serviceRuntime) error { return runtime.database.Close() },
		Move:  func(string, string) error { return errors.New("injected move failure") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Rename(t.Context(), "Renamed", password); err == nil || !CredentialWasVerified(err) {
		t.Fatalf("rename error=%v verified=%t", err, CredentialWasVerified(err))
	}
	reopened, ok := service.Active()
	if !ok || reopened.identity.ID != "default" || reopened.identity.Path != path {
		t.Fatalf("original runtime was not restored: %#v", reopened)
	}
	if err := reopened.database.PingContext(t.Context()); err != nil {
		t.Fatalf("reopened database is unusable: %v", err)
	}
	if err := service.CloseAll(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestServiceDeleteConfirmationFailsBeforeClosingRuntime(t *testing.T) {
	registry := NewRegistry("/data/default.db", "default", func(runtime *serviceRuntime) Identity { return runtime.identity })
	runtime := &serviceRuntime{identity: Identity{ID: "default", Path: "/data/default.db"}}
	registry.Activate(runtime)
	closed := false
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: "/data/default.db", Registry: registry,
		Open: func(context.Context, string, string, string) (*serviceRuntime, error) {
			return nil, errors.New("unused")
		},
		Close: func(*serviceRuntime) error { closed = true; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteCurrent(t.Context(), "wrong", "Password123456"); !errors.Is(err, ErrNameConfirmation) {
		t.Fatalf("delete error=%v", err)
	}
	if closed || !service.IsUnlocked() {
		t.Fatalf("confirmation failure changed runtime: closed=%t unlocked=%t", closed, service.IsUnlocked())
	}
}

func TestServiceDeleteCloseFailureStillPromotesRemainingRuntime(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	firstDB, err := db.OpenEncrypted(defaultPath, "Password123456")
	if err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(root, "databases", "second.db")
	secondDB, err := db.OpenEncrypted(secondPath, "Password123456")
	if err != nil {
		t.Fatal(err)
	}
	defer firstDB.Close()
	defer secondDB.Close()
	registry := NewRegistry(defaultPath, "default", func(runtime *serviceRuntime) Identity { return runtime.identity })
	first := &serviceRuntime{identity: Identity{ID: "default", Path: defaultPath}, database: firstDB}
	second := &serviceRuntime{identity: Identity{ID: "second", Path: secondPath}, database: secondDB}
	registry.Activate(second)
	registry.Activate(first)
	var activated *serviceRuntime
	service, err := NewService(Dependencies[*serviceRuntime]{
		DataPath: defaultPath, Registry: registry,
		Open: func(context.Context, string, string, string) (*serviceRuntime, error) {
			return nil, errors.New("unused")
		},
		Close:       func(*serviceRuntime) error { return errors.New("close failed") },
		Validate:    func(string, string) error { return nil },
		OnActivated: func(runtime *serviceRuntime) { activated = runtime },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteCurrent(t.Context(), "Default", "Password123456"); err == nil {
		t.Fatal("expected close failure")
	}
	if active, ok := service.Active(); !ok || active != second || activated != second {
		t.Fatalf("remaining runtime was not fully promoted: active=%#v activated=%#v", active, activated)
	}
}
