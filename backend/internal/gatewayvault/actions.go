package gatewayvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultactions"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type SessionEnvironmentErrorKind uint8

const (
	SessionEnvironmentValidation SessionEnvironmentErrorKind = iota + 1
	SessionEnvironmentNotFound
	SessionEnvironmentStale
)

func ClassifySessionEnvironmentError(err error) (SessionEnvironmentErrorKind, string, bool) {
	var validation projectvault.ValidationError
	switch {
	case errors.As(err, &validation):
		return SessionEnvironmentValidation, validation.Error(), true
	case errors.Is(err, projectvault.ErrNotFound):
		return SessionEnvironmentNotFound, "vault item not found", true
	case errors.Is(err, projectvault.ErrStale):
		return SessionEnvironmentStale, err.Error(), true
	default:
		return 0, "", false
	}
}

type actionProjectPort struct{ database *sql.DB }

type actionTokenPort struct {
	read func(context.Context, int64) (TokenState, error)
}

func (port actionTokenPort) Get(ctx context.Context, id int64) (vaultactions.TokenState, error) {
	token, err := port.read(ctx, id)
	return vaultactions.TokenState{
		Active: token.Active, ExpiresAt: token.ExpiresAt, UpdatedAt: token.UpdatedAt,
	}, err
}

type SessionEnvironment interface {
	Len() int
	ForEach(func(name string, value []byte, replaceExisting bool, itemID int64, valueVersion int64, sourceProjectID int64) error) error
}

type EnvironmentSessionHandle struct {
	ID         int64
	RuntimeID  int64
	Generation int64
}

type EnvironmentPreparation struct {
	Environment  SessionEnvironment
	Release      func()
	PostValidate func(context.Context) error
	Finalize     func(context.Context, EnvironmentSessionHandle) error
}

type EnvironmentPreparer func(context.Context, string) (EnvironmentPreparation, error)

type EnvironmentItem struct {
	ItemID int64
}

type EnvironmentPlan struct {
	Items                  []EnvironmentItem
	EnvironmentContentHash string
	Prepare                EnvironmentPreparer
}

type VaultActionApplication interface {
	BuildEnvironmentPlan(context.Context, int64, []SessionSelection) (EnvironmentPlan, error)
	Prepare(context.Context, int64, string, string, map[string]any) (vaultrequests.PreparedAction, error)
	AuthorizeOutput(context.Context, vaultrequests.Request) bool
	ValidateAuthorization(context.Context, vaultrequests.Request, vaultrequests.ApprovalContext) error
	Execute(context.Context, vaultrequests.Request) (any, error)
	Compensate(context.Context, vaultrequests.Request, any) error
	IsStale(error) bool
}

type vaultActionApplication struct{ *vaultactions.Runtime }

func (application vaultActionApplication) BuildEnvironmentPlan(ctx context.Context, runtimeID int64, selections []SessionSelection) (EnvironmentPlan, error) {
	items := make([]projectvault.SessionSelection, len(selections))
	for index, selection := range selections {
		items[index] = projectvault.SessionSelection{
			ItemID: selection.ItemID, SourceProjectID: selection.SourceProjectID,
			ReplaceExisting: selection.ReplaceExisting, BindingID: selection.BindingID,
			BindingRevision: selection.BindingRevision,
		}
	}
	plan, err := application.Runtime.BuildEnvironmentPlan(ctx, runtimeID, items)
	if err != nil {
		return EnvironmentPlan{}, err
	}
	result := EnvironmentPlan{
		Items:                  make([]EnvironmentItem, 0, len(plan.Items)),
		EnvironmentContentHash: plan.EnvironmentContentHash,
	}
	for _, item := range plan.Items {
		result.Items = append(result.Items, EnvironmentItem{ItemID: item.ItemID})
	}
	if plan.Prepare != nil {
		result.Prepare = func(prepareCtx context.Context, peerIdentity string) (EnvironmentPreparation, error) {
			prepared, err := plan.Prepare(prepareCtx, peerIdentity)
			if err != nil {
				return EnvironmentPreparation{}, err
			}
			var finalize func(context.Context, EnvironmentSessionHandle) error
			if prepared.Finalize != nil {
				finalize = func(finalizeCtx context.Context, handle EnvironmentSessionHandle) error {
					return prepared.Finalize(finalizeCtx, vaultactions.EnvironmentSessionHandle{
						ID: handle.ID, RuntimeID: handle.RuntimeID, Generation: handle.Generation,
					})
				}
			}
			return EnvironmentPreparation{
				Environment: prepared.Environment, Release: prepared.Release,
				PostValidate: prepared.PostValidate, Finalize: finalize,
			}, nil
		}
	}
	return result, nil
}

func (port actionProjectPort) ResolveRef(ctx context.Context, ref string) (vaultactions.Project, bool, error) {
	project, err := projectstore.NewStore(port.database).ResolveRef(ctx, ref)
	if errors.Is(err, projectstore.ErrNotFound) {
		return vaultactions.Project{}, false, nil
	}
	if err != nil {
		return vaultactions.Project{}, false, err
	}
	return vaultactions.Project{ID: project.ID, Name: project.Name, Slug: project.Slug}, true, nil
}

func (port actionProjectPort) TokenCanAccess(ctx context.Context, tokenID, projectID int64) (bool, error) {
	return projectstore.NewStore(port.database).TokenCanAccessProject(ctx, tokenID, projectID)
}

type actionItemPort struct{ store *projectvault.Store }

