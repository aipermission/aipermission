// Package connectorruntime implements gateway-owned, connector-scoped runtime
// ports without exposing database or Vault authority to connector adapters.
package connectorruntime

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectorresources"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

var ErrInvalidRuntime = errors.New("invalid connector runtime")

type SecretAccessorFactory func(map[string]any) connectors.SecretAccessor

type ResourceScopes interface {
	Scope(connectorKind, resourceKind string) connectorapi.CredentialResourceStore
}

type Dependencies struct {
	Database        *sql.DB
	Vault           *vault.Vault
	WorkspaceID     string
	Resources       ResourceScopes
	ConsoleSessions *console.Manager
	SecretAccessor  SecretAccessorFactory
}

// Scope owns the raw core dependencies for one connector kind. It is retained
// by the gateway composition root and creates least-authority adapter ports.
type Scope struct {
	kind            string
	database        *sql.DB
	vault           *vault.Vault
	workspaceID     string
	resources       ResourceScopes
	consoleSessions *console.Manager
	secretAccessor  SecretAccessorFactory
}

func NewScope(kind string, dependencies Dependencies) *Scope {
	return &Scope{
		kind:            strings.TrimSpace(kind),
		database:        dependencies.Database,
		vault:           dependencies.Vault,
		workspaceID:     strings.TrimSpace(dependencies.WorkspaceID),
		resources:       dependencies.Resources,
		consoleSessions: dependencies.ConsoleSessions,
		secretAccessor:  dependencies.SecretAccessor,
	}
}

func NewResourceScopes(database *sql.DB, secretVault *vault.Vault, workspaceID string) ResourceScopes {
	return connectorresources.NewStore(database, secretVault, workspaceID)
}

func (s *Scope) store() (*connectortargets.Store, error) {
	if s == nil || s.database == nil || s.kind == "" {
		return nil, ErrInvalidRuntime
	}
	return connectortargets.NewStore(s.database), nil
}

func (s *Scope) requireTarget(ctx context.Context, targetID int64) error {
	store, err := s.store()
	if err != nil {
		return err
	}
	target, err := store.GetTarget(ctx, targetID)
	if err != nil {
		return err
	}
	if target.ConnectorKind != s.kind {
		return connectortargets.ErrTargetNotFound
	}
	return nil
}

func (s *Scope) resolveTarget(ctx context.Context, targetRef string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	store, err := s.store()
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	target, profile, err := store.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	if target.ConnectorKind != s.kind || profile.ConnectorKind != s.kind {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, connectortargets.ErrInvalidTargetRef
	}
	return target, profile, nil
}

func (s *Scope) ensureSurface(ctx context.Context, input connectortargets.EnsureRuntimeSurfaceInput) (connectortargets.RuntimeSurface, error) {
	if input.ConnectorKind != "" && input.ConnectorKind != s.kind {
		return connectortargets.RuntimeSurface{}, connectortargets.ErrRuntimeSurfaceNotFound
	}
	if err := s.requireTarget(ctx, input.TargetID); err != nil {
		return connectortargets.RuntimeSurface{}, err
	}
	input.ConnectorKind = s.kind
	store, _ := s.store()
	return store.EnsureRuntimeSurface(ctx, input)
}

func (s *Scope) listSurfaces(ctx context.Context, targetID int64, profileID int64, capabilityKind string) ([]connectortargets.RuntimeSurface, error) {
	if err := s.requireTarget(ctx, targetID); err != nil {
		return nil, err
	}
	store, _ := s.store()
	surfaces, err := store.ListRuntimeSurfacesForProfile(ctx, targetID, profileID, capabilityKind)
	if err != nil {
		return nil, err
	}
	result := make([]connectortargets.RuntimeSurface, 0, len(surfaces))
	for _, surface := range surfaces {
		if surface.ConnectorKind == s.kind {
			result = append(result, surface)
		}
	}
	return result, nil
}

func (s *Scope) targetProfileByRuntimeID(ctx context.Context, runtimeID int64) (connectors.TargetView, connectors.CredentialProfileView, connectortargets.RuntimeSurface, error) {
	store, err := s.store()
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, connectortargets.RuntimeSurface{}, err
	}
	target, profile, surface, err := store.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, connectortargets.RuntimeSurface{}, err
	}
	if target.ConnectorKind != s.kind || profile.ConnectorKind != s.kind || surface.ConnectorKind != s.kind {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, connectortargets.RuntimeSurface{}, connectortargets.ErrRuntimeSurfaceNotFound
	}
	return target, profile, surface, nil
}

