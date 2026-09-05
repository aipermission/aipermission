package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (s connectorTargetHandlers) listConnectorTargets(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	store := connectortargets.NewStore(runtime.database)
	targets, err := store.ListTargets(r.Context(), connectortargets.ListTargetsFilter{ConnectorKind: kind})
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	items := make([]connectorTargetResponse, 0, len(targets))
	for _, target := range targets {
		items = append(items, connectorTargetToResponse(target, nil))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s connectorTargetHandlers) listConnectorTargetInventory(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	store := connectortargets.NewStore(runtime.database)
	targets, err := store.ListTargets(r.Context(), connectortargets.ListTargetsFilter{ConnectorKind: kind})
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	registry := runtime.connectorRegistry()
	items := make([]connectorTargetResponse, 0, len(targets))
	for _, target := range targets {
		connector, ok := registry.Get(target.ConnectorKind)
		if !ok {
			writeError(w, http.StatusBadRequest, "unsupported connector kind")
			return
		}
		profiles, err := store.ListCredentialProfiles(r.Context(), target.ID)
		if err != nil {
			handleConnectorTargetError(w, err)
			return
		}
		summaries := make([]profileSummary, 0, len(profiles))
		for _, profile := range profiles {
			targetView := connectors.TargetView{
				ID:            target.ID,
				Ref:           connectortargets.ConnectorTargetRef(target.ConnectorKind, target.ID, profile.ID),
				ConnectorKind: target.ConnectorKind,
				Name:          target.Name,
				Config:        target.Config,
			}
			profileView := connectortargets.CredentialProfileView(profile)
			actions, err := connectors.GetActionDefinitions(r.Context(), connector, targetView, profileView)
			if err != nil {
				writeInternalError(w)
				return
			}
			summary := profileToSummary(profile)
			summary.Actions = actions
			if adapter := s.connectorLiveConsoleTargetAdapterFor(target.ConnectorKind); adapter != nil {
				surface, surfaceErr := store.GetRuntimeSurfaceByProfile(
					r.Context(), target.ConnectorKind, target.ID, profile.ID, adapter.LiveConsoleCapabilityKind(),
				)
				switch {
				case surfaceErr == nil:
					summary.RuntimeID = surface.ID
					summary.VaultSession = requireSessionEnvironmentCapability(r.Context(), s.Server, runtime, surface.ID) == nil
				case !errors.Is(surfaceErr, connectortargets.ErrRuntimeSurfaceNotFound):
					handleConnectorTargetError(w, surfaceErr)
					return
				}
			}
			if s.connectorFileTransferAdapterFor(target.ConnectorKind) != nil {
				surface, surfaceErr := store.GetRuntimeSurfaceByProfile(
					r.Context(), target.ConnectorKind, target.ID, profile.ID, connectortargets.RuntimeCapabilityFileTransfer,
				)
				switch {
				case surfaceErr == nil:
					summary.TransferRuntimeID = surface.ID
				case !errors.Is(surfaceErr, connectortargets.ErrRuntimeSurfaceNotFound):
					handleConnectorTargetError(w, surfaceErr)
					return
				}
			}
			summaries = append(summaries, summary)
		}
		response := connectorTargetToResponse(target, nil)
		response.Profiles = summaries
		items = append(items, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s connectorTargetHandlers) testConnectorTargetDraft(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request createConnectorTargetRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	registry := runtime.connectorRegistry()
	connector, ok := registry.Get(strings.TrimSpace(request.ConnectorKind))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if err := validateConnectorTargetSchema(connector); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	config, err := connectors.NormalizeSchemaValues(connector.TargetSchema(), request.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Config = config
	if err := validateConnectorTargetConfig(connector, request.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.validateConnectorTransportConfig(r.Context(), connectortargets.NewStore(runtime.database), request.ProjectID, request.Config); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if adapter := s.connectorDraftTesterFor(request.ConnectorKind); adapter != nil {
		adapter.TestDraft(connectorPeerGatewayPort{server: s.Server}, w, r, connectorDataRuntimePort(runtime, request.ConnectorKind), request)
		return
	}
	writeError(w, http.StatusBadRequest, "draft test is not supported for this connector")
}

func (s connectorTargetHandlers) getConnectorTarget(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), id)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	profiles, err := store.ListCredentialProfiles(r.Context(), target.ID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, connectorTargetToResponse(target, profiles))
}
