package api

import (
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
	Capabilities []projectCapabilityInput `json:"capabilities"`
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
	writeJSON(w, http.StatusOK, map[string]any{
		"definitions": projectcapabilities.Definitions(),
		"items":       items,
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
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	inputs := make([]projectcapabilities.SetInput, 0, len(request.Capabilities))
	for _, capability := range request.Capabilities {
		inputs = append(inputs, projectcapabilities.SetInput{
			ProjectID: capability.ProjectID, Name: capability.CapabilityName,
			ExecutionRule: capability.ExecutionRule, ExpiresAt: capability.ExpiresAt,
		})
	}
	if err := s.writeAuditRequired(r.Context(), runtime, "user", nil, 0, "token.project_capabilities.update_requested", map[string]any{
		"token_id": tokenID, "capabilities": request.Capabilities,
	}); err != nil {
		writeInternalError(w)
		return
	}
	items, err := projectcapabilities.NewStore(runtime.database).Replace(r.Context(), tokenID, inputs)
	if err != nil {
		handleProjectCapabilityError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "token.project_capabilities.updated", map[string]any{
		"token_id": tokenID, "capabilities": items,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"definitions": projectcapabilities.Definitions(),
		"items":       items,
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
