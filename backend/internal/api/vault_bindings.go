package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
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
	if !decodeJSON(w, r, &request) {
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "Vault binding update was canceled")
		return
	}
	defer release()
	targetStore := connectortargets.NewStore(runtime.database)
	target, err := targetStore.GetTarget(r.Context(), request.TargetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	adapter := s.connectorLiveConsoleTargetAdapterFor(target.ConnectorKind)
	if adapter == nil {
		writeError(w, http.StatusConflict, "this connector profile does not support Vault session environments")
		return
	}
	surface, err := targetStore.GetRuntimeSurfaceByProfile(
		r.Context(),
		target.ConnectorKind,
		request.TargetID,
		request.ProfileID,
		adapter.LiveConsoleCapabilityKind(),
	)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if err := requireSessionEnvironmentCapability(r.Context(), s.Server, runtime, surface.ID); err != nil {
		writeError(w, http.StatusConflict, "this connector profile does not support Vault session environments")
		return
	}
	existing, found, err := store.FindDefaultBinding(r.Context(), projectvault.DefaultBindingInput{
		VaultItemID: request.VaultItemID, SourceProjectID: request.SourceProjectID,
		TargetID: request.TargetID, ProfileID: request.ProfileID,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	if (found && existing.BindingRevision != request.ExpectedBindingRevision) ||
		(!found && request.ExpectedBindingRevision != 0) {
		handleVaultBindingError(w, projectvault.ErrStale)
		return
	}
	if found && existing.ReplaceExisting == request.ReplaceExisting {
		writeJSON(w, http.StatusOK, existing)
		return
	}
	sessions := []projectvault.SessionReference{}
	if found {
		sessions, err = store.ActiveSessionsForMutation(r.Context(), projectvault.SessionMutationScope{BindingID: existing.ID})
		if err != nil {
			writeInternalError(w)
			return
		}
	}
	input := projectvault.DefaultBindingInput{
		VaultItemID: request.VaultItemID, SourceProjectID: request.SourceProjectID,
		TargetID: request.TargetID, ProfileID: request.ProfileID,
		ReplaceExisting:         request.ReplaceExisting,
		ExpectedBindingRevision: request.ExpectedBindingRevision,
	}
	var item projectvault.DefaultBinding
	err = s.withAuditedMutation(r.Context(), runtime, "user", nil, 0, "vault.binding.updated", func() any {
		return vaultBindingAuditPayload(item)
	}, func(tx *sql.Tx) error {
		var saveErr error
		item, saveErr = store.WithTx(tx).SaveDefaultBinding(r.Context(), input)
		return saveErr
	})
	if err != nil {
		handleVaultBindingError(w, err)
		return
	}
	if err := s.invalidateVaultMutationAfterCommit(r.Context(), runtime, sessions, projectvault.SessionMutationScope{BindingID: item.ID}); err != nil {
		writeInternalError(w)
		return
	}
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
	if !decodeJSON(w, r, &request) {
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "Vault binding deletion was canceled")
		return
	}
	defer release()
	binding, err := store.GetDefaultBinding(r.Context(), id)
	if err != nil {
		handleVaultBindingError(w, err)
		return
	}
	if binding.BindingRevision != request.ExpectedBindingRevision {
		handleVaultBindingError(w, projectvault.ErrStale)
		return
	}
	sessions, err := store.ActiveSessionsForMutation(r.Context(), projectvault.SessionMutationScope{BindingID: binding.ID})
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := s.withAuditedMutation(r.Context(), runtime, "user", nil, 0, "vault.binding.deleted", func() any {
		return map[string]any{"binding_id": id}
	}, func(tx *sql.Tx) error {
		return store.WithTx(tx).DeleteDefaultBinding(r.Context(), id, request.ExpectedBindingRevision)
	}); err != nil {
		handleVaultBindingError(w, err)
		return
	}
	if err := s.invalidateVaultMutationAfterCommit(r.Context(), runtime, sessions, projectvault.SessionMutationScope{BindingID: binding.ID}); err != nil {
		writeInternalError(w)
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
