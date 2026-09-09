package securitypolicy

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type HTTPScope struct {
	Service *Service
	Mutate  auditedmutation.Runner
}

type HTTPScopeProvider func(http.ResponseWriter) (HTTPScope, bool)

type HTTPHandlers struct{ scope HTTPScopeProvider }

func NewHTTPHandlers(scope HTTPScopeProvider) *HTTPHandlers {
	return &HTTPHandlers{scope: scope}
}

func (h *HTTPHandlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, false)
	if !ok {
		return
	}
	settings, err := scope.Service.ReadSettings(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, settings)
}

func (h *HTTPHandlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, true)
	if !ok {
		return
	}
	var settings Settings
	if !httptransport.DecodeJSON(w, r, &settings, httptransport.DefaultJSONBodyBytes) {
		return
	}
	settings, err := scope.Service.UpdateSettings(r.Context(), settings, scope.Mutate)
	if err != nil {
		handleError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, settings)
}

func (h *HTTPHandlers) ListRules(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, false)
	if !ok {
		return
	}
	items, err := scope.Service.ListRules(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, items)
}

func (h *HTTPHandlers) CreateRule(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, true)
	if !ok {
		return
	}
	var input RuleInput
	if !httptransport.DecodeJSON(w, r, &input, httptransport.DefaultJSONBodyBytes) {
		return
	}
	item, err := scope.Service.CreateRule(r.Context(), input, scope.Mutate)
	if err != nil {
		handleError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, item)
}

func (h *HTTPHandlers) UpdateRule(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, true)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var input RuleInput
	if !httptransport.DecodeJSON(w, r, &input, httptransport.DefaultJSONBodyBytes) {
		return
	}
	item, err := scope.Service.UpdateRule(r.Context(), id, input, scope.Mutate)
	if err != nil {
		handleError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}

func (h *HTTPHandlers) DeleteRule(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, true)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := scope.Service.DeleteRule(r.Context(), id, scope.Mutate); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandlers) resolve(w http.ResponseWriter, mutation bool) (HTTPScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return HTTPScope{}, false
	}
	if scope.Service == nil || mutation && scope.Mutate == nil {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	return scope, true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		httptransport.WriteError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func handleError(w http.ResponseWriter, err error) {
	var validation ValidationError
	switch {
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, sql.ErrNoRows):
		httptransport.WriteError(w, http.StatusNotFound, "redaction rule not found")
	case strings.Contains(strings.ToLower(err.Error()), "unique"):
		httptransport.WriteError(w, http.StatusConflict, "redaction rule name already exists")
	default:
		httptransport.WriteInternalError(w)
	}
}
