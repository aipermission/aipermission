package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

type saveVaultDefaultBindingRequest struct {
	VaultItemID             int64 `json:"vault_item_id"`
	SourceProjectID         int64 `json:"source_project_id"`
	TargetID                int64 `json:"target_id"`
	ProfileID               int64 `json:"profile_id"`
	ReplaceExisting         bool  `json:"replace_existing"`
	ExpectedBindingRevision int64 `json:"expected_binding_revision"`
}

type deleteVaultDefaultBindingRequest struct {
	ExpectedBindingRevision int64 `json:"expected_binding_revision"`
}

func (s vaultItemHandlers) listVaultDefaultBindings(w http.ResponseWriter, r *http.Request) {
	_, store, ok := s.store(w, r)
	if !ok {
		return
	}
	itemID, err := positiveQueryID(r, "vault_item_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetID, err := positiveQueryID(r, "target_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	profileID, err := positiveQueryID(r, "profile_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := store.ListDefaultBindings(r.Context(), itemID, targetID, profileID)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s vaultItemHandlers) saveVaultDefaultBinding(w http.ResponseWriter, r *http.Request) {
	runtime, store, ok := s.store(w, r)
	if !ok {
		return
	}
	var request saveVaultDefaultBindingRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.writeRequiredVaultAudit(r, runtime, "vault.binding.update.requested", map[string]any{
		"vault_item_id": request.VaultItemID, "source_project_id": request.SourceProjectID,
		"target_id": request.TargetID, "profile_id": request.ProfileID,
		"expected_binding_revision": request.ExpectedBindingRevision,
	}); err != nil {
		writeInternalError(w)
		return
	}
	item, err := store.SaveDefaultBinding(r.Context(), projectvault.DefaultBindingInput{
		VaultItemID: request.VaultItemID, SourceProjectID: request.SourceProjectID,
		TargetID: request.TargetID, ProfileID: request.ProfileID,
		ReplaceExisting:         request.ReplaceExisting,
		ExpectedBindingRevision: request.ExpectedBindingRevision,
	})
	if err != nil {
		handleVaultBindingError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "vault.binding.updated", vaultBindingAuditPayload(item))
	writeJSON(w, http.StatusOK, item)
}

func (s vaultItemHandlers) deleteVaultDefaultBinding(w http.ResponseWriter, r *http.Request) {
	runtime, store, ok := s.store(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request deleteVaultDefaultBindingRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.writeRequiredVaultAudit(r, runtime, "vault.binding.delete.requested", map[string]any{
		"binding_id": id, "expected_binding_revision": request.ExpectedBindingRevision,
	}); err != nil {
		writeInternalError(w)
		return
	}
	if err := store.DeleteDefaultBinding(r.Context(), id, request.ExpectedBindingRevision); err != nil {
		handleVaultBindingError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "vault.binding.deleted", map[string]any{"binding_id": id})
	w.WriteHeader(http.StatusNoContent)
}

func positiveQueryID(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, projectvault.ValidationError(name + " must be a positive integer")
	}
	return value, nil
}

func handleVaultBindingError(w http.ResponseWriter, err error) {
	var validation projectvault.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, projectvault.ErrNotFound):
		writeError(w, http.StatusNotFound, "vault default binding not found")
	case errors.Is(err, projectvault.ErrStale):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeInternalError(w)
	}
}

func vaultBindingAuditPayload(item projectvault.DefaultBinding) map[string]any {
	return map[string]any{
		"binding_id": item.ID, "vault_item_id": item.VaultItemID,
		"source_project_id": item.SourceProjectID,
		"target_id":         item.TargetID, "profile_id": item.ProfileID,
		"binding_revision": item.BindingRevision,
	}
}