func (port actionItemPort) SnapshotSession(ctx context.Context, items []projectvault.SessionSelection) (projectvault.SessionResolution, error) {
	return port.store.SnapshotSession(ctx, items)
}
func (port actionItemPort) ResolveSession(ctx context.Context, items []projectvault.SessionSelection) (projectvault.SessionResolution, error) {
	return port.store.ResolveSession(ctx, items)
}
func (port actionItemPort) RevalidateSession(ctx context.Context, items []projectvault.SessionItem) error {
	return port.store.RevalidateSession(ctx, items)
}
func (port actionItemPort) RecordSessionItems(ctx context.Context, id int64, items []projectvault.SessionItem) error {
	return port.store.RecordSessionItems(ctx, id, items)
}
func (port actionItemPort) MarkSessionItemsUsed(ctx context.Context, items []projectvault.SessionItem) error {
	return port.store.MarkSessionItemsUsed(ctx, items)
}
func (port actionItemPort) Create(ctx context.Context, tx *sql.Tx, input projectvault.CreateInput) (projectvault.Item, error) {
	return port.store.WithTx(tx).Create(ctx, input)
}
func (port actionItemPort) Delete(ctx context.Context, id, valueVersion, metadataRevision int64) error {
	return port.store.Delete(ctx, id, valueVersion, metadataRevision)
}

type actionDeliveryPort struct{ runtime Runtime }

type actionConnectorPort struct{ delegate ConnectorPort }

func (port actionConnectorPort) SessionEnvironmentVersion(ctx context.Context, runtimeID int64) (string, error) {
	return port.delegate.SessionEnvironmentVersion(ctx, runtimeID)
}

func (port actionConnectorPort) LiveConsolePermission(ctx context.Context, tokenID, targetID, profileID int64, kind string) (connectortargets.ActionPermission, string, error) {
	permission, action, err := port.delegate.LiveConsolePermission(ctx, tokenID, targetID, profileID, kind)
	if err != nil {
		return connectortargets.ActionPermission{}, "", err
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return connectortargets.ActionPermission{}, "", errors.New("this connector has an invalid live console action")
	}
	rule := connectortargets.ActionPermissionRule(permission.ExecutionRule)
	if rule != connectortargets.ActionPermissionAlwaysRun && rule != connectortargets.ActionPermissionApprovalRequired {
		return connectortargets.ActionPermission{}, "", errors.New("Vault session apply requires an active Prompt or Always connector action permission")
	}
	return connectortargets.ActionPermission{ExecutionRule: rule, ExpiresAt: permission.ExpiresAt, UpdatedAt: permission.UpdatedAt}, action, nil
}

func (port actionConnectorPort) ExpectedPeerIdentities(ctx context.Context, surface connectortargets.RuntimeSurface) (vaultactions.PeerIdentityExpectation, error) {
	expectation, err := port.delegate.ExpectedPeerIdentities(ctx, ConnectorRuntimeSurface{ID: surface.ID, ConnectorKind: surface.ConnectorKind})
	return vaultactions.PeerIdentityExpectation{Items: expectation.Items, Required: expectation.Required}, err
}

func (port actionDeliveryPort) AcquireDelivery(ctx context.Context) (func(), error) {
	if port.runtime.Session.AcquireDelivery == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	return port.runtime.Session.AcquireDelivery(ctx)
}
func (port actionDeliveryPort) AcquireExclusive(ctx context.Context) (func(), error) {
	if port.runtime.Session.AcquireExclusive == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	return port.runtime.Session.AcquireExclusive(ctx)
}

type actionMutationPort struct {
	component *Component
	runtime   Runtime
}

func (port actionMutationPort) WithMutation(ctx context.Context, tokenID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	if port.component == nil || port.runtime.Action.Mutate == nil {
		return vaultactions.ErrRuntimeUnavailable
	}
	return port.runtime.Action.Mutate(ctx, tokenID, action, payload, mutate)
}

func (component *Component) ActionRuntime(runtime Runtime) (VaultActionApplication, error) {
	if component == nil || runtime.Storage.Database == nil || runtime.Storage.SecretVault == nil || runtime.Storage.ReadToken == nil || runtime.Session.Sessions == nil ||
		runtime.Session.Leases == nil || runtime.Action.Connector == nil || runtime.Session.MCPStarted == nil ||
		runtime.Storage.DatabaseID == "" || component.dependencies.AllowGenerate == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	items, err := projectvault.NewStore(runtime.Storage.Database, runtime.Storage.SecretVault, runtime.Storage.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("initialize Vault action item store: %w", err)
	}
	owner, err := vaultactions.NewRuntime(vaultactions.Dependencies{
		Database: runtime.Storage.Database, Tokens: actionTokenPort{read: runtime.Storage.ReadToken},
		Projects: actionProjectPort{database: runtime.Storage.Database}, SessionItems: actionItemPort{store: items},
		ItemMutations: actionItemPort{store: items}, Sessions: runtime.Session.Sessions,
		Leases: runtime.Session.Leases, PersistedLeases: vaultsessions.NewPersistence(runtime.Storage.Database),
		Connector: actionConnectorPort{delegate: runtime.Action.Connector}, Delivery: actionDeliveryPort{runtime: runtime},
		Mutations: actionMutationPort{component: component, runtime: runtime}, WorkspaceID: runtime.Storage.WorkspaceID,
		RuntimeInstanceID: runtime.Session.RuntimeInstanceID, MCPStarted: runtime.Session.MCPStarted,
		AllowGenerate: func(tokenID int64) bool {
			return component.dependencies.AllowGenerate(fmt.Sprintf("vault-generate:%s:%d", runtime.Storage.DatabaseID, tokenID))
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Vault action application: %w", err)
	}
	return vaultActionApplication{Runtime: owner}, nil
}
