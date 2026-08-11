package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

type projectRequest struct {
	Name string `json:"name"`
}

func (s projectHandlers) listProjects(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	items, err := projectstore.NewStore(runtime.database).List(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s projectHandlers) createProject(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request projectRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	var item projectstore.Project
	err := s.withAuditedMutation(r.Context(), runtime, "user", nil, 0, "project.created", func() any {
		return map[string]any{"project_id": item.ID, "name": item.Name, "slug": item.Slug}
	}, func(tx *sql.Tx) error {
		var createErr error
		item, createErr = projectstore.NewTxStore(tx).Create(r.Context(), request.Name)
		return createErr
	})
	if err != nil {
		handleProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s projectHandlers) updateProject(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request projectRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	var item projectstore.Project
	err := s.withAuditedMutation(r.Context(), runtime, "user", nil, 0, "project.updated", func() any {
		return map[string]any{"project_id": item.ID, "name": item.Name, "slug": item.Slug}
	}, func(tx *sql.Tx) error {
		var updateErr error
		item, updateErr = projectstore.NewTxStore(tx).Update(r.Context(), id, request.Name)
		return updateErr
	})
	if err != nil {
		handleProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s projectHandlers) archiveProject(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "project archive was canceled")
		return
	}
	defer release()
	if err := s.withAuditedMutation(r.Context(), runtime, "user", nil, 0, "project.archived", func() any {
		return map[string]any{"project_id": id}
	}, func(tx *sql.Tx) error {
		return projectstore.NewTxStore(tx).Archive(r.Context(), id)
	}); err != nil {
		handleProjectError(w, err)
		return
	}
	if err := s.invalidateVaultProjectSessions(
		r.Context(),
		runtime,
		id,
		"project was archived; send a fresh Vault request",
	); err != nil {
		writeInternalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleProjectError(w http.ResponseWriter, err error) {
	var validation projectstore.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, strings.TrimSpace(validation.Error()))
	case errors.Is(err, projectstore.ErrNotFound):
		writeError(w, http.StatusNotFound, "project not found")
	case errors.Is(err, projectstore.ErrProtected), errors.Is(err, projectstore.ErrProjectNotEmpty):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeInternalError(w)
	}
}
