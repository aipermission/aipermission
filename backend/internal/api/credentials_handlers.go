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
	adapter, ok := credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.ListCredentialResources(s, w, r, runtime)
}

func (s credentialHandlers) createCredential(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.CreateCredentialResource(s, w, r, runtime)
}

func (s credentialHandlers) importCredential(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.ImportCredentialResource(s, w, r, runtime)
}

func (s credentialHandlers) getCredential(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.GetCredentialResource(s, w, r, runtime)
}

func (s credentialHandlers) updateCredential(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.UpdateCredentialResource(s, w, r, runtime)
}

func (s credentialHandlers) deleteCredential(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	adapter, ok := credentialResourceAdapter(w, r)
	if !ok {
		return
	}
	adapter.DeleteCredentialResource(s, w, r, runtime)
}

func credentialResourceAdapter(w http.ResponseWriter, r *http.Request) (connectorapi.CredentialResourceAdapter, bool) {
	adapter := connectorCredentialResourceAdapterFor(r.PathValue("kind"))
	if adapter == nil {
		writeError(w, http.StatusNotFound, "connector credential resources are not supported")
		return nil, false
	}
	return adapter, true
}
