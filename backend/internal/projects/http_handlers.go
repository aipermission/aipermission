package projects

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
)

type Scope struct {
	Database         *sql.DB
	Mutate           auditedmutation.Runner
	AcquireExclusive func(context.Context) (func(), error)
	Invalidate       func(context.Context, int64, []SessionReference) error
}

type SessionReference struct {
	SessionID  int64
	RuntimeID  int64
	Generation int64
}

type ScopeProvider func(http.ResponseWriter) (Scope, bool)

type HTTPHandlers struct {
	scope ScopeProvider
}

func NewHTTPHandlers(scope ScopeProvider) *HTTPHandlers {
	return &HTTPHandlers{scope: scope}
}

type Request struct {
	Name string `json:"name"`
}

func (h *HTTPHandlers) List(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.runtimeScope(w, false)
	if !ok {
		return
	}
	items, err := NewStore(scope.Database).List(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *HTTPHandlers) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.runtimeScope(w, true)
	if !ok {
		return
	}
	var request Request
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	var item Project
	err := scope.Mutate(r.Context(), "project.created", func() any {
		return map[string]any{"project_id": item.ID, "name": item.Name, "slug": item.Slug}
	}, func(tx *sql.Tx) error {
		var err error
		item, err = NewTxStore(tx).Create(r.Context(), request.Name)
		return err
	})
	if err != nil {
		WriteHTTPError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, item)
}

func (h *HTTPHandlers) Update(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.runtimeScope(w, true)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	var request Request
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	var item Project
	err := scope.Mutate(r.Context(), "project.updated", func() any {
		return map[string]any{"project_id": item.ID, "name": item.Name, "slug": item.Slug}
	}, func(tx *sql.Tx) error {
		var err error
		item, err = NewTxStore(tx).Update(r.Context(), id, request.Name)
		return err
	})
	if err != nil {
		WriteHTTPError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}

func (h *HTTPHandlers) Archive(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.runtimeScope(w, true)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	if scope.AcquireExclusive == nil || scope.Invalidate == nil {
		httptransport.WriteInternalError(w)
		return
	}
	release, err := scope.AcquireExclusive(r.Context())
	if err != nil {
		httptransport.WriteError(w, http.StatusRequestTimeout, "project archive was canceled")
		return
	}
	defer release()
	finalizations := vaultfinalization.NewStore(scope.Database)
	if err := finalizations.RequireReady(r.Context()); err != nil {
		WriteHTTPError(w, err)
		return
	}
	references, err := finalizations.ActiveProjectReferences(r.Context(), id)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	var finalizationID int64
	if err := scope.Mutate(r.Context(), "project.archived", func() any {
		return map[string]any{"project_id": id}
	}, func(tx *sql.Tx) error {
		if err := NewTxStore(tx).Archive(r.Context(), id); err != nil {
			return err
		}
		var err error
		finalizationID, err = vaultfinalization.NewStore(tx).Queue(r.Context(), vaultfinalization.Intent{
			Kind: "project", ProjectID: id, Reason: "project was archived; send a fresh Vault request", References: references,
		})
		return err
	}); err != nil {
		WriteHTTPError(w, err)
		return
	}
	if err := finalizations.Finalize(r.Context(), finalizationID, func(ctx context.Context) error {
		items := make([]SessionReference, len(references))
		for index, reference := range references {
			items[index] = SessionReference{
				SessionID: reference.SessionID, RuntimeID: reference.RuntimeID, Generation: reference.Generation,
			}
		}
		return scope.Invalidate(ctx, id, items)
	}); err != nil {
		WriteHTTPError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandlers) runtimeScope(w http.ResponseWriter, mutation bool) (Scope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return Scope{}, false
	}
	if scope.Database == nil || (mutation && scope.Mutate == nil) {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	return scope, true
}

func WriteHTTPError(w http.ResponseWriter, err error) {
	var validation ValidationError
	switch {
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, strings.TrimSpace(validation.Error()))
	case errors.Is(err, ErrNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "project not found")
	case errors.Is(err, ErrProtected), errors.Is(err, ErrProjectNotEmpty):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, vaultfinalization.ErrPending):
		httptransport.WriteErrorCode(w, http.StatusConflict, err.Error(), "vault_finalization_pending")
	case errors.Is(err, vaultfinalization.ErrBlocked):
		httptransport.WriteErrorCode(w, http.StatusConflict, err.Error(), "vault_finalization_blocked")
	default:
		httptransport.WriteInternalError(w)
	}
}
