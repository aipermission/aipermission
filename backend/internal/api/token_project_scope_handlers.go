package api

import (
	"database/sql"
	"errors"
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
	if !decodeJSON(w, r, &request) {
		return
	}
	var items []projectstore.TokenScope
	changed, err := s.mutateTokenWithVaultInvalidation(r.Context(), runtime, tokenID, "token.project_scopes.updated", func() any {
		return map[string]any{"token_id": tokenID, "enabled_project_ids": request.EnabledProjectIDs}
	}, "project visibility changed; send a fresh Vault request", func(tx *sql.Tx) (bool, error) {
		nextItems, mutationChanged, replaceErr := projectstore.NewTxStore(tx).ReplaceTokenScopesWithChange(r.Context(), tokenID, request.EnabledProjectIDs)
		items = nextItems
		return mutationChanged, replaceErr
	})
	if errors.Is(err, errVaultDeliveryCanceled) {
		writeError(w, http.StatusRequestTimeout, "project scope update was canceled")
		return
	}
	if err != nil {
		handleProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "changed": changed})
}
