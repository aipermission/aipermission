// Package gatewayworkspace is the bounded workspace lifecycle and encrypted-runtime composition API.
package gatewayworkspace

import (
	"context"
	"database/sql"
	"time"

	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/catalog"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/lifecycle"
	workspaceruntime "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type Runtime = workspaceruntime.Runtime
type Registry = workspacelifecycle.Registry[*Runtime]
type Service = workspacelifecycle.Service[*Runtime]
type Vault = workspaceruntime.Vault
type TokenStore = workspaceruntime.TokenStore
type AdoptInput = workspaceruntime.AdoptInput
type OpenInput = workspaceruntime.OpenInput
type Identity = lifecycle.Identity
type Dependencies = lifecycle.Dependencies
type HTTPDependencies = lifecycle.HTTPDependencies
type HTTPHandlers = lifecycle.HTTPHandlers
type PasswordAttempt = lifecycle.PasswordAttempt
type UnlockRequest = lifecycle.UnlockRequest
type SetupRequest = lifecycle.SetupRequest
type StatusResponse = lifecycle.StatusResponse
type RenameRequest = lifecycle.RenameRequest
type DeleteRequest = lifecycle.DeleteRequest
type DeleteLockedRequest = lifecycle.DeleteLockedRequest
type SwitchRequest = lifecycle.SwitchRequest
type ChangePasswordRequest = lifecycle.ChangePasswordRequest
type ActionWorkflow = workspaceruntime.ActionWorkflow

var (
	ErrDatabaseInUse  = catalog.ErrDatabaseInUse
	ErrAuthentication = lifecycle.ErrAuthentication
	ErrInitialization = lifecycle.ErrInitialization
)

func Scavenge(path string, now time.Time)         { catalog.Scavenge(path, now) }
func DefaultID(path string) string                { return catalog.DefaultID(path) }
func Move(currentPath, targetPath string) error   { return catalog.Move(currentPath, targetPath) }
func Delete(path string) error                    { return catalog.Delete(path) }
func Publish(sourcePath, targetPath string) error { return catalog.Publish(sourcePath, targetPath) }
func LooksPlaintext(path string) bool             { return catalog.LooksPlaintext(path) }
func UnsupportedSchemaMessage(err error) string   { return catalog.UnsupportedSchemaMessage(err) }
func Adopt(ctx context.Context, input AdoptInput) (*Runtime, error) {
	return workspaceruntime.Adopt(ctx, input)
}
func Open(ctx context.Context, input OpenInput) (*Runtime, error) {
	return workspaceruntime.Open(ctx, input)
}
func Discard(runtime *Runtime) error { return workspaceruntime.Discard(runtime) }
func Close(runtime *Runtime, resolve func() (ActionWorkflow, error)) error {
	return workspaceruntime.Close(runtime, resolve)
}
func NewRegistry(path, id string, describe func(*Runtime) Identity) *Registry {
	return lifecycle.NewRegistry(path, id, describe)
}
func NewService(dependencies Dependencies) (*Service, error) {
	return lifecycle.NewService(dependencies)
}
func NewHTTP(dependencies HTTPDependencies) *HTTPHandlers { return lifecycle.NewHTTP(dependencies) }
func ValidatePassword(password, confirmation string) error {
	return lifecycle.ValidatePassword(password, confirmation)
}
func PasswordPolicyError(err error) error { return lifecycle.PasswordPolicyError(err) }
func ValidateRemoteBackupPassword(password, name string) error {
	return lifecycle.ValidateRemoteBackupPassword(password, name)
}
func HasActiveRemoteBackup(ctx context.Context, database *sql.DB) (bool, error) {
	return lifecycle.HasActiveRemoteBackup(ctx, database)
}
