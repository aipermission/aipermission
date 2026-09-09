package accesscontrol

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

type UpdateProjectScopesRequest struct {
	EnabledProjectIDs []int64 `json:"enabled_project_ids"`
	ExpectedRevision  string  `json:"expected_revision"`
}

func (h *HTTPHandlers) ListProjectScopes(w http.ResponseWriter, r *http.Request) {
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
	items, err := projectstore.NewStore(scope.Database).ListTokenScopes(r.Context(), tokenID)
	if err != nil {
		projectstore.WriteHTTPError(w, err)
		return
	}
	revision, err := projectScopesRevision(items)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "revision": revision})
}

func (h *HTTPHandlers) UpdateProjectScopes(w http.ResponseWriter, r *http.Request) {
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
	var request UpdateProjectScopesRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	var items []projectstore.TokenScope
	changed, err := mutateAuthorization(r.Context(), scope, tokenID, "token.project_scopes.updated", func() any {
		return map[string]any{"token_id": tokenID, "enabled_project_ids": request.EnabledProjectIDs}
	}, "project visibility changed; send a fresh Vault request", func(tx *sql.Tx) (bool, error) {
		txStore := projectstore.NewTxStore(tx)
		current, currentErr := txStore.ListTokenScopes(r.Context(), tokenID)
		if currentErr != nil {
			return false, currentErr
		}
		currentRevision, revisionErr := projectScopesRevision(current)
		if _, revisionErr = requireAuthorizationRevision(request.ExpectedRevision, currentRevision, revisionErr); revisionErr != nil {
			return false, revisionErr
		}
		nextItems, mutationChanged, replaceErr := txStore.ReplaceTokenScopesWithChange(r.Context(), tokenID, request.EnabledProjectIDs)
		items = nextItems
		return mutationChanged, replaceErr
	})
	if errors.Is(err, ErrVaultDeliveryCanceled) {
		httptransport.WriteError(w, http.StatusRequestTimeout, "project scope update was canceled")
		return
	}
	if err != nil {
		if writeAuthorizationRevisionError(w, err) {
			return
		}
		projectstore.WriteHTTPError(w, err)
		return
	}
	revision, revisionErr := projectScopesRevision(items)
	if revisionErr != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "changed": changed, "revision": revision})
}
