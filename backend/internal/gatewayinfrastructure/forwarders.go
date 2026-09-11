package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/gatewayoptions"
	"github.com/aipermission/aipermission/backend/internal/gatewayroutes"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

func ResolveOptions(options []gatewayoptions.Option) gatewayoptions.Options {
	return gatewayoptions.Resolve(options)
}

func WithConnectorAdapterRegistry(registry *gatewayoptions.ConnectorAdapterRegistry) gatewayoptions.Option {
	return gatewayoptions.WithConnectorAdapterRegistry(registry)
}

func WithConnectorRegistry(registry *gatewayoptions.ConnectorRegistry) gatewayoptions.Option {
	return gatewayoptions.WithConnectorRegistry(registry)
}

func WithMaintenanceConsole(runtime gatewayoptions.MaintenanceConsoleRuntime) gatewayoptions.Option {
	return gatewayoptions.WithMaintenanceConsole(runtime)
}

func WithRuntimeInstanceIDGenerator(generator func() (string, error)) gatewayoptions.Option {
	return gatewayoptions.WithRuntimeInstanceIDGenerator(generator)
}

func Health(w http.ResponseWriter, arg1 *http.Request) {
	gatewayroutes.Health(w, arg1)
}

func RegisterRoutes(mux *http.ServeMux, d gatewayroutes.Dependencies) {
	gatewayroutes.Register(mux, d)
}

func Scavenge(path string, now time.Time) {
	gatewayworkspace.Scavenge(path, now)
}

func DefaultID(path string) string {
	return gatewayworkspace.DefaultID(path)
}

func Move(currentPath string, targetPath string) error {
	return gatewayworkspace.Move(currentPath, targetPath)
}

func Publish(sourcePath string, targetPath string) error {
	return gatewayworkspace.Publish(sourcePath, targetPath)
}

func Adopt(ctx context.Context, input gatewayworkspace.AdoptInput) (gatewayworkspace.Runtime, error) {
	return gatewayworkspace.Adopt(ctx, input)
}

func Open(ctx context.Context, input gatewayworkspace.OpenInput) (gatewayworkspace.Runtime, error) {
	return gatewayworkspace.Open(ctx, input)
}

func Discard(runtime gatewayworkspace.Runtime) error {
	return gatewayworkspace.Discard(runtime)
}

func Close(runtime gatewayworkspace.Runtime, resolve func() (gatewayworkspace.ActionWorkflow, error)) error {
	return gatewayworkspace.Close(runtime, resolve)
}

func Delete(path string) error {
	return gatewayworkspace.Delete(path)
}

func NewRegistry(path string, id string, describe func(gatewayworkspace.Runtime) gatewayworkspace.Identity) *gatewayworkspace.Registry {
	return gatewayworkspace.NewRegistry(path, id, describe)
}

func NewService(dependencies gatewayworkspace.Dependencies) (*gatewayworkspace.Service, error) {
	return gatewayworkspace.NewService(dependencies)
}

func NewWorkspaceHTTP(dependencies gatewayworkspace.HTTPDependencies) *gatewayworkspace.HTTPHandlers {
	return gatewayworkspace.NewHTTP(dependencies)
}

func HasActiveRemoteBackup(ctx context.Context, database *sql.DB) (bool, error) {
	return gatewayworkspace.HasActiveRemoteBackup(ctx, database)
}

func LooksPlaintext(path string) bool {
	return gatewayworkspace.LooksPlaintext(path)
}

func PasswordPolicyError(err error) error {
	return gatewayworkspace.PasswordPolicyError(err)
}

func UnsupportedSchemaMessage(err error) string {
	return gatewayworkspace.UnsupportedSchemaMessage(err)
}

func ValidatePassword(password string, confirmation string) error {
	return gatewayworkspace.ValidatePassword(password, confirmation)
}

func ValidateRemoteBackupPassword(password string, name string) error {
	return gatewayworkspace.ValidateRemoteBackupPassword(password, name)
}
