package api

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
)

type connectorCatalogItem struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Version string `json:"version"`
}

type connectorCatalogDetail struct {
	Kind              string                        `json:"kind"`
	Label             string                        `json:"label"`
	Version           string                        `json:"version"`
	TargetSchema      connectors.Schema             `json:"target_schema"`
	CredentialSchemas []connectors.CredentialSchema `json:"credential_schemas"`
	Actions           []connectors.ActionDefinition `json:"actions"`
	Help              connectors.ConnectorHelp      `json:"help"`
}

func (s connectorHandlers) listConnectors(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.activeRuntimeOrLocked(w); !ok {
		return
	}
	registry, err := builtin.NewRegistry()
	if err != nil {
		writeInternalError(w)
		return
	}
	infos := registry.List()
	items := make([]connectorCatalogItem, 0, len(infos))
	for _, info := range infos {
		items = append(items, connectorCatalogItem{
			Kind:    info.Kind,
			Label:   info.Label,
			Version: info.Version,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s connectorHandlers) getConnector(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.activeRuntimeOrLocked(w); !ok {
		return
	}
	kind := r.PathValue("kind")
	if !connectors.ValidIdentifier(kind) {
		writeError(w, http.StatusBadRequest, "invalid connector kind")
		return
	}
	registry, err := builtin.NewRegistry()
	if err != nil {
		writeInternalError(w)
		return
	}
	connector, ok := registry.Get(kind)
	if !ok {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	target := connectors.TargetView{ConnectorKind: connector.Kind()}
	actions, err := connector.GetActionList(r.Context(), target)
	if err != nil {
		writeInternalError(w)
		return
	}
	help, err := connector.GetHelp(r.Context(), target)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, connectorCatalogDetail{
		Kind:              connector.Kind(),
		Label:             connector.Label(),
		Version:           connector.Version(),
		TargetSchema:      connector.TargetSchema(),
		CredentialSchemas: connector.CredentialSchemas(),
		Actions:           actions,
		Help:              help,
	})
}
