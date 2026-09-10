package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

type vaultSessionOptionsResponse struct {
	Supported       bool                          `json:"supported"`
	TargetProjectID int64                         `json:"target_project_id"`
	Items           []projectvault.Item           `json:"items"`
	Total           int                           `json:"total"`
	Defaults        []projectvault.DefaultBinding `json:"defaults"`
	Projects        []projectstore.Project        `json:"projects"`
}

func (s vaultItemHandlers) vaultSessionOptions(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	runtimeID, ok := parseInt64Query(w, strings.TrimSpace(r.URL.Query().Get("runtime_id")), "runtime_id")
	if !ok {
		return
	}
	targetStore := connectortargets.NewStore(runtime.database)
	surface, err := targetStore.GetRuntimeSurface(r.Context(), runtimeID)
	if errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		writeError(w, http.StatusNotFound, "runtime surface not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	target, err := targetStore.GetTarget(r.Context(), surface.TargetID)
	if errors.Is(err, connectortargets.ErrTargetNotFound) {
		writeError(w, http.StatusNotFound, "connector target not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := requireSessionEnvironmentCapability(r.Context(), s.Server, runtime, runtimeID); errors.Is(err, connectors.ErrSessionEnvironmentUnsupported) {
		writeJSON(w, http.StatusOK, vaultSessionOptionsResponse{
			Supported: false, TargetProjectID: target.ProjectID,
			Items: []projectvault.Item{}, Defaults: []projectvault.DefaultBinding{},
		})
		return
	} else if err != nil {
		writeInternalError(w)
		return
	}
	owner, err := s.projectVaultRuntime(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	items, total, err := owner.List(r.Context(), projectvault.ListFilter{
		Query: strings.TrimSpace(r.URL.Query().Get("q")), Limit: 100,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	defaults, err := owner.ListDefaultBindings(r.Context(), projectvault.DefaultBindingFilter{
		TargetID: surface.TargetID, ProfileID: surface.ProfileID,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	projects, err := projectstore.NewStore(runtime.database).List(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, vaultSessionOptionsResponse{
		Supported: true, TargetProjectID: target.ProjectID,
		Items: items, Total: total, Defaults: defaults, Projects: projects,
	})
}
