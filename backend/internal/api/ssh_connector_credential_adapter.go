package api

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/sshkeys"
)

func (sshRuntimeAdapter) ListCredentialResources(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime) {
	items, err := runtime.sshKeys.List(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (sshRuntimeAdapter) CreateCredentialResource(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime) {
	var request sshkeys.CreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	item, err := runtime.sshKeys.Create(r.Context(), request)
	if err != nil {
		handleSSHKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (sshRuntimeAdapter) ImportCredentialResource(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime) {
	var request sshkeys.ImportRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	item, err := runtime.sshKeys.Import(r.Context(), request)
	if err != nil {
		handleSSHKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (sshRuntimeAdapter) GetCredentialResource(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	item, err := runtime.sshKeys.Get(r.Context(), id)
	if err != nil {
		handleSSHKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (sshRuntimeAdapter) UpdateCredentialResource(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request sshkeys.UpdateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	item, err := runtime.sshKeys.Update(r.Context(), id, request)
	if err != nil {
		handleSSHKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (sshRuntimeAdapter) DeleteCredentialResource(handler credentialHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	if err := runtime.sshKeys.Delete(r.Context(), id); err != nil {
		handleSSHKeyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
