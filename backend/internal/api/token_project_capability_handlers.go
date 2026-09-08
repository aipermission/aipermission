package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/projectcapabilities"
)

type projectCapabilityInput struct {
	ProjectID      int64  `json:"project_id"`
	CapabilityName string `json:"capability_name"`
	ExecutionRule  string `json:"execution_rule"`
	ExpiresAt      string `json:"expires_at"`
}

type updateProjectCapabilitiesRequest struct {
	Capabilities     []projectCapabilityInput `json:"capabilities"`
	ExpectedRevision string                   `json:"expected_revision"`
}

func (s tokenHandlers) listTokenProjectCapabilities(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	tokenID, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, err := runtime.tokens.Get(r.Context(), tokenID); err != nil {
		handleTokenError(w, err)
		return
	}
	items, err := projectcapabilities.NewStore(runtime.database).List(r.Context(), tokenID)
	if err != nil {
		writeInternalError(w)
		return
	}
	revision, err := projectCapabilitiesRevision(items)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"definitions": projectcapabilities.Definitions(),
		"items":       items,
		"revision":    revision,
	})
}

func (s tokenHandlers) updateTokenProjectCapabilities(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	tokenID, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, err := runtime.tokens.Get(r.Context(), tokenID); err != nil {
		handleTokenError(w, err)
		return
	}
	var request updateProjectCapabilitiesRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	inputs := make([]projectcapabilities.SetInput, 0, len(request.Capabilities))
	for _, capability := range request.Capabilities {
		inputs = append(inputs, projectcapabilities.SetInput{
			ProjectID: capability.ProjectID, Name: capability.CapabilityName,
			ExecutionRule: capability.ExecutionRule, ExpiresAt: capability.ExpiresAt,
		})
	}
	var items []projectcapabilities.Capability
	changed, err := s.mutateTokenWithVaultInvalidation(r.Context(), runtime, tokenID, "token.project_capabilities.updated", func() any {
		return map[string]any{"token_id": tokenID, "capabilities": items}
	}, "Vault project capability changed; send a fresh request", func(tx *sql.Tx) (bool, error) {
		txStore := projectcapabilities.NewTxStore(tx)
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
	if errors.Is(err, errVaultDeliveryCanceled) {
		writeError(w, http.StatusRequestTimeout, "Vault capability update was canceled")
		return
	}
	if err != nil {
		if handleAuthorizationRevisionError(w, err) {
			return
		}
		handleProjectCapabilityError(w, err)
		return
	}
	revision, revisionErr := projectCapabilitiesRevision(items)
	if revisionErr != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"definitions": projectcapabilities.Definitions(),
		"items":       items, "changed": changed, "revision": revision,
	})
}

func handleProjectCapabilityError(w http.ResponseWriter, err error) {
	var validation projectcapabilities.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, strings.TrimSpace(validation.Error()))
	case errors.Is(err, projectcapabilities.ErrNotFound):
		writeError(w, http.StatusNotFound, "project capability not found")
	default:
		writeInternalError(w)
	}
}
