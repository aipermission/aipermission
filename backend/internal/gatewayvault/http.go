package gatewayvault

import (
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
		Projects:       projects.NewHTTPHandlers(projects.ScopeProvider(dependencies.Projects)),
		ProjectVault:   projectvault.NewHTTPHandlers(projectvault.HTTPScopeProvider(dependencies.ProjectVault)),
		VaultApprovals: vaultrequests.NewHTTPHandlers(vaultrequests.HTTPScopeProvider(dependencies.VaultApprovals)),
		MCPVault:       vaultrequests.NewMCPHTTPHandlers(vaultrequests.MCPHTTPScopeProvider(dependencies.MCPVault)),
	}
}
