package connectormanagement

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func (h *HTTPHandlers) ListTargets(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireDatabase)
	if !ok {
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	targets, err := connectortargets.NewStore(scope.Database).ListTargets(
		r.Context(), connectortargets.ListTargetsFilter{ConnectorKind: kind},
	)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	items := make([]TargetResponse, 0, len(targets))
	for _, target := range targets {
		items = append(items, TargetToResponse(target, nil))
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *HTTPHandlers) ListTargetInventory(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireDatabase|requireRegistry|requireFeatures|requireSessionEnvironment)
	if !ok {
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	store := connectortargets.NewStore(scope.Database)
	targets, err := store.ListTargets(r.Context(), connectortargets.ListTargetsFilter{ConnectorKind: kind})
	if err != nil {
		writeTargetError(w, err)
		return
	}
	items := make([]TargetResponse, 0, len(targets))
	for _, target := range targets {
		connector, exists := scope.Registry.Get(target.ConnectorKind)
		if !exists {
			httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
			return
		}
		profiles, listErr := store.ListCredentialProfiles(r.Context(), target.ID)
		if listErr != nil {
			writeTargetError(w, listErr)
			return
		}
		summaries := make([]ProfileSummary, 0, len(profiles))
		for _, profile := range profiles {
			targetView := connectors.TargetView{
				ID: target.ID, ProjectID: target.ProjectID,
				Ref:           connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID),
				ConnectorKind: target.ConnectorKind, Name: target.Name,
				Config: target.Config, UpdatedAt: target.UpdatedAt,
			}
			actions, actionsErr := connectors.GetActionDefinitions(
				r.Context(), connector, targetView, connectortargets.CredentialProfileView(profile),
			)
			if actionsErr != nil {
				httptransport.WriteInternalError(w)
				return
			}
			summary := ProfileToSummary(profile)
			summary.Actions = actions
			features := scope.Features(target.ConnectorKind)
			if features.LiveConsoleCapability != "" {
				surface, surfaceErr := store.GetRuntimeSurfaceByProfile(
					r.Context(), target.ConnectorKind, target.ID, profile.ID, features.LiveConsoleCapability,
				)
				switch {
				case surfaceErr == nil:
					summary.RuntimeID = surface.ID
					summary.VaultSession = scope.SessionEnvironmentSupported(r.Context(), surface.ID)
				case !errors.Is(surfaceErr, connectortargets.ErrRuntimeSurfaceNotFound):
					writeTargetError(w, surfaceErr)
					return
				}
			}
			if features.FileTransfer {
				surface, surfaceErr := store.GetRuntimeSurfaceByProfile(
					r.Context(), target.ConnectorKind, target.ID, profile.ID, connectortargets.RuntimeCapabilityFileTransfer,
				)
				switch {
				case surfaceErr == nil:
					summary.TransferRuntimeID = surface.ID
				case !errors.Is(surfaceErr, connectortargets.ErrRuntimeSurfaceNotFound):
					writeTargetError(w, surfaceErr)
					return
				}
			}
			summaries = append(summaries, summary)
		}
		response := TargetToResponse(target, nil)
		response.Profiles = summaries
		items = append(items, response)
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *HTTPHandlers) GetTarget(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireDatabase)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	store := connectortargets.NewStore(scope.Database)
	target, err := store.GetTarget(r.Context(), id)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	profiles, err := store.ListCredentialProfiles(r.Context(), target.ID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, TargetToResponse(target, profiles))
}

func (h *HTTPHandlers) ListCredentialProfiles(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireDatabase)
	if !ok {
		return
	}
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	store := connectortargets.NewStore(scope.Database)
	if _, err := store.GetTarget(r.Context(), targetID); err != nil {
		writeTargetError(w, err)
		return
	}
	profiles, err := store.ListCredentialProfiles(r.Context(), targetID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": ProfileSummaries(profiles)})
}

func (h *HTTPHandlers) ListCredentialProfileActions(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireDatabase|requireRegistry)
	if !ok {
		return
	}
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	profileID, ok := httptransport.ParsePathInt64(w, r, "profile_id", "profile_id is required")
	if !ok {
		return
	}
	target, profile, err := connectortargets.NewStore(scope.Database).ResolveTargetProfileViews(r.Context(), targetID, profileID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	connector, exists := scope.Registry.Get(target.ConnectorKind)
	if !exists {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	actions, err := connectors.GetActionDefinitions(r.Context(), connector, target, profile)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": actions})
}

func writeTargetError(w http.ResponseWriter, err error) {
	var validation connectortargets.ValidationError
	switch {
	case errors.Is(err, connectortargets.ErrTargetUpdateConflict),
		errors.Is(err, connectortargets.ErrCredentialProfileUpdateConflict):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, connectortargets.ErrTargetNotFound),
		errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "connector target not found")
	case errors.Is(err, connectortargets.ErrInvalidTargetRef):
		httptransport.WriteError(w, http.StatusBadRequest, "invalid connector target ref")
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
	default:
		httptransport.WriteInternalError(w)
	}
}
