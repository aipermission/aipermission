package gatewayconnectormanagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type Transaction func(context.Context, func(*sql.Tx, AuditAppender) error) error

func adaptTransaction(transaction Transaction) func(context.Context, func(*sql.Tx, connectormanagement.AuditAppender) error) error {
	if transaction == nil {
		return nil
	}
	return func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
		return transaction(ctx, func(tx *sql.Tx, appendAudit AuditAppender) error {
			return mutate(tx, connectormanagement.AuditAppender(appendAudit))
		})
	}
}

type StoragePorts struct {
	Database         *sql.DB
	Registry         connectors.Catalog
	AcquireExclusive func(context.Context) (func(), error)
	Transaction      Transaction
	EncryptSecret    func(context.Context, int64, json.RawMessage) (string, error)
}

type CredentialPorts struct {
	Preparation        CredentialPreparationPorts
	Runtime            CredentialRuntimePorts
	SessionEnvironment func(context.Context, int64) bool
	BeforeCreate       func(context.Context, Target) error
	BeforeDelete       func(context.Context, Target, CredentialProfile) error
	SpecialTest        func(context.Context, connectors.TargetView, connectors.CredentialProfileView) (*connectors.ManagementResponse, error)
	RedactDetails      func(context.Context, map[string]any, CredentialBoundary) (map[string]any, error)
	ResourceRuntime    func(string) connectorapi.CredentialResourceRuntime
}

type LifecyclePorts struct {
	AfterChange    func(context.Context, TargetLifecycleChange) error
	DeleteTarget   func(context.Context, Target, map[string]any) error
	FinalizeTarget func(context.Context, Target, string) (int64, error)
}

type TargetAdapterPorts struct {
	DataRuntime      func(string) connectorapi.ConnectorDataRuntime
	LifecycleRuntime func(string) connectorapi.TargetLifecycleRuntime
	DeletionGateway  func(string, int64) connectorapi.TargetDeletionGateway
	OperationGateway func(string, int64) connectorapi.TargetOperationGateway
}

type NetworkPorts struct {
	Probe  func(context.Context, connectors.NetworkDialRequest) error
	Redact func(context.Context, string) string
}

type ObservationPorts struct {
	Observe       func(context.Context, string, any)
	AuditRequired func(context.Context, string, any) error
}

type Workspace struct {
	Storage     StoragePorts
	Credentials CredentialPorts
	Lifecycle   LifecyclePorts
	Network     NetworkPorts
	Observation ObservationPorts
	Adapters    TargetAdapterPorts
}

type CapabilityDependencies struct {
	LiveConsoleKind func(string) (string, bool)
	HasFileTransfer func(string) bool
	HasTCPTransport func(string) bool
}

type Dependencies struct {
	Active       func(http.ResponseWriter) (Workspace, bool)
	Approvals    ConnectorApprovalScopeProvider
	Capabilities CapabilityDependencies
	Adapters     connectorapi.Catalog
	PeerIdentity connectorapi.PeerIdentityGateway
}

type Component struct{ dependencies Dependencies }

func New(dependencies Dependencies) *Component { return &Component{dependencies: dependencies} }

type HTTPHandlers struct {
	Queries           *connectormanagement.HTTPHandlers
	Approvals         *connectorapproval.HTTPHandlers
	CombinedMutations *connectormanagement.CombinedMutationHTTPHandler
	TargetMutations   *connectormanagement.TargetMutationHTTPHandler
	HostPing          *connectormanagement.HostPingHTTPHandler
	ProfileMutations  *connectormanagement.ProfileMutationHTTPHandler
	ProfileProvision  *connectormanagement.ProvisioningHTTPHandler
	ProfileBackup     *connectormanagement.ProfileBackupHTTPHandler
	ProfileDelete     *connectormanagement.ProfileDeletionHTTPHandler
	ProfileTest       *connectormanagement.ProfileTestingHTTPHandler
	TargetDraft       *TargetDraftHTTPHandler
	TargetDelete      *TargetDeleteHTTPHandler
	TargetOperation   *TargetOperationHTTPHandler
}

