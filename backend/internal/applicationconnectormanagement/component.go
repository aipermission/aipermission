// Package applicationconnectormanagement composes connector target and
// credential management scopes around an active encrypted workspace.
package applicationconnectormanagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type Transaction func(context.Context, *workspaceruntime.Runtime, func(*sql.Tx, connectormanagement.AuditAppender) error) error

type Dependencies struct {
	ActiveRuntime      func(http.ResponseWriter) (*workspaceruntime.Runtime, bool)
	LiveConsoleKind    func(string) (string, bool)
	HasFileTransfer    func(string) bool
	HasTCPTransport    func(string) bool
	SessionEnvironment func(context.Context, *workspaceruntime.Runtime, int64) bool
	Preparation        func(*workspaceruntime.Runtime) connectormanagement.CredentialPreparationPorts
	CredentialRuntime  func(*workspaceruntime.Runtime) connectormanagement.CredentialRuntimePorts
	Transaction        Transaction
	AfterLifecycle     func(context.Context, *workspaceruntime.Runtime, connectormanagement.TargetLifecycleChange) error
	BeforeCreate       func(context.Context, *workspaceruntime.Runtime, connectortargets.Target) error
	BeforeDelete       func(context.Context, *workspaceruntime.Runtime, connectortargets.Target, connectortargets.CredentialProfile) error
	SpecialTest        func(http.ResponseWriter, *http.Request, *workspaceruntime.Runtime, connectors.TargetView, connectors.CredentialProfileView) bool
	RedactDetails      func(context.Context, *workspaceruntime.Runtime, map[string]any, connectormanagement.CredentialBoundary) (map[string]any, error)
	Probe              func(context.Context, *workspaceruntime.Runtime, connectors.NetworkDialRequest) error
	Redact             func(context.Context, *workspaceruntime.Runtime, string) string
	Observe            func(context.Context, *workspaceruntime.Runtime, string, any)
	AuditRequired      func(context.Context, *workspaceruntime.Runtime, string, any) error
}

type Component struct{ dependencies Dependencies }

func New(dependencies Dependencies) *Component { return &Component{dependencies: dependencies} }

func (component *Component) active(w http.ResponseWriter) (*workspaceruntime.Runtime, bool) {
	return component.dependencies.ActiveRuntime(w)
}

func (component *Component) QueryScope(w http.ResponseWriter) (connectormanagement.Scope, bool) {
	runtime, ok := component.active(w)
	if !ok {
		return connectormanagement.Scope{}, false
	}
	return connectormanagement.Scope{
		Database: runtime.Storage.Database, Registry: runtime.Connectors.ConnectorRegistry(),
		Features: func(kind string) connectormanagement.ConnectorFeatures {
			features := connectormanagement.ConnectorFeatures{FileTransfer: component.dependencies.HasFileTransfer(kind)}
			if capability, ok := component.dependencies.LiveConsoleKind(kind); ok {
				features.LiveConsoleCapability = capability
			}
			return features
		},
		SessionEnvironmentSupported: func(ctx context.Context, runtimeID int64) bool {
			return component.dependencies.SessionEnvironment(ctx, runtime, runtimeID)
		},
	}, true
}

func (component *Component) TargetMutationScope(w http.ResponseWriter) (connectormanagement.TargetMutationScope, bool) {
	runtime, ok := component.active(w)
	if !ok {
		return connectormanagement.TargetMutationScope{}, false
	}
	return component.targetMutation(runtime), true
}

func (component *Component) targetMutation(runtime *workspaceruntime.Runtime) connectormanagement.TargetMutationScope {
	return connectormanagement.TargetMutationScope{
		Database: runtime.Storage.Database, Registry: runtime.Connectors.ConnectorRegistry(),
		ValidateTransport: func(ctx context.Context, projectID int64, config map[string]any) error {
			return ValidateTransport(ctx, connectortargets.NewStore(runtime.Storage.Database), projectID, config, component.dependencies.HasTCPTransport)
		},
		AcquireExclusive: runtime.Security.VaultDelivery.AcquireExclusive,
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return component.dependencies.Transaction(ctx, runtime, mutate)
		},
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return component.ensureRuntimeSurfaces(ctx, store, target, profile)
		},
		AfterLifecycleChange: func(ctx context.Context, change connectormanagement.TargetLifecycleChange) error {
			return component.dependencies.AfterLifecycle(ctx, runtime, change)
		},
	}
}

