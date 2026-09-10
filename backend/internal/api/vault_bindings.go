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
	_, owner, ok := s.owner(w, r)
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
	items, err := owner.ListDefaultBindings(r.Context(), projectvault.DefaultBindingFilter{
		VaultItemID: itemID, TargetID: targetID, ProfileID: profileID,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s vaultItemHandlers) saveVaultDefaultBinding(w http.ResponseWriter, r *http.Request) {
	_, owner, ok := s.owner(w, r)
	if !ok {
		return
	}
	var request saveVaultDefaultBindingRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	input := projectvault.DefaultBindingInput{
		VaultItemID: request.VaultItemID, SourceProjectID: request.SourceProjectID,
		TargetID: request.TargetID, ProfileID: request.ProfileID,
		ReplaceExisting:         request.ReplaceExisting,
		ExpectedBindingRevision: request.ExpectedBindingRevision,
	}
	item, err := owner.SaveDefaultBinding(r.Context(), input)
	if err != nil {
		if writeVaultDeliveryCancellation(w, err, "Vault binding update was canceled") {
			return
		}
		handleVaultBindingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s vaultItemHandlers) deleteVaultDefaultBinding(w http.ResponseWriter, r *http.Request) {
	_, owner, ok := s.owner(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request deleteVaultDefaultBindingRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := owner.DeleteDefaultBinding(r.Context(), id, request.ExpectedBindingRevision); err != nil {
		if writeVaultDeliveryCancellation(w, err, "Vault binding deletion was canceled") {
			return
		}
		handleVaultBindingError(w, err)
		return
	}
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
	case errors.Is(err, projectvault.ErrBindingTargetNotFound):
		writeError(w, http.StatusNotFound, "connector target not found")
	case errors.Is(err, projectvault.ErrSessionEnvironmentUnsupported):
		writeError(w, http.StatusConflict, "this connector profile does not support Vault session environments")
	case errors.Is(err, projectvault.ErrStale):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeInternalError(w)
	}
}
