package lifecycle

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimecontract"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	workspacehttp "github.com/aipermission/aipermission/backend/internal/workspacelifecycle/httpapi"
)

type Runtime = runtimecontract.Runtime
type Identity = workspacelifecycle.Identity
type PasswordAttempt = workspacehttp.PasswordAttempt
type HTTPDependencies = workspacehttp.Dependencies
type HTTPHandlers = workspacehttp.Handlers
type UnlockRequest = workspacehttp.UnlockRequest
type SetupRequest = workspacehttp.SetupRequest
type StatusResponse = workspacehttp.StatusResponse
type RenameRequest = workspacehttp.RenameRequest
type DeleteRequest = workspacehttp.DeleteRequest
type DeleteLockedRequest = workspacehttp.DeleteLockedRequest
type SwitchRequest = workspacehttp.SwitchRequest
type ChangePasswordRequest = workspacehttp.ChangePasswordRequest

var (
	ErrAuthentication = workspacelifecycle.ErrAuthentication
	ErrInitialization = workspacelifecycle.ErrInitialization
)

type Dependencies struct {
	DataPath              string
	Registry              *workspacelifecycle.Registry[Runtime]
	Open                  func(string, string, string) (Runtime, error)
	Close                 func(Runtime) error
	OnActivated, OnOpened func(Runtime)
	Move                  func(string, string) error
	Delete                func(string) error
	ValidateNewPassword   func(context.Context, *sql.DB, string, string) error
	Publish               func(string, string) error
	GatewaySecret         func() string
}

func NewRegistry(path, id string, describe func(Runtime) Identity) *workspacelifecycle.Registry[Runtime] {
	return workspacelifecycle.NewRegistry(path, id, describe)
}

func NewService(dependencies Dependencies) (*workspacelifecycle.Service[Runtime], error) {
	return workspacelifecycle.NewService(workspacelifecycle.Dependencies[Runtime]{
		DataPath: dependencies.DataPath, Registry: dependencies.Registry, Open: dependencies.Open,
		Close: dependencies.Close, OnActivated: dependencies.OnActivated, OnOpened: dependencies.OnOpened,
		Move: dependencies.Move, Delete: dependencies.Delete, ValidateNewPassword: dependencies.ValidateNewPassword,
		Publish: dependencies.Publish, GatewaySecret: dependencies.GatewaySecret,
	})
}

func NewHTTP(dependencies HTTPDependencies) *HTTPHandlers { return workspacehttp.New(dependencies) }
func ValidatePassword(password, confirmation string) error {
	return workspacelifecycle.ValidatePassword(password, confirmation)
}
func PasswordPolicyError(err error) error { return workspacelifecycle.PasswordPolicyError(err) }
func ValidateRemoteBackupPassword(password, databaseName string) error {
	return backups.ValidateRemoteBackupPassword(password, databaseName)
}
func HasActiveRemoteBackup(ctx context.Context, database *sql.DB) (bool, error) {
	return backups.NewStore(database).HasActiveProvider(ctx)
}
