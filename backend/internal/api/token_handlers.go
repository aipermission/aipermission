package api

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func (s tokenHandlers) listTokens(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	items, err := runtime.tokens.List(r.Context())
	if err != nil {
		log.Printf("list tokens failed: %v", err)
		writeInternalError(w)
		return
	}
	settings, err := readSecuritySettings(r.Context(), runtime)
	if err != nil {
		log.Printf("read security settings for token list failed: %v", err)
		writeInternalError(w)
		return
	}
	if !settings.ReusableTokens {
		items = stripReusableTokenValues(items)
	}
	writeSensitiveJSON(w, http.StatusOK, items)
}

func (s tokenHandlers) createToken(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request tokens.CreateRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	settings, err := readSecuritySettings(r.Context(), runtime)
	if err != nil {
		log.Printf("read security settings for token create failed: %v", err)
		writeInternalError(w)
		return
	}
	var item tokens.CreateResponse
	err = s.withAuditedMutation(r.Context(), runtime, "user", nil, 0, "token.created", func() any {
		return map[string]any{"token_id": item.ID, "name": item.Name, "reusable_tokens": settings.ReusableTokens}
	}, func(tx *sql.Tx) error {
		var createErr error
		item, createErr = runtime.tokens.WithTx(tx).Create(r.Context(), request, tokens.CreateOptions{StoreReusableToken: settings.ReusableTokens})
		return createErr
	})
	if err != nil {
		handleTokenError(w, err)
		return
	}
	writeSensitiveJSON(w, http.StatusCreated, item)
}

func (s tokenHandlers) revokeToken(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	release, err := runtime.vaultDelivery.acquireExclusive(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "token revoke was canceled")
		return
	}
	defer release()
	var item tokens.Token
	err = s.withAuditedMutation(r.Context(), runtime, "user", nil, 0, "token.revoked", func() any {
		return map[string]any{"token_id": item.ID, "name": item.Name}
	}, func(tx *sql.Tx) error {
		var revokeErr error
		item, revokeErr = runtime.tokens.WithTx(tx).Revoke(r.Context(), id)
		return revokeErr
	})
	if err != nil {
		handleTokenError(w, err)
		return
	}
	settings, err := readSecuritySettings(r.Context(), runtime)
	if err != nil {
		log.Printf("read security settings for token revoke failed: %v", err)
		writeInternalError(w)
		return
	}
	if !settings.ReusableTokens {
		item.TokenValue = ""
	}
	if runtime.vaultLeases != nil {
		if err := s.invalidateVaultTokenSessions(r.Context(), runtime, id, "token revoked; send a fresh Vault request"); err != nil {
			log.Printf("invalidate Vault token sessions failed token=%d error=%v", id, err)
			writeInternalError(w)
			return
		}
	}
	writeSensitiveJSON(w, http.StatusOK, item)
}
