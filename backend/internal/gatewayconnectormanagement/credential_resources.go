package gatewayconnectormanagement

import (
	"net/http"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type CredentialResourceAdapter interface {
	ListCredentialResources(http.ResponseWriter, *http.Request, connectorapi.CredentialResourceRuntime)
	CreateCredentialResource(http.ResponseWriter, *http.Request, connectorapi.CredentialResourceRuntime)
	ImportCredentialResource(http.ResponseWriter, *http.Request, connectorapi.CredentialResourceRuntime)
	GetCredentialResource(http.ResponseWriter, *http.Request, connectorapi.CredentialResourceRuntime)
	UpdateCredentialResource(http.ResponseWriter, *http.Request, connectorapi.CredentialResourceRuntime)
	DeleteCredentialResource(http.ResponseWriter, *http.Request, connectorapi.CredentialResourceRuntime)
}

type CredentialResourceDependencies struct {
	Adapter    func(string) CredentialResourceAdapter
	WriteError func(http.ResponseWriter, int, string)
}

type CredentialResourceHandlers struct {
	component    *Component
	dependencies CredentialResourceDependencies
}

func (component *Component) CredentialResources(dependencies CredentialResourceDependencies) CredentialResourceHandlers {
	return CredentialResourceHandlers{component: component, dependencies: dependencies}
}

func (handlers CredentialResourceHandlers) List(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, CredentialResourceAdapter.ListCredentialResources)
}
func (handlers CredentialResourceHandlers) Create(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, CredentialResourceAdapter.CreateCredentialResource)
}
func (handlers CredentialResourceHandlers) Import(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, CredentialResourceAdapter.ImportCredentialResource)
}
func (handlers CredentialResourceHandlers) Get(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, CredentialResourceAdapter.GetCredentialResource)
}
func (handlers CredentialResourceHandlers) Update(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, CredentialResourceAdapter.UpdateCredentialResource)
}
func (handlers CredentialResourceHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, CredentialResourceAdapter.DeleteCredentialResource)
}

type credentialResourceOperation func(CredentialResourceAdapter, http.ResponseWriter, *http.Request, connectorapi.CredentialResourceRuntime)

func (handlers CredentialResourceHandlers) run(w http.ResponseWriter, r *http.Request, operation credentialResourceOperation) {
	workspace, ok := handlers.component.active(w)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	adapter := handlers.dependencies.Adapter(kind)
	if adapter == nil {
		handlers.dependencies.WriteError(w, http.StatusNotFound, "connector credential resources are not supported")
		return
	}
	if workspace.Credentials.ResourceRuntime == nil {
		handlers.dependencies.WriteError(w, http.StatusServiceUnavailable, "connector credential runtime is unavailable")
		return
	}
	operation(adapter, w, r, workspace.Credentials.ResourceRuntime(kind))
}
