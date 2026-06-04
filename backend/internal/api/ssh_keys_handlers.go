package api

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/sshkeys"
)

func (s sshKeyHandlers) listSSHKeys(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	items, err := runtime.sshKeys.List(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s sshKeyHandlers) createSSHKey(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
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

func (s sshKeyHandlers) importSSHKey(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
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

func (s sshKeyHandlers) getSSHKey(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
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

func (s sshKeyHandlers) deleteSSHKey(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
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