func (component *Component) ProfileMutationScope(w http.ResponseWriter) (connectormanagement.ProfileMutationScope, bool) {
	runtime, ok := component.active(w)
	if !ok {
		return connectormanagement.ProfileMutationScope{}, false
	}
	return component.profileMutation(runtime), true
}

func (component *Component) profileMutation(runtime *workspaceruntime.Runtime) connectormanagement.ProfileMutationScope {
	return connectormanagement.ProfileMutationScope{
		Database: runtime.Storage.Database, Registry: runtime.Connectors.ConnectorRegistry(),
		Preparation: component.dependencies.Preparation(runtime), AcquireExclusive: runtime.Security.VaultDelivery.AcquireExclusive,
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return component.dependencies.Transaction(ctx, runtime, mutate)
		},
		BeforeCreate: func(ctx context.Context, target connectortargets.Target) error {
			return component.dependencies.BeforeCreate(ctx, runtime, target)
		},
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return component.ensureRuntimeSurfaces(ctx, store, target, profile)
		},
		AfterLifecycleChange: func(ctx context.Context, change connectormanagement.TargetLifecycleChange) error {
			return component.dependencies.AfterLifecycle(ctx, runtime, change)
		},
	}
}

func (component *Component) CombinedMutationScope(w http.ResponseWriter) (connectormanagement.CombinedMutationScope, bool) {
	runtime, ok := component.active(w)
	if !ok {
		return connectormanagement.CombinedMutationScope{}, false
	}
	target, profile := component.targetMutation(runtime), component.profileMutation(runtime)
	return connectormanagement.CombinedMutationScope{
		Database: target.Database, Registry: target.Registry, Preparation: profile.Preparation,
		ValidateTransport: target.ValidateTransport, AcquireExclusive: target.AcquireExclusive,
		WithTransaction: profile.WithTransaction, BeforeCreate: profile.BeforeCreate,
		EnsureRuntimeSurfaces: profile.EnsureRuntimeSurfaces, AfterLifecycleChange: profile.AfterLifecycleChange,
	}, true
}

func (component *Component) ensureRuntimeSurfaces(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
	capabilities := []string{}
	if capability, ok := component.dependencies.LiveConsoleKind(target.ConnectorKind); ok {
		capabilities = append(capabilities, capability)
	}
	if component.dependencies.HasFileTransfer(target.ConnectorKind) {
		capabilities = append(capabilities, connectortargets.RuntimeCapabilityFileTransfer)
	}
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		if _, err := store.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
			ConnectorKind: target.ConnectorKind, TargetID: target.ID, ProfileID: profile.ID,
			CapabilityKind: capability, Label: profile.Label,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (component *Component) ProvisioningScope(w http.ResponseWriter) (connectormanagement.ProvisioningScope, bool) {
	runtime, ok := component.active(w)
	if !ok {
		return connectormanagement.ProvisioningScope{}, false
	}
	return connectormanagement.ProvisioningScope{
		Database: runtime.Storage.Database, Registry: runtime.Connectors.ConnectorRegistry(), Runtime: component.dependencies.CredentialRuntime(runtime),
		EncryptSecret: func(_ context.Context, id int64, secret json.RawMessage) (string, error) {
			return recordcrypto.EncryptJSON(runtime.Storage.Vault, runtime.WorkspaceUUID, recordcrypto.ConnectorCredentialProfile, id, secret)
		},
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return component.dependencies.Transaction(ctx, runtime, mutate)
		},
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return component.ensureRuntimeSurfaces(ctx, store, target, profile)
		},
		AuditRequired: func(ctx context.Context, action string, payload any) error {
			return component.dependencies.AuditRequired(ctx, runtime, action, payload)
		},
	}, true
}