func (s *Scope) listProfiles(ctx context.Context, targetID int64) ([]connectors.CredentialProfileView, error) {
	if err := s.requireTarget(ctx, targetID); err != nil {
		return nil, err
	}
	store, _ := s.store()
	profiles, err := store.ListCredentialProfiles(ctx, targetID)
	if err != nil {
		return nil, err
	}
	result := make([]connectors.CredentialProfileView, 0, len(profiles))
	for _, profile := range profiles {
		result = append(result, connectortargets.CredentialProfileView(profile))
	}
	return result, nil
}

func (s *Scope) resourcesFor(resourceKind string) connectorapi.CredentialResourceStore {
	if s == nil || s.resources == nil || s.kind == "" || strings.TrimSpace(resourceKind) == "" {
		return nil
	}
	return s.resources.Scope(s.kind, resourceKind)
}

func (s *Scope) managerFor(ctx context.Context, runtimeID int64) (*console.Manager, error) {
	if _, _, _, err := s.targetProfileByRuntimeID(ctx, runtimeID); err != nil {
		return nil, err
	}
	if s.consoleSessions == nil {
		return nil, ErrInvalidRuntime
	}
	return s.consoleSessions, nil
}

func (s *Scope) resolveRuntimeContext(ctx context.Context, runtimeID int64, capabilityKind string) (connectors.RuntimeContext, connectortargets.RuntimeSurface, error) {
	target, profile, surface, err := s.targetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return connectors.RuntimeContext{}, connectortargets.RuntimeSurface{}, err
	}
	if surface.CapabilityKind != strings.TrimSpace(capabilityKind) {
		return connectors.RuntimeContext{}, connectortargets.RuntimeSurface{}, connectortargets.ErrRuntimeSurfaceNotFound
	}
	store, _ := s.store()
	storedProfile, err := store.GetCredentialProfile(ctx, surface.TargetID, surface.ProfileID)
	if err != nil {
		return connectors.RuntimeContext{}, connectortargets.RuntimeSurface{}, err
	}
	secrets := map[string]any{}
	if storedProfile.EncryptedSecretJSON != "" {
		if s.vault == nil || s.workspaceID == "" {
			return connectors.RuntimeContext{}, connectortargets.RuntimeSurface{}, ErrInvalidRuntime
		}
		if err := recordcrypto.DecryptJSON(s.vault, s.workspaceID, recordcrypto.ConnectorCredentialProfile, storedProfile.ID, storedProfile.EncryptedSecretJSON, &secrets); err != nil {
			return connectors.RuntimeContext{}, connectortargets.RuntimeSurface{}, err
		}
	}
	if s.secretAccessor == nil {
		return connectors.RuntimeContext{}, connectortargets.RuntimeSurface{}, ErrInvalidRuntime
	}
	return connectors.RuntimeContext{Target: target, Profile: profile, Secrets: s.secretAccessor(secrets), Events: noopEventSink{}}, surface, nil
}

func (s *Scope) DataRuntime() connectorapi.ConnectorDataRuntime {
	return dataRuntime{scope: s}
}

func (s *Scope) LiveConsoleRuntime() connectorapi.LiveConsoleRuntime {
	return dataRuntime{scope: s}
}

func (s *Scope) ActionRuntime() connectorapi.ActionRuntime {
	return actionRuntime{liveRuntime: liveRuntime{dataRuntime: dataRuntime{scope: s}}}
}

func (s *Scope) TransferRuntime() connectorapi.TransferRuntime {
	return transferRuntime{dataRuntime: dataRuntime{scope: s}}
}

func (s *Scope) RequireRuntimeID(ctx context.Context, runtimeID int64) error {
	_, _, _, err := s.targetProfileByRuntimeID(ctx, runtimeID)
	return err
}

func (s *Scope) RequireTargetRuntimeID(ctx context.Context, targetID int64, runtimeID int64) error {
	_, _, surface, err := s.targetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return err
	}
	if surface.TargetID != targetID {
		return connectortargets.ErrRuntimeSurfaceNotFound
	}
	return nil
}

