package lifecycle

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	workspacehttp "github.com/aipermission/aipermission/backend/internal/workspacelifecycle/httpapi"
)

type Runtime interface {
	WorkspaceIdentity() workspacelifecycle.Identity
	WorkspaceDatabase() *sql.DB
}
type PasswordAttempt interface {
	Success()
	Failure()
}

type HTTPDependencies struct {
	BeginAttempt       func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool)
	HasSession         func(*http.Request) bool
	IssueSession       func(http.ResponseWriter) error
	ClearSessions      func(http.ResponseWriter)
	InvalidateSessions func(string)
	CloseMaintenance   func(string)
	Now                func() time.Time
}

type HTTPHandlers interface {
	Status(http.ResponseWriter, *http.Request)
	Setup(http.ResponseWriter, *http.Request)
	Unlock(http.ResponseWriter, *http.Request)
	Lock(http.ResponseWriter, *http.Request)
	Rename(http.ResponseWriter, *http.Request)
	Delete(http.ResponseWriter, *http.Request)
	DeleteLocked(http.ResponseWriter, *http.Request)
	Switch(http.ResponseWriter, *http.Request)
	ChangePassword(http.ResponseWriter, *http.Request)
}

func InitializationError() error { return workspacelifecycle.ErrInitialization }

type Dependencies struct {
	DataPath              string
	Open                  func(context.Context, string, string, string) (Runtime, error)
	Close                 func(Runtime) error
	WaitClosed            func(context.Context, Runtime) error
	IsOwned               func(workspacelifecycle.Identity) bool
	OnActivated, OnOpened func(Runtime)
	Move                  func(string, string) error
	Delete                func(string) error
	ValidateNewPassword   func(context.Context, Runtime, string, string) error
	Publish               func(string, string) error
	GatewaySecret         func() string
}

// Component owns the runtime registry and serialized workspace lifecycle for
// one gateway process. The registry never escapes this boundary.
type Component struct {
	registry *workspacelifecycle.Registry[Runtime]
	service  *workspacelifecycle.Service[Runtime]
}

func NewComponent(path, id string, describe func(Runtime) workspacelifecycle.Identity) *Component {
	return &Component{registry: workspacelifecycle.NewRegistry(path, id, describe)}
}

func (component *Component) Configure(dependencies Dependencies) error {
	if component == nil || component.registry == nil {
		return InitializationError()
	}
	service, err := newService(dependencies, component.registry)
	if err != nil {
		return err
	}
	component.service = service
	return nil
}

func (component *Component) IsUnlocked() bool {
	return component != nil && component.registry != nil && component.registry.IsUnlocked()
}

func (component *Component) Selection() workspacelifecycle.Identity {
	if component == nil || component.registry == nil {
		return workspacelifecycle.Identity{}
	}
	return component.registry.Selection()
}

func (component *Component) Lookup(id string) (Runtime, bool) {
	if component == nil || component.registry == nil {
		return nil, false
	}
	return component.registry.Lookup(id)
}

func (component *Component) Activate(runtime Runtime) {
	if component != nil && component.registry != nil && runtime != nil {
		component.registry.Activate(runtime)
	}
}

func (component *Component) Active() Runtime {
	if component == nil || component.registry == nil {
		return nil
	}
	if component.service != nil {
		runtime, _ := component.service.Active()
		return runtime
	}
	runtime, _ := component.registry.Active()
	return runtime
}

func (component *Component) Snapshot() []Runtime {
	if component == nil || component.registry == nil {
		return nil
	}
	if component.service != nil {
		return component.service.Snapshot()
	}
	return component.registry.Snapshot()
}

func (component *Component) Len() int {
	if component == nil || component.registry == nil {
		return 0
	}
	return component.registry.Len()
}

func (component *Component) DatabaseName() (string, error) {
	if component == nil || component.service == nil {
		return "", InitializationError()
	}
	status, err := component.service.Status()
	return status.DatabaseName, err
}

func (component *Component) AcquireReadContext(ctx context.Context) (func(), error) {
	if component == nil || component.service == nil {
		return nil, InitializationError()
	}
	return component.service.AcquireReadContext(ctx)
}

func (component *Component) AcquireMutationContext(ctx context.Context) (func(), error) {
	if component == nil || component.service == nil {
		return nil, InitializationError()
	}
	return component.service.AcquireMutationContext(ctx)
}

func (component *Component) Import(ctx context.Context, input workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error) {
	if component == nil || component.service == nil {
		return workspacelifecycle.Transition{}, InitializationError()
	}
	return component.service.Import(ctx, input)
}

func (component *Component) CloseAll(ctx context.Context) error {
	if component == nil || component.service == nil {
		return InitializationError()
	}
	return component.service.CloseAll(ctx)
}

func (component *Component) HTTP(dependencies HTTPDependencies) HTTPHandlers {
	if component == nil || component.service == nil {
		return nil
	}
	return newHTTP(component.service, dependencies)
}

func newService(dependencies Dependencies, registry *workspacelifecycle.Registry[Runtime]) (*workspacelifecycle.Service[Runtime], error) {
	return workspacelifecycle.NewService(workspacelifecycle.Dependencies[Runtime]{
		DataPath: dependencies.DataPath, Registry: registry, Open: dependencies.Open,
		Close: dependencies.Close, WaitClosed: dependencies.WaitClosed, IsOwned: dependencies.IsOwned,
		OnActivated: dependencies.OnActivated, OnOpened: dependencies.OnOpened,
		Move: dependencies.Move, Delete: dependencies.Delete, ValidateNewPassword: dependencies.ValidateNewPassword,
		Publish: dependencies.Publish, GatewaySecret: dependencies.GatewaySecret,
	})
}

func newHTTP(service *workspacelifecycle.Service[Runtime], dependencies HTTPDependencies) HTTPHandlers {
	converted := workspacehttp.Dependencies{
		Lifecycle:  service,
		HasSession: dependencies.HasSession, IssueSession: dependencies.IssueSession,
		ClearSessions: dependencies.ClearSessions, InvalidateSessions: dependencies.InvalidateSessions,
		CloseMaintenance: dependencies.CloseMaintenance,
		Now:              dependencies.Now,
	}
	if dependencies.BeginAttempt != nil {
		converted.BeginAttempt = func(w http.ResponseWriter, r *http.Request) (workspacehttp.PasswordAttempt, bool) {
			return dependencies.BeginAttempt(w, r)
		}
	}
	return workspacehttp.New(converted)
}
