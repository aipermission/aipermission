package api

import (
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
	writeJSON(w, http.StatusOK, items)
}

func (s tokenHandlers) createToken(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request tokens.CreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	settings, err := readSecuritySettings(r.Context(), runtime)
	if err != nil {
		log.Printf("read security settings for token create failed: %v", err)
		writeInternalError(w)
		return
	}
	item, err := runtime.tokens.Create(r.Context(), request, tokens.CreateOptions{StoreReusableToken: settings.ReusableTokens})
	if err != nil {
		handleTokenError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "token.created", map[string]any{
		"token_id":        item.ID,
		"name":            item.Name,
		"reusable_tokens": settings.ReusableTokens,
	})
	writeJSON(w, http.StatusCreated, item)
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

	item, err := runtime.tokens.Revoke(r.Context(), id)
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
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "token.revoked", map[string]any{
		"token_id": item.ID,
		"name":     item.Name,
	})
	writeJSON(w, http.StatusOK, item)
}
