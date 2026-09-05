package api

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
)

func (s credentialHandlers) listCredentials(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := s.credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.ListCredentialResources(w, r, connectorCredentialResourceRuntime(runtime, r.PathValue("kind")))
}

func (s credentialHandlers) createCredential(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := s.credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.CreateCredentialResource(w, r, connectorCredentialResourceRuntime(runtime, r.PathValue("kind")))
}

func (s credentialHandlers) importCredential(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := s.credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.ImportCredentialResource(w, r, connectorCredentialResourceRuntime(runtime, r.PathValue("kind")))
}

func (s credentialHandlers) getCredential(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := s.credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.GetCredentialResource(w, r, connectorCredentialResourceRuntime(runtime, r.PathValue("kind")))
}

func (s credentialHandlers) updateCredential(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := s.credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.UpdateCredentialResource(w, r, connectorCredentialResourceRuntime(runtime, r.PathValue("kind")))
}

func (s credentialHandlers) deleteCredential(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := s.credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.DeleteCredentialResource(w, r, connectorCredentialResourceRuntime(runtime, r.PathValue("kind")))
}

func (s credentialHandlers) credentialResourceAdapter(w http.ResponseWriter, r *http.Request) (connectorapi.CredentialResourceAdapter, bool) {
	adapter := s.connectorCredentialResourceAdapterFor(r.PathValue("kind"))
	if adapter == nil {
		writeError(w, http.StatusNotFound, "connector credential resources are not supported")
		return nil, false
	}
	return adapter, true
}
