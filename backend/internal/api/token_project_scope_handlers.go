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
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "project scope update was canceled")
		return
	}
	defer release()
	var items []projectstore.TokenScope
	changed := false
	err = s.withAuditedMutation(r.Context(), runtime, "user", nil, 0, "token.project_scopes.updated", func() any {
		return map[string]any{"token_id": tokenID, "enabled_project_ids": request.EnabledProjectIDs}
	}, func(tx *sql.Tx) error {
		var replaceErr error
		items, changed, replaceErr = projectstore.NewTxStore(tx).ReplaceTokenScopesWithChange(r.Context(), tokenID, request.EnabledProjectIDs)
		if replaceErr == nil && !changed {
			return errAuditedMutationUnchanged
		}
		return replaceErr
	})
	if errors.Is(err, errAuditedMutationUnchanged) {
		err = nil
	}
	if err != nil {
		handleProjectError(w, err)
		return
	}
	if changed {
		if err := s.invalidateVaultTokenSessions(r.Context(), runtime, tokenID, "project visibility changed; send a fresh Vault request"); err != nil {
			writeInternalError(w)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "changed": changed})
}
