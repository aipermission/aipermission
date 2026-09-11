package gatewayvault

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type HTTPDependencies struct {
	Projects       ProjectScopeProvider
	ProjectVault   ProjectVaultScopeProvider
	VaultApprovals VaultApprovalScopeProvider
	MCPVault       MCPVaultScopeProvider
}

type ProjectScopeProvider func(http.ResponseWriter) (ProjectScope, bool)
type ProjectVaultScopeProvider func(http.ResponseWriter) (ProjectVaultHTTPScope, bool)
type VaultApprovalScopeProvider func(http.ResponseWriter) (VaultApprovalHTTPScope, bool)
type MCPVaultScopeProvider func(http.ResponseWriter, *http.Request) (VaultMCPHTTPScope, bool)

type HTTPHandlers struct {
	Projects       ProjectsHTTP
	ProjectVault   ProjectVaultHTTP
	VaultApprovals VaultApprovalsHTTP
	MCPVault       MCPVaultHTTP
}

type ProjectsHTTP interface {
	List(http.ResponseWriter, *http.Request)
	Create(http.ResponseWriter, *http.Request)
	Update(http.ResponseWriter, *http.Request)
	Archive(http.ResponseWriter, *http.Request)
}

type ProjectVaultHTTP interface {
	ListItems(http.ResponseWriter, *http.Request)
	CreateItem(http.ResponseWriter, *http.Request)
	GetItem(http.ResponseWriter, *http.Request)
	UpdateItem(http.ResponseWriter, *http.Request)
	GenerateItemPreview(http.ResponseWriter, *http.Request)
	ReplaceItemValue(http.ResponseWriter, *http.Request)
	RevealItem(http.ResponseWriter, *http.Request)
	DeleteItem(http.ResponseWriter, *http.Request)
	ListDefaultBindings(http.ResponseWriter, *http.Request)
	SaveDefaultBinding(http.ResponseWriter, *http.Request)
	DeleteDefaultBinding(http.ResponseWriter, *http.Request)
	SessionOptions(http.ResponseWriter, *http.Request)
}

type VaultApprovalsHTTP interface {
	List(http.ResponseWriter, *http.Request)
	Run(http.ResponseWriter, *http.Request)
	Decline(http.ResponseWriter, *http.Request)
}

type MCPVaultHTTP interface {
	ListItems(http.ResponseWriter, *http.Request)
	Call(http.ResponseWriter, *http.Request)
	GetRequest(http.ResponseWriter, *http.Request)
	CancelRequest(http.ResponseWriter, *http.Request)
}

func (*Component) HTTPHandlers(dependencies HTTPDependencies) HTTPHandlers {
	return HTTPHandlers{
		Projects:       projects.NewHTTPHandlers(projectScopeProvider(dependencies.Projects)),
		ProjectVault:   projectvault.NewHTTPHandlers(projectVaultScopeProvider(dependencies.ProjectVault)),
		VaultApprovals: vaultrequests.NewHTTPHandlers(vaultApprovalScopeProvider(dependencies.VaultApprovals)),
		MCPVault:       vaultrequests.NewMCPHTTPHandlers(mcpVaultScopeProvider(dependencies.MCPVault)),
	}
}

func projectScopeProvider(provider ProjectScopeProvider) projects.ScopeProvider {
	if provider == nil {
		return nil
	}
	return func(w http.ResponseWriter) (projects.Scope, bool) {
		scope, ok := provider(w)
		var mutate func(context.Context, string, func() any, func(*sql.Tx) error) error
		if scope.Mutate != nil {
			mutate = func(ctx context.Context, action string, payload func() any, mutation func(*sql.Tx) error) error {
				return scope.Mutate(ctx, action, payload, mutation)
			}
		}
		return projects.Scope{
			Database: scope.Database, Mutate: mutate,
			AcquireExclusive: scope.AcquireExclusive, Invalidate: scope.Invalidate,
		}, ok
	}
}

func projectVaultScopeProvider(provider ProjectVaultScopeProvider) projectvault.HTTPScopeProvider {
	if provider == nil {
		return nil
	}
	return func(w http.ResponseWriter) (projectvault.HTTPScope, bool) {
		scope, ok := provider(w)
		return projectvault.HTTPScope{
			Runtime: scope.Runtime, RuntimeID: scope.RuntimeID, SessionCatalog: scope.SessionCatalog,
		}, ok
	}
}

func vaultApprovalScopeProvider(provider VaultApprovalScopeProvider) vaultrequests.HTTPScopeProvider {
	if provider == nil {
		return nil
	}
	return func(w http.ResponseWriter) (vaultrequests.HTTPScope, bool) {
		scope, ok := provider(w)
		var runtime func(context.Context) (vaultrequests.Application, error)
		if scope.Runtime != nil {
			runtime = func(ctx context.Context) (vaultrequests.Application, error) { return scope.Runtime(ctx) }
		}
		return vaultrequests.HTTPScope{MCPStarted: scope.MCPStarted, Runtime: runtime}, ok
	}
}

func mcpVaultScopeProvider(provider MCPVaultScopeProvider) vaultrequests.MCPHTTPScopeProvider {
	if provider == nil {
		return nil
	}
	return func(w http.ResponseWriter, r *http.Request) (vaultrequests.MCPHTTPScope, bool) {
		scope, ok := provider(w, r)
		var runtime func(context.Context) (vaultrequests.Application, error)
		if scope.Runtime != nil {
			runtime = func(ctx context.Context) (vaultrequests.Application, error) { return scope.Runtime(ctx) }
		}
		return vaultrequests.MCPHTTPScope{
			Database: scope.Database, Vault: scope.Vault, WorkspaceUUID: scope.WorkspaceUUID,
			TokenID: scope.TokenID, MCPStarted: scope.MCPStarted, Runtime: runtime, MetadataRead: scope.MetadataRead,
		}, ok
	}
}