func (component *Component) HTTPHandlers() HTTPHandlers {
	return HTTPHandlers{
		Queries:           connectormanagement.NewHTTPHandlers(component.QueryScope),
		Approvals:         connectorapproval.NewHTTPHandlers(component.approvalScope),
		CombinedMutations: connectormanagement.NewCombinedMutationHTTPHandler(component.CombinedMutationScope),
		TargetMutations:   connectormanagement.NewTargetMutationHTTPHandler(component.TargetMutationScope),
		HostPing:          connectormanagement.NewHostPingHTTPHandler(component.HostPingScope),
		ProfileMutations:  connectormanagement.NewProfileMutationHTTPHandler(component.ProfileMutationScope),
		ProfileProvision:  connectormanagement.NewProvisioningHTTPHandler(component.ProvisioningScope),
		ProfileBackup:     connectormanagement.NewProfileBackupHTTPHandler(component.ProfileBackupScope),
		ProfileDelete:     connectormanagement.NewProfileDeletionHTTPHandler(component.ProfileDeletionScope),
		ProfileTest:       connectormanagement.NewProfileTestingHTTPHandler(component.ProfileTestingScope),
		TargetDraft:       &TargetDraftHTTPHandler{component: component},
		TargetDelete:      &TargetDeleteHTTPHandler{component: component},
		TargetOperation:   &TargetOperationHTTPHandler{component: component},
	}
}

func (component *Component) RecoverProfileRestores(ctx context.Context, workspace Workspace) error {
	return connectormanagement.RecoverProfileRestores(ctx, adaptTransaction(workspace.Storage.Transaction))
}

func (component *Component) approvalScope(w http.ResponseWriter) (connectorapproval.Scope, bool) {
	if component == nil || component.dependencies.Approvals == nil {
		return connectorapproval.Scope{}, false
	}
	scope, ok := component.dependencies.Approvals(w)
	var workflow func() (connectorapproval.Workflow, error)
	if scope.Workflow != nil {
		workflow = func() (connectorapproval.Workflow, error) {
			value, err := scope.Workflow()
			if err != nil {
				return nil, err
			}
			return domainApprovalWorkflow{workflow: value}, nil
		}
	}
	return connectorapproval.Scope{
		Requests: scope.Requests, Workflow: workflow, MCPStarted: scope.MCPStarted, Redact: scope.Redact,
	}, ok
}

func (component *Component) active(w http.ResponseWriter) (Workspace, bool) {
	if component == nil || component.dependencies.Active == nil {
		return Workspace{}, false
	}
	return component.dependencies.Active(w)
}

func (component *Component) QueryScope(w http.ResponseWriter) (connectormanagement.Scope, bool) {
	workspace, ok := component.active(w)
	if !ok {
		return connectormanagement.Scope{}, false
	}
	return connectormanagement.Scope{
		Database: workspace.Storage.Database, Registry: workspace.Storage.Registry,
		Features: func(kind string) connectormanagement.ConnectorFeatures {
			features := connectormanagement.ConnectorFeatures{FileTransfer: component.dependencies.Capabilities.HasFileTransfer(kind)}
			if capability, ok := component.dependencies.Capabilities.LiveConsoleKind(kind); ok {
				features.LiveConsoleCapability = capability
			}
			return features
		},
		SessionEnvironmentSupported: func(ctx context.Context, runtimeID int64) bool {
			return workspace.Credentials.SessionEnvironment != nil && workspace.Credentials.SessionEnvironment(ctx, runtimeID)
		},
	}, true
}

func (component *Component) TargetMutationScope(w http.ResponseWriter) (connectormanagement.TargetMutationScope, bool) {
	workspace, ok := component.active(w)
	if !ok {
		return connectormanagement.TargetMutationScope{}, false
	}
	return component.targetMutation(workspace), true
}

