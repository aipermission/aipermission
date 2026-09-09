package accesscontrol

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type ProjectCapabilityInput struct {
	ProjectID      int64  `json:"project_id"`
	CapabilityName string `json:"capability_name"`
	ExecutionRule  string `json:"execution_rule"`
	ExpiresAt      string `json:"expires_at"`
}

type UpdateProjectCapabilitiesRequest struct {
	Capabilities     []ProjectCapabilityInput `json:"capabilities"`
	ExpectedRevision string                   `json:"expected_revision"`
}

func (h *HTTPHandlers) ListProjectCapabilities(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireDatabase|requireTokens)
	if !ok {
		return
	}
	tokenID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	if _, err := scope.Tokens.Get(r.Context(), tokenID); err != nil {
		writeTokenError(w, err)
		return
	}
	items, err := NewCapabilityStore(scope.Database).List(r.Context(), tokenID)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	revision, err := projectCapabilitiesRevision(items)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"definitions": CapabilityDefinitions(), "items": items, "revision": revision,
	})
}

func (h *HTTPHandlers) UpdateProjectCapabilities(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireDatabase|requireTokens|requireAuthorizationMutation)
	if !ok {
		return
	}
	tokenID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	if _, err := scope.Tokens.Get(r.Context(), tokenID); err != nil {
		writeTokenError(w, err)
		return
	}
	var request UpdateProjectCapabilitiesRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	inputs := make([]CapabilitySetInput, 0, len(request.Capabilities))
	for _, capability := range request.Capabilities {
		inputs = append(inputs, CapabilitySetInput{
			ProjectID: capability.ProjectID, Name: capability.CapabilityName,
			ExecutionRule: capability.ExecutionRule, ExpiresAt: capability.ExpiresAt,
		})
	}
	var items []Capability
	changed, err := mutateAuthorization(r.Context(), scope, tokenID, "token.project_capabilities.updated", func() any {
		return map[string]any{"token_id": tokenID, "capabilities": items}
	}, "Vault project capability changed; send a fresh request", func(tx *sql.Tx) (bool, error) {
		txStore := NewCapabilityTxStore(tx)
		current, currentErr := txStore.List(r.Context(), tokenID)
		if currentErr != nil {
			return false, currentErr
		}
		currentRevision, revisionErr := projectCapabilitiesRevision(current)
		if _, revisionErr = requireAuthorizationRevision(request.ExpectedRevision, currentRevision, revisionErr); revisionErr != nil {
			return false, revisionErr
		}
		nextItems, mutationChanged, replaceErr := txStore.ReplaceWithChange(r.Context(), tokenID, inputs)
		items = nextItems
		return mutationChanged, replaceErr
	})
	if errors.Is(err, ErrVaultDeliveryCanceled) {
		httptransport.WriteError(w, http.StatusRequestTimeout, "Vault capability update was canceled")
		return
	}
	if err != nil {
		if writeAuthorizationRevisionError(w, err) {
			return
		}
		writeProjectCapabilityError(w, err)
		return
	}
	revision, revisionErr := projectCapabilitiesRevision(items)
	if revisionErr != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"definitions": CapabilityDefinitions(),
		"items":       items, "changed": changed, "revision": revision,
	})
}

func writeProjectCapabilityError(w http.ResponseWriter, err error) {
	var validation ValidationError
	switch {
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, strings.TrimSpace(validation.Error()))
	case errors.Is(err, ErrCapabilityNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "project capability not found")
	default:
		httptransport.WriteInternalError(w)
	}
}
