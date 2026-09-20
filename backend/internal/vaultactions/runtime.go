package vaultactions

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

var ErrRuntimeUnavailable = errors.New("Vault action application runtime is unavailable")

type staleContextError struct{ message string }

func (e staleContextError) Error() string { return e.message }

func staleContext(message string) error { return staleContextError{message: message} }

func IsStale(err error) bool {
	var target staleContextError
	return errors.As(err, &target)
}

type ConnectorPort interface {
	SessionEnvironmentVersion(context.Context, int64) (string, error)
	LiveConsolePermission(context.Context, int64, int64, int64, string) (connectortargets.ActionPermission, string, error)
	ExpectedPeerIdentities(context.Context, connectortargets.RuntimeSurface) (PeerIdentityExpectation, error)
}

type PeerIdentityExpectation struct {
	Items    []string
	Required bool
}

type DeliveryGate interface {
	AcquireDelivery(context.Context) (func(), error)
	AcquireExclusive(context.Context) (func(), error)
	WithAdmission(context.Context) context.Context
}

type Project struct {
	ID   int64
	Name string
	Slug string
}

type ProjectPort interface {
	ResolveRef(context.Context, string) (Project, bool, error)
	TokenCanAccess(context.Context, int64, int64) (bool, error)
}

type SessionItemPort interface {
	SnapshotSession(context.Context, []projectvault.SessionSelection) (projectvault.SessionResolution, error)
	ResolveSession(context.Context, []projectvault.SessionSelection) (projectvault.SessionResolution, error)
	RevalidateSession(context.Context, []projectvault.SessionItem) error
	RecordSessionItems(context.Context, int64, []projectvault.SessionItem) error
	MarkSessionItemsUsed(context.Context, []projectvault.SessionItem) error
}

type ItemMutationPort interface {
	Create(context.Context, *sql.Tx, projectvault.CreateInput) (projectvault.Item, error)
	Delete(context.Context, int64, int64, int64) error
}

type SessionPort interface {
	ActiveRecord(context.Context, int64) (console.Record, error)
	// ReplaceIfCurrent consumes every request-owned sensitive resource and
	// startup admission release callback, including on error.
	ReplaceIfCurrent(context.Context, executionprincipal.Principal, console.SessionHandle, console.CreateRequest) (console.Record, error)
	Close(context.Context, executionprincipal.Principal, int64) error
}

type LeasePort interface {
	Grant(vaultsessions.Lease) error
	RevokeSession(console.SessionHandle)
	Authorize(context.Context, executionprincipal.Principal, console.SessionAuthorization, console.SessionOperation) error
}

type LeasePersistence interface {
	Grant(context.Context, int64, vaultsessions.Lease) error
	Revoke(context.Context, int64, int64) error
}

type TokenState struct {
	Active    bool
	ExpiresAt string
	UpdatedAt string
}

type TokenReader interface {
	Get(context.Context, int64) (TokenState, error)
}

type Dependencies struct {
	Database          *sql.DB
	Tokens            TokenReader
	Projects          ProjectPort
	SessionItems      SessionItemPort
	ItemMutations     ItemMutationPort
	Sessions          SessionPort
	Leases            LeasePort
	PersistedLeases   LeasePersistence
	Connector         ConnectorPort
	Delivery          DeliveryGate
	WorkspaceID       string
	RuntimeInstanceID string
	MCPStarted        func() bool
	AllowGenerate     func(int64) bool
}

type Runtime struct {
	database          *sql.DB
	tokens            TokenReader
	projects          ProjectPort
	sessionItems      SessionItemPort
	itemMutations     ItemMutationPort
	sessions          SessionPort
	leases            LeasePort
	persistedLeases   LeasePersistence
	connector         ConnectorPort
	delivery          DeliveryGate
	workspaceID       string
	runtimeInstanceID string
	mcpStarted        func() bool
	allowGenerate     func(int64) bool
}

func NewRuntime(dependencies Dependencies) (*Runtime, error) {
	if dependencies.Database == nil || dependencies.Tokens == nil || dependencies.Projects == nil ||
		dependencies.SessionItems == nil || dependencies.ItemMutations == nil ||
		dependencies.Sessions == nil || dependencies.Leases == nil || dependencies.PersistedLeases == nil ||
		dependencies.Connector == nil || dependencies.Delivery == nil ||
		dependencies.WorkspaceID == "" || dependencies.RuntimeInstanceID == "" ||
		dependencies.MCPStarted == nil || dependencies.AllowGenerate == nil {
		return nil, ErrRuntimeUnavailable
	}
	return &Runtime{
		database: dependencies.Database, tokens: dependencies.Tokens,
		projects: dependencies.Projects, sessionItems: dependencies.SessionItems,
		itemMutations: dependencies.ItemMutations,
		sessions:      dependencies.Sessions, leases: dependencies.Leases,
		persistedLeases: dependencies.PersistedLeases, connector: dependencies.Connector,
		delivery:    dependencies.Delivery,
		workspaceID: dependencies.WorkspaceID, runtimeInstanceID: dependencies.RuntimeInstanceID,
		mcpStarted: dependencies.MCPStarted, allowGenerate: dependencies.AllowGenerate,
	}, nil
}

func (r *Runtime) validate() error {
	if r == nil || r.database == nil || r.tokens == nil || r.projects == nil ||
		r.sessionItems == nil || r.itemMutations == nil || r.sessions == nil ||
		r.leases == nil || r.persistedLeases == nil || r.connector == nil || r.delivery == nil ||
		r.workspaceID == "" || r.runtimeInstanceID == "" ||
		r.mcpStarted == nil || r.allowGenerate == nil {
		return ErrRuntimeUnavailable
	}
	return nil
}

func (r *Runtime) LocalPrincipal() (executionprincipal.Principal, error) {
	if err := r.validate(); err != nil {
		return executionprincipal.Principal{}, err
	}
	return executionprincipal.LocalOperator(r.workspaceID, r.runtimeInstanceID)
}

func (r *Runtime) TokenPrincipal(tokenID int64) (executionprincipal.Principal, error) {
	if err := r.validate(); err != nil {
		return executionprincipal.Principal{}, err
	}
	return executionprincipal.MCPToken(tokenID, r.workspaceID, r.runtimeInstanceID)
}

func (r *Runtime) IsStale(err error) bool { return IsStale(err) }

func (r *Runtime) Prepare(
	ctx context.Context,
	tokenID int64,
	projectRef string,
	actionName string,
	input map[string]any,
) (vaultrequests.PreparedAction, error) {
	if err := r.validate(); err != nil {
		return vaultrequests.PreparedAction{}, err
	}
	project, found, err := r.projects.ResolveRef(ctx, projectRef)
	if err != nil {
		return vaultrequests.PreparedAction{}, err
	}
	if !found {
		return vaultrequests.PreparedAction{}, vaultrequests.ErrProjectNotFound
	}
	approval, contextHash, normalizedInput, err := r.buildApprovalContext(ctx, tokenID, project, actionName, input)
	if err != nil {
		return vaultrequests.PreparedAction{}, vaultrequests.PreparationError{Err: err}
	}
	return vaultrequests.PreparedAction{
		ProjectID: project.ID, RuntimeID: approval.RuntimeID, Input: normalizedInput,
		ApprovalContext: approval, ApprovalContextHash: contextHash,
		RunImmediately: approval.ExecutionRule == accesscontrol.RuleAlwaysRun,
	}, nil
}
