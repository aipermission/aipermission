package gatewayworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	workspacehttp "github.com/aipermission/aipermission/backend/internal/workspacelifecycle/httpapi"
)

type workspacePasswordAttempt struct{ succeeded, failed int }

func (attempt *workspacePasswordAttempt) Success() { attempt.succeeded++ }
func (attempt *workspacePasswordAttempt) Failure() { attempt.failed++ }

func TestNativeLifecycleHTTPUsesOneOwnedRuntimeAndClearsItOnLock(t *testing.T) {
	input := nativeWorkspaceInput(t, "default")
	seed := openNativeWorkspace(t, input)
	if err := (&Component{}).Discard(seed, nil, nil); err != nil {
		t.Fatal(err)
	}
	component := NewComponent(input.Path, nil)
	var opened, activated, closed *Runtime
	openCount, closeCount := 0, 0
	dependencies := Dependencies{
		DataPath: input.Path,
		Open: func(ctx context.Context, path, id, password string) (*Runtime, error) {
			if path != input.Path || id != "default" || password != boundaryPassword {
				t.Fatal("lifecycle rewrote native open input")
			}
			openCount++
			copy := input
			copy.Path, copy.ID, copy.Password = path, id, password
			return component.Open(ctx, copy)
		},
		Close: func(runtime *Runtime) error {
			closeCount++
			closed = runtime
			return component.Close(runtime, nil, nil, nil, nil)
		},
		WaitClosed:  func(ctx context.Context, runtime *Runtime) error { return runtime.WaitTeardown(ctx) },
		OnOpened:    func(runtime *Runtime) { opened = runtime },
		OnActivated: func(runtime *Runtime) { activated = runtime },
	}
	if err := component.Configure(dependencies); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := component.CloseAll(context.Background()); err != nil {
			t.Errorf("close lifecycle fixture: %v", err)
		}
	})
	attempt := &workspacePasswordAttempt{}
	issued, cleared := 0, 0
	handlers := component.HTTP(HTTPDependencies{
		BeginAttempt:  func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool) { return attempt, true },
		IssueSession:  func(http.ResponseWriter) error { issued++; return nil },
		ClearSessions: func(http.ResponseWriter) { cleared++ },
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/unlock", strings.NewReader(`{"password":"disposable-workspace-password","database_id":"default"}`))
	request.Header.Set("Content-Type", "application/json")
	handlers.Unlock(response, request)
	if response.Code != http.StatusOK || attempt.succeeded != 1 || attempt.failed != 0 || issued != 1 || openCount != 1 ||
		opened == nil || activated != opened || component.Active() != opened || !component.IsUnlocked() || component.Len() != 1 {
		t.Fatalf("native unlock failed or ownership diverged: status=%d opens=%d attempts=%+v", response.Code, openCount, attempt)
	}
	if found, ok := component.Lookup("default"); !ok || found != opened {
		t.Fatal("unlock runtime not retained under exact database ID")
	}
	if selection := component.Selection(); selection.ID != "default" || selection.Path != input.Path || selection.RetryIdentity != opened.Identity.UIRetryID {
		t.Fatalf("selection lost native runtime identity: %+v", selection)
	}
	if snapshot := component.Snapshot(); len(snapshot) != 1 || snapshot[0] != opened {
		t.Fatal("snapshot lost runtime ownership")
	}
	if name, err := component.DatabaseName(); err != nil || name != "Default" {
		t.Fatalf("database name = %q %v", name, err)
	}
	for _, acquire := range []func(context.Context) (func(), error){component.AcquireReadContext, component.AcquireMutationContext} {
		release, err := acquire(context.Background())
		if err != nil || release == nil {
			t.Fatalf("configured lifecycle gate: %v", err)
		}
		release()
	}
	if _, err := component.Import(context.Background(), workspacelifecycle.ImportInput{}); !errors.Is(err, workspacelifecycle.ErrNameRequired) {
		t.Fatalf("import validation bridge: %v", err)
	}
	response = httptest.NewRecorder()
	handlers.Status(response, httptest.NewRequest(http.MethodGet, "/status", nil))
	var status workspacehttp.StatusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || status.State != "session_required" || status.DatabaseID != "default" || strings.Contains(response.Body.String(), input.Path) {
		t.Fatal("status failed UI-session or private-path boundary")
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/lock", strings.NewReader(`{"scope":"all"}`))
	request.Header.Set("Content-Type", "application/json")
	handlers.Lock(response, request)
	if response.Code != http.StatusOK || cleared != 1 || closeCount != 1 || closed != opened || opened.WorkspaceDatabase().Ping() == nil ||
		component.IsUnlocked() || component.Active() != nil || component.Len() != 0 || len(component.Snapshot()) != 0 {
		t.Fatalf("lock retained runtime authority: status=%d clears=%d closes=%d", response.Code, cleared, closeCount)
	}
}

func TestAbsentWorkspaceLifecycleDoesNotPublishAuthority(t *testing.T) {
	for _, component := range []*Component{nil, {}} {
		if component.IsUnlocked() || component.Selection() != (Identity{}) || component.Active() != nil || component.Len() != 0 ||
			len(component.Snapshot()) != 0 || component.HTTP(HTTPDependencies{}) != nil {
			t.Fatal("absent lifecycle published runtime authority")
		}
		if runtime, ok := component.Lookup("missing"); ok || runtime != nil {
			t.Fatal("absent lifecycle returned a runtime")
		}
		component.Activate(nil)
		if err := component.Configure(Dependencies{}); !errors.Is(err, InitializationError()) {
			t.Fatalf("absent configuration = %v", err)
		}
		if name, err := component.DatabaseName(); name != "" || !errors.Is(err, InitializationError()) {
			t.Fatalf("absent database name = %q %v", name, err)
		}
		if _, err := component.Import(context.Background(), workspacelifecycle.ImportInput{}); !errors.Is(err, InitializationError()) {
			t.Fatalf("absent import = %v", err)
		}
	}
}
