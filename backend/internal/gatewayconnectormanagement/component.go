package gatewayconnectormanagement

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

type Transaction func(context.Context, workspaceruntime.Port, func(*sql.Tx, connectormanagement.AuditAppender) error) error

type RuntimeDependencies struct {
	Active      func(http.ResponseWriter) (workspaceruntime.Port, bool)
	Transaction Transaction
}

type CapabilityDependencies struct {
	LiveConsoleKind    func(string) (string, bool)
	HasFileTransfer    func(string) bool
	HasTCPTransport    func(string) bool
	SessionEnvironment func(context.Context, workspaceruntime.Port, int64) bool
}

type CredentialDependencies struct {
	Preparation   func(workspaceruntime.Port) connectormanagement.CredentialPreparationPorts
	Runtime       func(workspaceruntime.Port) connectormanagement.CredentialRuntimePorts
	BeforeCreate  func(context.Context, workspaceruntime.Port, connectortargets.Target) error
	BeforeDelete  func(context.Context, workspaceruntime.Port, connectortargets.Target, connectortargets.CredentialProfile) error
	SpecialTest   func(http.ResponseWriter, *http.Request, workspaceruntime.Port, connectors.TargetView, connectors.CredentialProfileView) bool
	RedactDetails func(context.Context, workspaceruntime.Port, map[string]any, connectormanagement.CredentialBoundary) (map[string]any, error)
}

type LifecycleDependencies struct {
	AfterChange func(context.Context, workspaceruntime.Port, connectormanagement.TargetLifecycleChange) error
}

type NetworkDependencies struct {
	Probe  func(context.Context, workspaceruntime.Port, connectors.NetworkDialRequest) error
	Redact func(context.Context, workspaceruntime.Port, string) string
}

type ObservationDependencies struct {
	Observe       func(context.Context, workspaceruntime.Port, string, any)
	AuditRequired func(context.Context, workspaceruntime.Port, string, any) error
}

type Dependencies struct {
	Runtime      RuntimeDependencies
	Capabilities CapabilityDependencies
	Credentials  CredentialDependencies
	Lifecycle    LifecycleDependencies
	Network      NetworkDependencies
	Observation  ObservationDependencies
}

type Component struct{ dependencies Dependencies }

func New(dependencies Dependencies) *Component { return &Component{dependencies: dependencies} }

func (component *Component) active(w http.ResponseWriter) (workspaceruntime.Port, bool) {
	return component.dependencies.Runtime.Active(w)
}

func (component *Component) QueryScope(w http.ResponseWriter) (connectormanagement.Scope, bool) {
	runtime, ok := component.active(w)
	if !ok {
		return connectormanagement.Scope{}, false
	}
	return connectormanagement.Scope{
		Database: runtime.StoragePort().DatabaseHandle(), Registry: runtime.ConnectorPort().ConnectorRegistry(),
		Features: func(kind string) connectormanagement.ConnectorFeatures {
			features := connectormanagement.ConnectorFeatures{FileTransfer: component.dependencies.Capabilities.HasFileTransfer(kind)}
			if capability, ok := component.dependencies.Capabilities.LiveConsoleKind(kind); ok {
				features.LiveConsoleCapability = capability
			}
			return features
		},
		SessionEnvironmentSupported: func(ctx context.Context, runtimeID int64) bool {
			return component.dependencies.Capabilities.SessionEnvironment(ctx, runtime, runtimeID)
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

func (component *Component) targetMutation(runtime workspaceruntime.Port) connectormanagement.TargetMutationScope {
	return connectormanagement.TargetMutationScope{
		Database: runtime.StoragePort().DatabaseHandle(), Registry: runtime.ConnectorPort().ConnectorRegistry(),
		ValidateTransport: func(ctx context.Context, projectID int64, config map[string]any) error {
			return ValidateTransport(ctx, connectortargets.NewStore(runtime.StoragePort().DatabaseHandle()), projectID, config, component.dependencies.Capabilities.HasTCPTransport)
		},
		AcquireExclusive: runtime.SecurityPort().VaultDeliveryCoordinator().AcquireExclusive,
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return component.dependencies.Runtime.Transaction(ctx, runtime, mutate)
		},
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return component.EnsureRuntimeSurfaces(ctx, store, target, profile)
		},
		AfterLifecycleChange: func(ctx context.Context, change connectormanagement.TargetLifecycleChange) error {
			return component.dependencies.Lifecycle.AfterChange(ctx, runtime, change)
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

func (component *Component) profileMutation(runtime workspaceruntime.Port) connectormanagement.ProfileMutationScope {
	return connectormanagement.ProfileMutationScope{
		Database: runtime.StoragePort().DatabaseHandle(), Registry: runtime.ConnectorPort().ConnectorRegistry(),
		Preparation: component.dependencies.Credentials.Preparation(runtime), AcquireExclusive: runtime.SecurityPort().VaultDeliveryCoordinator().AcquireExclusive,
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return component.dependencies.Runtime.Transaction(ctx, runtime, mutate)
		},
		BeforeCreate: func(ctx context.Context, target connectortargets.Target) error {
			return component.dependencies.Credentials.BeforeCreate(ctx, runtime, target)
		},
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return component.EnsureRuntimeSurfaces(ctx, store, target, profile)
		},
		AfterLifecycleChange: func(ctx context.Context, change connectormanagement.TargetLifecycleChange) error {
			return component.dependencies.Lifecycle.AfterChange(ctx, runtime, change)
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

func (component *Component) EnsureRuntimeSurfaces(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
	if store == nil {
		return nil
	}
	capabilities := []string{}
	if capability, ok := component.dependencies.Capabilities.LiveConsoleKind(target.ConnectorKind); ok {
		capabilities = append(capabilities, capability)
	}
	if component.dependencies.Capabilities.HasFileTransfer(target.ConnectorKind) {
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
		Database: runtime.StoragePort().DatabaseHandle(), Registry: runtime.ConnectorPort().ConnectorRegistry(), Runtime: component.dependencies.Credentials.Runtime(runtime),
		EncryptSecret: func(_ context.Context, id int64, secret json.RawMessage) (string, error) {
			return recordcrypto.EncryptJSON(runtime.StoragePort().SecretVault(), runtime.WorkspaceIdentifier(), recordcrypto.ConnectorCredentialProfile, id, secret)
		},
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return component.dependencies.Runtime.Transaction(ctx, runtime, mutate)
		},
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return component.EnsureRuntimeSurfaces(ctx, store, target, profile)
		},
		AuditRequired: func(ctx context.Context, action string, payload any) error {
			return component.dependencies.Observation.AuditRequired(ctx, runtime, action, payload)
		},
	}, true
}
