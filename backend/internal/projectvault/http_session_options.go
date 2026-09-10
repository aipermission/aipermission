package projectvault

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

var ErrSessionRuntimeNotFound = errors.New("runtime surface not found")
var ErrSessionTargetNotFound = errors.New("connector target not found")

type SessionOptionsTarget struct {
	ProjectID                   int64
	TargetID                    int64
	ProfileID                   int64
	SessionEnvironmentSupported bool
}

type SessionOptionsProject struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	TargetCount int    `json:"target_count"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type SessionOptionsCatalog interface {
	ResolveSessionOptionsTarget(context.Context, int64) (SessionOptionsTarget, error)
	ListSessionOptionsProjects(context.Context) ([]SessionOptionsProject, error)
}

type SessionOptionsResponse struct {
	Supported       bool                    `json:"supported"`
	TargetProjectID int64                   `json:"target_project_id"`
	Items           []Item                  `json:"items"`
	Total           int                     `json:"total"`
	Defaults        []DefaultBinding        `json:"defaults"`
	Projects        []SessionOptionsProject `json:"projects"`
}

func (h *HTTPHandlers) SessionOptions(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	if scope.SessionCatalog == nil {
		httptransport.WriteInternalError(w)
		return
	}
	runtimeID, ok := httptransport.ParseQueryInt64(
		w, strings.TrimSpace(r.URL.Query().Get("runtime_id")), "runtime_id",
	)
	if !ok {
		return
	}
	target, err := scope.SessionCatalog.ResolveSessionOptionsTarget(r.Context(), runtimeID)
	switch {
	case errors.Is(err, ErrSessionRuntimeNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "runtime surface not found")
		return
	case errors.Is(err, ErrSessionTargetNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "connector target not found")
		return
	case err != nil:
		httptransport.WriteInternalError(w)
		return
	}
	if !target.SessionEnvironmentSupported {
		httptransport.WriteJSON(w, http.StatusOK, SessionOptionsResponse{
			Supported: false, TargetProjectID: target.ProjectID,
			Items: []Item{}, Defaults: []DefaultBinding{},
		})
		return
	}
	items, total, err := scope.Runtime.List(r.Context(), ListFilter{
		Query: strings.TrimSpace(r.URL.Query().Get("q")), Limit: 100,
	})
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	defaults, err := scope.Runtime.ListDefaultBindings(r.Context(), DefaultBindingFilter{
		TargetID: target.TargetID, ProfileID: target.ProfileID,
	})
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	projects, err := scope.SessionCatalog.ListSessionOptionsProjects(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, SessionOptionsResponse{
		Supported: true, TargetProjectID: target.ProjectID,
		Items: items, Total: total, Defaults: defaults, Projects: projects,
	})
}