type dataRuntime struct{ scope *Scope }

func (r dataRuntime) ResolveConnectorActionTarget(ctx context.Context, targetRef string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	return r.scope.resolveTarget(ctx, targetRef)
}

func (r dataRuntime) EnsureRuntimeSurface(ctx context.Context, input connectortargets.EnsureRuntimeSurfaceInput) (connectortargets.RuntimeSurface, error) {
	return r.scope.ensureSurface(ctx, input)
}

func (r dataRuntime) ListRuntimeSurfacesForProfile(ctx context.Context, targetID int64, profileID int64, capabilityKind string) ([]connectortargets.RuntimeSurface, error) {
	return r.scope.listSurfaces(ctx, targetID, profileID, capabilityKind)
}

func (r dataRuntime) TargetProfileByRuntimeID(ctx context.Context, runtimeID int64) (connectors.TargetView, connectors.CredentialProfileView, connectortargets.RuntimeSurface, error) {
	return r.scope.targetProfileByRuntimeID(ctx, runtimeID)
}

func (r dataRuntime) ListCredentialProfiles(ctx context.Context, targetID int64) ([]connectors.CredentialProfileView, error) {
	return r.scope.listProfiles(ctx, targetID)
}

func (r dataRuntime) CredentialResources(resourceKind string) connectorapi.CredentialResourceStore {
	return r.scope.resourcesFor(resourceKind)
}

type liveRuntime struct{ dataRuntime }

func (r liveRuntime) ConnectorConsoleSessions() connectorapi.ConsoleSessionRuntime {
	return sessionRuntime{scope: r.scope}
}

type actionRuntime struct{ liveRuntime }

type transferRuntime struct{ dataRuntime }

func (r transferRuntime) ResolveRuntimeContext(ctx context.Context, runtimeID int64, capabilityKind string) (connectors.RuntimeContext, connectortargets.RuntimeSurface, error) {
	return r.scope.resolveRuntimeContext(ctx, runtimeID, capabilityKind)
}

type sessionRuntime struct{ scope *Scope }

func (r sessionRuntime) EnsureReady(ctx context.Context, principal executionprincipal.Principal, runtimeID int64) (console.SessionHandle, error) {
	manager, err := r.scope.managerFor(ctx, runtimeID)
	if err != nil {
		return console.SessionHandle{}, err
	}
	return manager.EnsureReady(ctx, principal, runtimeID)
}

func (r sessionRuntime) Exec(ctx context.Context, principal executionprincipal.Principal, runtimeID int64, command string) (console.ExecResult, error) {
	manager, err := r.scope.managerFor(ctx, runtimeID)
	if err != nil {
		return console.ExecResult{}, err
	}
	return manager.Exec(ctx, principal, runtimeID, command)
}

func (r sessionRuntime) ActiveSnapshot(ctx context.Context, principal executionprincipal.Principal, runtimeID int64) (console.Record, error) {
	manager, err := r.scope.managerFor(ctx, runtimeID)
	if err != nil {
		return console.Record{}, err
	}
	return manager.ActiveSnapshot(ctx, principal, runtimeID)
}

func (r sessionRuntime) WaitActive(ctx context.Context, principal executionprincipal.Principal, handle console.SessionHandle) (console.ExecResult, error) {
	manager, err := r.scope.managerFor(ctx, handle.RuntimeID)
	if err != nil {
		return console.ExecResult{}, err
	}
	return manager.WaitActive(ctx, principal, handle)
}

func (r sessionRuntime) InterruptActive(ctx context.Context, principal executionprincipal.Principal, handle console.SessionHandle) error {
	manager, err := r.scope.managerFor(ctx, handle.RuntimeID)
	if err != nil {
		return err
	}
	return manager.InterruptActive(ctx, principal, handle)
}

type noopEventSink struct{}

func (noopEventSink) Emit(context.Context, connectors.ActionEvent) error { return nil }

var _ connectorapi.ConnectorDataRuntime = dataRuntime{}
var _ connectorapi.LiveConsoleRuntime = dataRuntime{}
var _ connectorapi.ActionRuntime = actionRuntime{}
var _ connectorapi.TransferRuntime = transferRuntime{}
var _ connectorapi.ConsoleSessionRuntime = sessionRuntime{}