func (component *Component) targetMutation(workspace Workspace) connectormanagement.TargetMutationScope {
	return connectormanagement.TargetMutationScope{
		Database: workspace.Storage.Database, Registry: workspace.Storage.Registry,
		ValidateTransport: func(ctx context.Context, projectID int64, config map[string]any) error {
			return ValidateTransport(ctx, connectortargets.NewStore(workspace.Storage.Database), projectID, config, component.dependencies.Capabilities.HasTCPTransport)
		},
		AcquireExclusive: workspace.Storage.AcquireExclusive,
		WithTransaction:  adaptTransaction(workspace.Storage.Transaction),
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return component.ensureRuntimeSurfaces(ctx, store, target, profile)
		},
		AfterLifecycleChange: func(ctx context.Context, change connectormanagement.TargetLifecycleChange) error {
			return workspace.Lifecycle.AfterChange(ctx, TargetLifecycleChange(change))
		},
	}
}

func (component *Component) ProfileMutationScope(w http.ResponseWriter) (connectormanagement.ProfileMutationScope, bool) {
	workspace, ok := component.active(w)
	if !ok {
		return connectormanagement.ProfileMutationScope{}, false
	}
	return component.profileMutation(workspace), true
}

func (component *Component) profileMutation(workspace Workspace) connectormanagement.ProfileMutationScope {
	return connectormanagement.ProfileMutationScope{
		Database: workspace.Storage.Database, Registry: workspace.Storage.Registry,
		Preparation: workspace.Credentials.Preparation.domain(), AcquireExclusive: workspace.Storage.AcquireExclusive,
		WithTransaction: adaptTransaction(workspace.Storage.Transaction),
		BeforeCreate: func(ctx context.Context, target connectortargets.Target) error {
			return workspace.Credentials.BeforeCreate(ctx, targetFromDomain(target))
		},
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return component.ensureRuntimeSurfaces(ctx, store, target, profile)
		},
		AfterLifecycleChange: func(ctx context.Context, change connectormanagement.TargetLifecycleChange) error {
			return workspace.Lifecycle.AfterChange(ctx, TargetLifecycleChange(change))
		},
	}
}

func (component *Component) CombinedMutationScope(w http.ResponseWriter) (connectormanagement.CombinedMutationScope, bool) {
	workspace, ok := component.active(w)
	if !ok {
		return connectormanagement.CombinedMutationScope{}, false
	}
	target, profile := component.targetMutation(workspace), component.profileMutation(workspace)
	return connectormanagement.CombinedMutationScope{
		Database: target.Database, Registry: target.Registry, Preparation: profile.Preparation,
		ValidateTransport: target.ValidateTransport, AcquireExclusive: target.AcquireExclusive,
		WithTransaction: profile.WithTransaction, BeforeCreate: profile.BeforeCreate,
		EnsureRuntimeSurfaces: profile.EnsureRuntimeSurfaces, AfterLifecycleChange: profile.AfterLifecycleChange,
	}, true
}

func (component *Component) ensureRuntimeSurfaces(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
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
	workspace, ok := component.active(w)
	if !ok {
		return connectormanagement.ProvisioningScope{}, false
	}
	return connectormanagement.ProvisioningScope{
		Database: workspace.Storage.Database, Registry: workspace.Storage.Registry, Runtime: workspace.Credentials.Runtime.domain(),
		EncryptSecret:   workspace.Storage.EncryptSecret,
		WithTransaction: adaptTransaction(workspace.Storage.Transaction),
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return component.ensureRuntimeSurfaces(ctx, store, target, profile)
		},
		AuditRequired: func(ctx context.Context, action string, payload any) error {
			return workspace.Observation.AuditRequired(ctx, action, payload)
		},
	}, true
}
