package lifecycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	workspacehttp "github.com/aipermission/aipermission/backend/internal/workspacelifecycle/httpapi"
)

type boundaryRuntime struct{ identity workspacelifecycle.Identity }

func (r *boundaryRuntime) WorkspaceIdentity() workspacelifecycle.Identity { return r.identity }

// These adapter tests exercise metadata and ownership, not database I/O.
func (*boundaryRuntime) WorkspaceDatabase() *sql.DB { return nil }

func boundaryComponent(t *testing.T) (*Component, Dependencies) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "default.db")
	component := NewComponent(path, "local-default", func(r Runtime) workspacelifecycle.Identity { return r.WorkspaceIdentity() })
	dependencies := Dependencies{
		DataPath: path,
		Open: func(context.Context, string, string, string) (Runtime, error) {
			t.Error("unexpected runtime open")
			return nil, errors.New("unexpected runtime open")
		},
		Close: func(Runtime) error { return nil },
	}
	return component, dependencies
}

func TestBoundaryConfigurationFailsWithoutReplacingService(t *testing.T) {
	component, dependencies := boundaryComponent(t)
	for _, invalidate := range []func(*Dependencies){
		func(d *Dependencies) { d.DataPath = "" },
		func(d *Dependencies) { d.Open = nil },
		func(d *Dependencies) { d.Close = nil },
	} {
		invalid := dependencies
		invalidate(&invalid)
		if err := component.Configure(invalid); err == nil {
			t.Fatal("incomplete dependencies accepted")
		}
		if component.HTTP(HTTPDependencies{}) != nil {
			t.Fatal("failed configuration published HTTP service")
		}
	}
	if err := component.Configure(dependencies); err != nil {
		t.Fatal(err)
	}
	original := component.service
	for _, invalid := range []Dependencies{{}, {DataPath: dependencies.DataPath, Open: dependencies.Open}, {DataPath: dependencies.DataPath, Close: dependencies.Close}} {
		if err := component.Configure(invalid); err == nil {
			t.Fatal("invalid reconfiguration accepted")
		}
		if component.service != original {
			t.Fatal("rejected configuration replaced existing service")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	release, err := component.AcquireReadContext(ctx)
	if err != nil || release == nil {
		t.Fatalf("existing service lost after rejected configuration: %v", err)
	}
	release()
}

func TestBoundaryRegistrySurvivesConfigurationAndClearsOnClose(t *testing.T) {
	component, dependencies := boundaryComponent(t)
	first := &boundaryRuntime{identity: workspacelifecycle.Identity{ID: "first", Path: dependencies.DataPath}}
	second := &boundaryRuntime{identity: workspacelifecycle.Identity{ID: "second", Path: dependencies.DataPath}}
	component.Activate(nil)
	component.Activate(first)
	component.Activate(second)
	for _, configured := range []bool{false, true} {
		if configured {
			if err := component.Configure(dependencies); err != nil {
				t.Fatal(err)
			}
		}
		if !component.IsUnlocked() || component.Len() != 2 || component.Active() != second || component.Selection() != second.identity {
			t.Fatalf("registry selection changed (configured=%t)", configured)
		}
		if found, ok := component.Lookup("first"); !ok || found != first {
			t.Fatal("inactive runtime lost")
		}
		if found, ok := component.Lookup("missing"); ok || found != nil {
			t.Fatal("unknown runtime found")
		}
		if snapshot := component.Snapshot(); len(snapshot) != 2 || snapshot[0] != second {
			t.Fatal("snapshot lost active-first ordering")
		}
	}
	if name, err := component.DatabaseName(); err != nil || name == "" {
		t.Fatalf("database name = %q, %v", name, err)
	}
	if err := component.CloseAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if component.IsUnlocked() || component.Len() != 0 || component.Active() != nil || len(component.Snapshot()) != 0 {
		t.Fatal("closed registry still exposes runtime")
	}
}

func TestBoundaryGateCancellationRetainsRegistry(t *testing.T) {
	component, dependencies := boundaryComponent(t)
	if err := component.Configure(dependencies); err != nil {
		t.Fatal(err)
	}
	runtime := &boundaryRuntime{identity: workspacelifecycle.Identity{ID: "active", Path: dependencies.DataPath}}
	component.Activate(runtime)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, acquire := range []func(context.Context) (func(), error){component.AcquireReadContext, component.AcquireMutationContext} {
		if release, err := acquire(ctx); release != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled acquisition = %t, %v", release != nil, err)
		}
	}
	for _, acquire := range []func(context.Context) (func(), error){component.AcquireReadContext, component.AcquireMutationContext} {
		available, stop := context.WithTimeout(context.Background(), 3*time.Second)
		release, err := acquire(available)
		stop()
		if err != nil || release == nil {
			t.Fatalf("gate remained unavailable: %v", err)
		}
		release()
	}
	if _, err := component.Import(ctx, workspacelifecycle.ImportInput{}); !errors.Is(err, workspacelifecycle.ErrNameRequired) {
		t.Fatalf("import validation lost: %v", err)
	}
	if component.Active() != runtime || component.Len() != 1 {
		t.Fatal("rejected request changed registry")
	}
}

func TestBoundaryCloseCancellationDetachesBeforeWaiting(t *testing.T) {
	component, dependencies := boundaryComponent(t)
	entered, finish, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(finish) }) }
	t.Cleanup(release)
	dependencies.Close = func(Runtime) error { close(entered); <-finish; close(closed); return nil }
	if err := component.Configure(dependencies); err != nil {
		t.Fatal(err)
	}
	component.Activate(&boundaryRuntime{identity: workspacelifecycle.Identity{ID: "active", Path: dependencies.DataPath}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- component.CloseAll(ctx) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("close callback did not begin")
	}
	if component.IsUnlocked() || component.Active() != nil || component.Len() != 0 {
		t.Fatal("close began waiting while registry authority remained attached")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("close error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("close ignored cancellation")
	}
	if component.IsUnlocked() || component.Active() != nil {
		t.Fatal("canceled close retained registry authority")
	}
	release()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("close callback did not finish")
	}
}

