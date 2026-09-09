package accesscontrol

import (
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func (h *HTTPHandlers) ListTokens(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireTokens|requireReusableTokens)
	if !ok {
		return
	}
	items, err := scope.Tokens.List(r.Context())
	if err != nil {
		log.Printf("list tokens failed: %v", err)
		writeTokenError(w, err)
		return
	}
	reusableTokens, err := scope.ReusableTokens(r.Context())
	if err != nil {
		log.Printf("read security settings for token list failed: %v", err)
		httptransport.WriteInternalError(w)
		return
	}
	if !reusableTokens {
		stripReusableTokenValues(items)
	}
	writeSensitiveJSON(w, http.StatusOK, items)
}

func stripReusableTokenValues(items []tokens.Token) {
	for index := range items {
		items[index].TokenValue = ""
	}
}

func (h *HTTPHandlers) CreateToken(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireTokens|requireReusableTokens|requireMutation)
	if !ok {
		return
	}
	var request tokens.CreateRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	reusableTokens, err := scope.ReusableTokens(r.Context())
	if err != nil {
		log.Printf("read security settings for token create failed: %v", err)
		httptransport.WriteInternalError(w)
		return
	}
	var item tokens.CreateResponse
	err = scope.Mutate(r.Context(), "token.created", func() any {
		return map[string]any{"token_id": item.ID, "name": item.Name, "reusable_tokens": reusableTokens}
	}, func(tx *sql.Tx) error {
		var createErr error
		item, createErr = scope.Tokens.WithTx(tx).Create(r.Context(), request, tokens.CreateOptions{StoreReusableToken: reusableTokens})
		return createErr
	})
	if err != nil {
		writeTokenError(w, err)
		return
	}
	writeSensitiveJSON(w, http.StatusCreated, item)
}

func (h *HTTPHandlers) RevokeToken(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireTokens|requireReusableTokens|requireAuthorizationMutation)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	reusableTokens, err := scope.ReusableTokens(r.Context())
	if err != nil {
		log.Printf("read security settings for token revoke failed: %v", err)
		httptransport.WriteInternalError(w)
		return
	}
	var item tokens.Token
	_, err = mutateAuthorization(r.Context(), scope, id, "token.revoked", func() any {
		return map[string]any{"token_id": item.ID, "name": item.Name}
	}, "token revoked; send a fresh Vault request", func(tx *sql.Tx) (bool, error) {
		current, getErr := scope.Tokens.WithTx(tx).Get(r.Context(), id)
		if getErr != nil {
			return false, getErr
		}
		if current.RevokedAt != "" {
			item = current
			return false, nil
		}
		var revokeErr error
		item, revokeErr = scope.Tokens.WithTx(tx).Revoke(r.Context(), id)
		return true, revokeErr
	})
	if errors.Is(err, ErrVaultDeliveryCanceled) {
		httptransport.WriteError(w, http.StatusRequestTimeout, "token revoke was canceled")
		return
	}
	if err != nil {
		writeTokenError(w, err)
		return
	}
	if !reusableTokens {
		item.TokenValue = ""
	}
	writeSensitiveJSON(w, http.StatusOK, item)
}

func writeSensitiveJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	httptransport.WriteJSON(w, status, payload)
}

func writeTokenError(w http.ResponseWriter, err error) {
	var validation tokens.ValidationError
	switch {
	case errors.Is(err, tokens.ErrNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "token not found")
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
	default:
		httptransport.WriteError(w, http.StatusInternalServerError, "token operation failed")
	}
}
