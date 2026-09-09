package retention

import (
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type PurgeRequest struct {
	Target string `json:"target"`
	Days   int    `json:"days"`
}

type PurgeResponse struct {
	Target  string `json:"target"`
	Days    int    `json:"days"`
	Deleted int64  `json:"deleted"`
}

type HTTPScope struct {
	Service *Service
	Mutate  auditedmutation.Runner
}

type HTTPScopeProvider func(http.ResponseWriter) (HTTPScope, bool)

type HTTPHandlers struct{ scope HTTPScopeProvider }

func NewHTTPHandlers(scope HTTPScopeProvider) *HTTPHandlers { return &HTTPHandlers{scope: scope} }

func (h *HTTPHandlers) Get(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveService(w)
	if !ok {
		return
	}
	settings, err := scope.Service.Read(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, settings)
}

func (h *HTTPHandlers) Update(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveMutation(w)
	if !ok {
		return
	}
	var settings Settings
	if !httptransport.DecodeJSON(w, r, &settings, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if _, err := scope.Service.Update(r.Context(), settings, scope.Mutate); err != nil {
		handleError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, settings)
}

func (h *HTTPHandlers) Purge(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveMutation(w)
	if !ok {
		return
	}
	var request PurgeRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	deleted, err := scope.Service.PurgeAudited(r.Context(), request.Target, request.Days, scope.Mutate)
	if err != nil {
		handleError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, PurgeResponse{Target: request.Target, Days: request.Days, Deleted: deleted})
}

func (h *HTTPHandlers) resolveService(w http.ResponseWriter) (HTTPScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return HTTPScope{}, false
	}
	if scope.Service == nil {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	return scope, true
}

func (h *HTTPHandlers) resolveMutation(w http.ResponseWriter) (HTTPScope, bool) {
	scope, ok := h.resolveService(w)
	if !ok {
		return HTTPScope{}, false
	}
	if scope.Mutate == nil {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	return scope, true
}

func handleError(w http.ResponseWriter, err error) {
	var validation ValidationError
	if errors.As(err, &validation) {
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
		return
	}
	httptransport.WriteInternalError(w)
}