func TestBoundaryHTTPPreservesSessionAndAttemptPolicy(t *testing.T) {
	component, dependencies := boundaryComponent(t)
	if err := component.Configure(dependencies); err != nil {
		t.Fatal(err)
	}
	component.Activate(&boundaryRuntime{identity: workspacelifecycle.Identity{ID: "active", Path: dependencies.DataPath}})
	for _, state := range []string{"missing", "unauthenticated", "authenticated"} {
		policy := HTTPDependencies{}
		if state != "missing" {
			policy.HasSession = func(*http.Request) bool { return state == "authenticated" }
		}
		handlers := component.HTTP(policy)
		response := httptest.NewRecorder()
		handlers.Status(response, httptest.NewRequest(http.MethodGet, "/status", nil))
		var status workspacehttp.StatusResponse
		if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		want := "session_required"
		if state == "authenticated" {
			want = "unlocked"
		}
		if response.Code != http.StatusOK || status.State != want || status.DatabaseID != "active" || strings.Contains(response.Body.String(), dependencies.DataPath) {
			t.Fatalf("status policy = %d/%s", response.Code, status.State)
		}
	}
	for _, blocked := range []bool{false, true} {
		policy := HTTPDependencies{}
		want := http.StatusInternalServerError
		if blocked {
			want = http.StatusTooManyRequests
			policy.BeginAttempt = func(w http.ResponseWriter, _ *http.Request) (PasswordAttempt, bool) {
				w.WriteHeader(http.StatusTooManyRequests)
				return nil, false
			}
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/unlock", strings.NewReader(`{"password":"fixture-password"}`))
		request.Header.Set("Content-Type", "application/json")
		component.HTTP(policy).Unlock(response, request)
		if response.Code != want || component.Len() != 1 {
			t.Fatalf("blocked unlock = %d, runtimes=%d", response.Code, component.Len())
		}
	}
}
