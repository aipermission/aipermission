package applicationconnectormanagement

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
)

type CredentialResourceDependencies struct {
	Adapter    func(string) connectorapi.CredentialResourceAdapter
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
	handlers.run(w, r, connectorapi.CredentialResourceAdapter.ListCredentialResources)
}
func (handlers CredentialResourceHandlers) Create(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, connectorapi.CredentialResourceAdapter.CreateCredentialResource)
}
func (handlers CredentialResourceHandlers) Import(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, connectorapi.CredentialResourceAdapter.ImportCredentialResource)
}
func (handlers CredentialResourceHandlers) Get(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, connectorapi.CredentialResourceAdapter.GetCredentialResource)
}
func (handlers CredentialResourceHandlers) Update(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, connectorapi.CredentialResourceAdapter.UpdateCredentialResource)
}
func (handlers CredentialResourceHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	handlers.run(w, r, connectorapi.CredentialResourceAdapter.DeleteCredentialResource)
}

type credentialResourceOperation func(connectorapi.CredentialResourceAdapter, http.ResponseWriter, *http.Request, connectorapi.CredentialResourceRuntime)

func (handlers CredentialResourceHandlers) run(w http.ResponseWriter, r *http.Request, operation credentialResourceOperation) {
	runtime, ok := handlers.component.active(w)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	adapter := handlers.dependencies.Adapter(kind)
	if adapter == nil {
		handlers.dependencies.WriteError(w, http.StatusNotFound, "connector credential resources are not supported")
		return
	}
	operation(adapter, w, r, connectortransport.CredentialResourceRuntime(runtime, kind))
}
