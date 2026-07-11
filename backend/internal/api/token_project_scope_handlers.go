package api

import (
	"net/http"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

type updateTokenProjectScopesRequest struct {
	EnabledProjectIDs []int64 `json:"enabled_project_ids"`
}

func (s tokenHandlers) listTokenProjectScopes(w http.ResponseWriter, r *http.Request) {
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
	items, err := projectstore.NewStore(runtime.database).ListTokenScopes(r.Context(), tokenID)
	if err != nil {
		handleProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s tokenHandlers) updateTokenProjectScopes(w http.ResponseWriter, r *http.Request) {
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
	var request updateTokenProjectScopesRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	items, err := projectstore.NewStore(runtime.database).ReplaceTokenScopes(r.Context(), tokenID, request.EnabledProjectIDs)
	if err != nil {
		handleProjectError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "token.project_scopes.updated", map[string]any{
		"token_id":            tokenID,
		"enabled_project_ids": request.EnabledProjectIDs,
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
